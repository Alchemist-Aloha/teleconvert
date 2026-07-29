package orchestrator

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"teleconvert/internal/config"
	"teleconvert/internal/discovery"
	"teleconvert/internal/ledger"
	"teleconvert/internal/tui"
	"teleconvert/internal/worker"
)

type Options struct {
	ConfigPath    string
	InputPath     string
	OutputDir     string
	OutputExt     string
	DeleteSource  bool
	PollInterval  time.Duration
	ContinueOnErr bool
	Verbose       bool
}

type Orchestrator struct {
	opts          Options
	workerFactory func(config.Node, func(string, ...any)) worker.Worker
	sigNotify     func(chan<- os.Signal, ...os.Signal)
	ui            *tui.Dashboard
}

type slot struct {
	name      string
	w         worker.Worker
	node      config.Node
	slotIdx   int
	pidFile   string
	exitFile  string
	stderrLog string
}

type activeProc struct {
	job  discovery.Job
	sl   slot
	pid  int
	part string
}

type jobLedger interface {
	Set(jobPath, status, worker, lastErr string) error
	Get(jobPath string) (ledger.Entry, bool)
}

func New(opts Options) *Orchestrator {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}
	return &Orchestrator{
		opts: opts,
		ui:   tui.New(os.Stdin, os.Stdout),
		workerFactory: func(node config.Node, vlog func(string, ...any)) worker.Worker {
			if config.IsLocalAddress(node.Address) {
				return worker.NewLocal(node, vlog)
			}
			return worker.NewSSH(node, vlog)
		},
		sigNotify: signal.Notify,
	}
}

func (o *Orchestrator) Run(ctx context.Context) error {
	o.ui.Start()
	defer o.ui.Close()

	cfg, err := config.Load(o.opts.ConfigPath)
	if err != nil {
		return err
	}

	o.log("Discovering jobs...")
	jobs, _, err := discovery.Discover(o.opts.InputPath, o.opts.OutputDir, o.opts.OutputExt)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return errors.New("no video files discovered")
	}

	jobPaths := make([]string, 0, len(jobs))
	for _, j := range jobs {
		jobPaths = append(jobPaths, j.InputPath)
	}
	ld, err := ledger.NewRouter(jobPaths)
	if err != nil {
		return err
	}
	if err := ld.InitJobs(jobPaths); err != nil {
		return err
	}
	o.log("Discovered %d total jobs", len(jobs))

	slots, err := o.buildSlots(ctx, cfg)
	if err != nil {
		return err
	}
	if len(slots) == 0 {
		return errors.New("no healthy worker slots available")
	}

	pending := make([]discovery.Job, 0, len(jobs))
	for _, j := range jobs {
		e, ok := ld.Get(j.InputPath)
		if ok && e.Status == ledger.StatusDone {
			continue
		}
		pending = append(pending, j)
	}
	if len(pending) == 0 {
		o.log("All jobs already marked done in ledger. Press Ctrl-C or q to close the dashboard.")
		o.waitForDismissal(ctx)
		return nil
	}

	o.ui.SetTotal(len(pending))
	for _, sl := range slots {
		o.ui.RegisterWorker(sl.name, sl.node.Name)
	}
	o.log("%d pending jobs across %d worker slots", len(pending), len(slots))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	o.sigNotify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	active := make(map[string]activeProc)
	var activeMu sync.Mutex
	results := make(chan jobResult, len(pending))

	inFlight := 0
	nextIdx := 0
	freeSlots := make([]slot, len(slots))
	copy(freeSlots, slots)

	for (nextIdx < len(pending) || inFlight > 0) && ctx.Err() == nil {
		for nextIdx < len(pending) && len(freeSlots) > 0 {
			j := pending[nextIdx]
			nextIdx++
			sl := freeSlots[0]
			freeSlots = freeSlots[1:]
			inFlight++
			go o.runJob(ctx, j, sl, ld, &activeMu, active, results)
		}

		select {
		case sig := <-sigCh:
			o.log("Received signal %s; performing clean shutdown...", sig)
			o.cleanKill(context.Background(), ld, &activeMu, active)
			cancel()
		case <-o.ui.Quit():
			o.log("Shutdown requested; stopping active encoders...")
			o.cleanKill(context.Background(), ld, &activeMu, active)
			cancel()
		case res := <-results:
			inFlight--
			freeSlots = append(freeSlots, res.slot)
			o.ui.JobFinished(res.slot.name, res.err)
			if res.err != nil {
				o.log("FAILED %s on %s: %v", res.job.InputPath, res.slot.name, res.err)
				if !o.opts.ContinueOnErr {
					o.cleanKill(context.Background(), ld, &activeMu, active)
					cancel()
				}
			} else {
				o.log("Done %s -> %s", res.job.InputPath, res.job.OutputPath)
			}
		case <-time.After(250 * time.Millisecond):
		}
	}

	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}

	if nextIdx < len(pending) || inFlight > 0 {
		return errors.New("conversion interrupted before completion")
	}
	o.log("All jobs finished. Press Ctrl-C or q to close the dashboard.")
	o.waitForDismissal(ctx)
	return nil
}

func (o *Orchestrator) waitForDismissal(ctx context.Context) {
	if !o.ui.Interactive() {
		return
	}
	select {
	case <-o.ui.Quit():
	case <-ctx.Done():
	}
}

type jobResult struct {
	job  discovery.Job
	slot slot
	err  error
}

func (o *Orchestrator) runJob(ctx context.Context, job discovery.Job, sl slot, ld jobLedger, activeMu *sync.Mutex, active map[string]activeProc, results chan<- jobResult) {
	err := o.executeJob(ctx, job, sl, ld, activeMu, active)
	results <- jobResult{job: job, slot: sl, err: err}
}

func (o *Orchestrator) executeJob(ctx context.Context, job discovery.Job, sl slot, ld jobLedger, activeMu *sync.Mutex, active map[string]activeProc) error {
	w := sl.w
	node := sl.node
	o.ui.JobStarted(sl.name, job.InputPath)
	defer func() {
		activeMu.Lock()
		delete(active, job.InputPath)
		activeMu.Unlock()
	}()
	if err := ld.Set(job.InputPath, ledger.StatusTransferring, node.Name, ""); err != nil {
		return err
	}
	o.ui.JobStage(sl.name, "preparing")

	if err := w.EnsureDir(ctx, node.TmpDir); err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "ensure tmp dir failed: "+err.Error())
		return err
	}

	remoteInput, remoteOutput := remoteJobPaths(node.TmpDir, job)
	isLocal := config.IsLocalAddress(node.Address)
	part := ""
	var err error
	cleanupPaths := []string{remoteOutput, sl.pidFile, sl.exitFile, sl.stderrLog}
	if isLocal {
		remoteInput = job.InputPath
		o.vlog("using source file directly for local worker: %s", remoteInput)
	} else {
		o.ui.JobStage(sl.name, "uploading")
		o.vlog("uploading %s to %s:%s", job.InputPath, node.Name, remoteInput)
		part, err = w.UploadAtomic(ctx, job.InputPath, remoteInput)
		cleanupPaths = append([]string{part, remoteInput}, cleanupPaths...)
	}
	commandMayBeRunning := false
	pid := 0
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if commandMayBeRunning {
			if pid <= 0 {
				pid, _ = w.ReadPID(cleanupCtx, sl.pidFile)
			}
			if pid > 0 {
				if err := w.SignalTERM(cleanupCtx, pid); err != nil {
					o.log("Warning: failed to stop pid %d during cleanup on %s: %v", pid, node.Name, err)
				}
			}
		}
		if err := w.Remove(cleanupCtx, cleanupPaths...); err != nil {
			o.log("Warning: cleanup failed on %s: %v", node.Name, err)
		}
	}()
	if err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "upload failed: "+err.Error())
		return err
	}
	if !isLocal {
		o.ui.JobStage(sl.name, "verifying")
		o.vlog("upload done for %s", job.InputPath)
		o.vlog("verifying md5 for %s", job.InputPath)
		localMD5, err := fileMD5(job.InputPath)
		if err != nil {
			_ = ld.Set(job.InputPath, ledger.StatusPending, "", "local md5 failed: "+err.Error())
			return err
		}
		remoteMD5, err := w.MD5(ctx, remoteInput)
		if err != nil {
			_ = ld.Set(job.InputPath, ledger.StatusPending, "", "remote md5 failed: "+err.Error())
			return err
		}
		if !strings.EqualFold(localMD5, remoteMD5) {
			_ = ld.Set(job.InputPath, ledger.StatusPending, "", "md5 mismatch")
			return fmt.Errorf("md5 mismatch for %s", job.InputPath)
		}
		o.vlog("md5 match for %s (%s)", job.InputPath, localMD5)
	}

	cmd, err := renderCommand(node.Command, remoteInput, remoteOutput)
	if err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "render command failed: "+err.Error())
		return err
	}

	o.vlog("starting remote command on %s: %s", node.Name, cmd)
	activeMu.Lock()
	active[job.InputPath] = activeProc{job: job, sl: sl, part: part}
	activeMu.Unlock()

	commandMayBeRunning = true
	o.ui.JobStage(sl.name, "starting")
	pid, err = w.StartCommand(ctx, cmd, sl.pidFile, sl.exitFile, sl.stderrLog)
	if err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "start command failed: "+err.Error())
		return err
	}
	o.vlog("remote command started on %s with pid %d", node.Name, pid)

	activeMu.Lock()
	active[job.InputPath] = activeProc{job: job, sl: sl, pid: pid, part: part}
	activeMu.Unlock()

	if err := ld.Set(job.InputPath, ledger.StatusWorking, node.Name, ""); err != nil {
		return err
	}

	o.ui.JobStage(sl.name, "encoding")
	exitCode, err := w.WaitForExit(ctx, pid, sl.exitFile, sl.stderrLog, o.opts.PollInterval, o.ui.WorkerWriter(sl.name))
	if err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "wait failed: "+err.Error())
		return err
	}
	commandMayBeRunning = false
	if exitCode != 0 {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", fmt.Sprintf("remote exit code %d", exitCode))
		return fmt.Errorf("remote command exit code %d", exitCode)
	}

	ok, err := w.FileExistsNonZero(ctx, remoteOutput)
	if err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "verify output failed: "+err.Error())
		return err
	}
	if !ok {
		err := fmt.Errorf("remote output %q missing or empty; encoder exited successfully but did not create the expected file (check the command template and output format options)", remoteOutput)
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", err.Error())
		return err
	}

	localTmp := job.OutputPath + ".tmp"
	defer func() {
		if err := os.Remove(localTmp); err != nil && !os.IsNotExist(err) {
			o.log("Warning: failed to remove local temp file %s: %v", localTmp, err)
		}
	}()
	o.vlog("downloading %s:%s to %s", node.Name, remoteOutput, job.OutputPath)
	o.ui.JobStage(sl.name, "downloading")
	if err := w.Download(ctx, remoteOutput, localTmp); err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "download failed: "+err.Error())
		return err
	}
	if err := os.Rename(localTmp, job.OutputPath); err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "finalize output failed: "+err.Error())
		return err
	}
	o.vlog("download done for %s", job.OutputPath)

	if o.opts.DeleteSource {
		if err := os.Remove(job.InputPath); err != nil && !os.IsNotExist(err) {
			_ = ld.Set(job.InputPath, ledger.StatusFailed, node.Name, "delete source failed: "+err.Error())
			return err
		}
	}

	return ld.Set(job.InputPath, ledger.StatusDone, node.Name, "")
}

func (o *Orchestrator) cleanKill(ctx context.Context, ld jobLedger, activeMu *sync.Mutex, active map[string]activeProc) {
	activeMu.Lock()
	processes := make([]activeProc, 0, len(active))
	for _, p := range active {
		processes = append(processes, p)
	}
	activeMu.Unlock()

	for _, p := range processes {
		pid := p.pid
		if pid <= 0 {
			var err error
			pid, err = p.sl.w.ReadPID(ctx, p.sl.pidFile)
			if err != nil {
				o.log("Warning: failed to read pid on %s during shutdown: %v", p.sl.node.Name, err)
			}
		}
		if pid <= 0 {
			o.log("Warning: no pid available for interrupted job on %s", p.sl.node.Name)
		} else if err := p.sl.w.SignalTERM(ctx, pid); err != nil {
			o.log("Warning: failed to stop pid %d on %s: %v", pid, p.sl.node.Name, err)
		} else {
			remoteInput, remoteOutput := remoteJobPaths(p.sl.node.TmpDir, p.job)
			if err := p.sl.w.Remove(ctx, p.part, remoteInput, remoteOutput, p.sl.pidFile, p.sl.exitFile, p.sl.stderrLog); err != nil {
				o.log("Warning: failed to clean interrupted job on %s: %v", p.sl.node.Name, err)
			}
			localTmp := p.job.OutputPath + ".tmp"
			if err := os.Remove(localTmp); err != nil && !os.IsNotExist(err) {
				o.log("Warning: failed to remove interrupted local temp file %s: %v", localTmp, err)
			}
		}
		_ = ld.Set(p.job.InputPath, ledger.StatusPending, "", "interrupted")
	}
}

func (o *Orchestrator) vlog(format string, args ...any) {
	if o.opts.Verbose {
		o.ui.Event(format, args...)
	}
}

func (o *Orchestrator) log(format string, args ...any) {
	o.ui.Event(format, args...)
}

func (o *Orchestrator) buildSlots(ctx context.Context, cfg *config.Config) ([]slot, error) {
	all := make([]slot, 0, len(cfg.Nodes))
	for _, n := range cfg.Nodes {
		o.vlog("checking node %s (%s)", n.Name, n.Address)
		var w worker.Worker
		if config.IsLocalAddress(n.Address) {
			o.vlog("node %s is local", n.Name)
			w = o.workerFactory(n, o.vlog)
		} else {
			o.vlog("node %s is remote", n.Name)
			if n.User == "" {
				o.log("Skip node %s: user is required for ssh node", n.Name)
				continue
			}
			if n.SSHKey == "" {
				o.log("Skip node %s: ssh_key is required for ssh node", n.Name)
				continue
			}
			w = o.workerFactory(n, o.vlog)
		}

		if err := w.Heartbeat(ctx); err != nil {
			o.log("Skip node %s: heartbeat failed: %v", n.Name, err)
			continue
		}
		o.vlog("node %s heartbeat ok", n.Name)

		cmdName := strings.Fields(n.Command)[0]
		if err := w.CheckCommand(ctx, cmdName); err != nil {
			o.log("Skip node %s: command %q not found: %v", n.Name, cmdName, err)
			continue
		}
		o.vlog("node %s command %q found", n.Name, cmdName)

		for i := 0; i < n.MaxConcurrent; i++ {
			pidFile := filepath.ToSlash(filepath.Join(n.TmpDir, fmt.Sprintf("teleconvert.%d.pid", i)))
			exitFile := filepath.ToSlash(filepath.Join(n.TmpDir, fmt.Sprintf("teleconvert.%d.exit", i)))
			stderrLog := filepath.ToSlash(filepath.Join(n.TmpDir, fmt.Sprintf("teleconvert.%d.stderr.log", i)))
			o.vlog("node %s slot %d: checking pid file %s", n.Name, i, pidFile)
			pid, err := w.ReadPID(ctx, pidFile)
			if err == nil && pid > 0 {
				running, runErr := w.IsProcessRunning(ctx, pid)
				if runErr == nil && running {
					o.log("Node %s slot %d busy with pid %d", n.Name, i, pid)
					continue
				}
			}
			o.vlog("node %s slot %d is free", n.Name, i)
			all = append(all, slot{
				name:      fmt.Sprintf("%s#%d", n.Name, i),
				w:         w,
				node:      n,
				slotIdx:   i,
				pidFile:   pidFile,
				exitFile:  exitFile,
				stderrLog: stderrLog,
			})
		}
	}
	return all, nil
}

func renderCommand(tpl, input, output string) (string, error) {
	funcMap := template.FuncMap{
		"quote": shellQuote,
	}
	t, err := template.New("command").Funcs(funcMap).Parse(tpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	data := struct {
		Input  string
		Output string
	}{
		Input:  shellQuote(input),
		Output: shellQuote(output),
	}
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

func jobIDFromPath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return "teleconvert-" + hex.EncodeToString(sum[:16])
}

func remoteJobPaths(tmpDir string, job discovery.Job) (string, string) {
	id := jobIDFromPath(job.InputPath)
	inputName := id + ".input" + safeRemoteExtension(filepath.Ext(job.InputPath))
	outputName := id + ".output" + safeRemoteExtension(filepath.Ext(job.OutputPath))
	return filepath.ToSlash(filepath.Join(tmpDir, inputName)),
		filepath.ToSlash(filepath.Join(tmpDir, outputName))
}

func safeRemoteExtension(ext string) string {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	if ext == "" || len(ext) > 16 {
		return ""
	}
	for _, r := range ext {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return "." + ext
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func fileMD5(path string) (string, error) {
	return workerMD5(path)
}

type prefixWriter struct {
	prefix string
	out    io.Writer
	buf    []byte
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	p.buf = append(p.buf, b...)
	for {
		idx := strings.IndexByte(string(p.buf), '\n')
		if idx == -1 {
			break
		}
		line := p.buf[:idx+1]
		if _, err := fmt.Fprint(p.out, p.prefix); err != nil {
			return 0, err
		}
		if _, err := p.out.Write(line); err != nil {
			return 0, err
		}
		p.buf = p.buf[idx+1:]
	}
	return len(b), nil
}

func workerMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5Pool.Get().(hashWrapper)
	h.Reset()
	defer md5Pool.Put(h)

	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return h.Hex(), nil
}

type hashWrapper interface {
	io.Writer
	Reset()
	Hex() string
}

type md5Hash struct {
	h hash.Hash
}

func (m md5Hash) Write(p []byte) (int, error) { return m.h.Write(p) }
func (m md5Hash) Reset()                      { m.h.Reset() }
func (m md5Hash) Hex() string                 { return hex.EncodeToString(m.h.Sum(nil)) }

var md5Pool = sync.Pool{
	New: func() any {
		return md5Hash{h: md5.New()}
	},
}
