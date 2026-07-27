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
	"teleconvert/internal/worker"

	"github.com/pterm/pterm"
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
	if o.opts.Verbose {
		pterm.EnableDebugMessages()
	}

	cfg, err := config.Load(o.opts.ConfigPath)
	if err != nil {
		return err
	}

	spinner, _ := pterm.DefaultSpinner.Start("Discovering jobs...")
	jobs, _, err := discovery.Discover(o.opts.InputPath, o.opts.OutputDir, o.opts.OutputExt)
	if err != nil {
		spinner.Fail(err.Error())
		return err
	}
	if len(jobs) == 0 {
		spinner.Fail("no video files discovered")
		return errors.New("no video files discovered")
	}

	jobPaths := make([]string, 0, len(jobs))
	for _, j := range jobs {
		jobPaths = append(jobPaths, j.InputPath)
	}
	ld, err := ledger.NewRouter(jobPaths)
	if err != nil {
		spinner.Fail(err.Error())
		return err
	}
	if err := ld.InitJobs(jobPaths); err != nil {
		spinner.Fail(err.Error())
		return err
	}
	spinner.Success(fmt.Sprintf("Discovered %d total jobs", len(jobs)))

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
		pterm.Info.Println("all jobs already marked done in ledger")
		return nil
	}

	pterm.Info.Printf("discovered %d total jobs, %d pending\n", len(jobs), len(pending))

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
			pterm.Warning.Printf("received signal %s, performing clean shutdown...\n", sig)
			o.cleanKill(context.Background(), ld, &activeMu, active)
			cancel()
		case res := <-results:
			inFlight--
			freeSlots = append(freeSlots, res.slot)
			if res.err != nil {
				pterm.Error.Printf("job failed: %s (%v)\n", res.job.InputPath, res.err)
				if !o.opts.ContinueOnErr {
					o.cleanKill(context.Background(), ld, &activeMu, active)
					cancel()
				}
			} else {
				pterm.Success.Printf("job done: %s -> %s\n", res.job.InputPath, res.job.OutputPath)
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
	return nil
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
	defer func() {
		activeMu.Lock()
		delete(active, job.InputPath)
		activeMu.Unlock()
	}()
	if err := ld.Set(job.InputPath, ledger.StatusTransferring, node.Name, ""); err != nil {
		return err
	}

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
					fmt.Fprintf(os.Stderr, "warning: failed to stop pid %d during cleanup on %s: %v\n", pid, node.Name, err)
				}
			}
		}
		if err := w.Remove(cleanupCtx, cleanupPaths...); err != nil {
			fmt.Fprintf(os.Stderr, "warning: cleanup failed on %s: %v\n", node.Name, err)
		}
	}()
	if err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "upload failed: "+err.Error())
		return err
	}
	if !isLocal {
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

	color := getColorForNode(node.Name)
	prefix := pterm.Color(color).Sprint(" " + node.Name + " ")
	prefixed := &prefixWriter{prefix: "[" + prefix + "] ", out: os.Stderr}
	exitCode, err := w.WaitForExit(ctx, pid, sl.exitFile, sl.stderrLog, o.opts.PollInterval, prefixed)
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
			fmt.Fprintf(os.Stderr, "warning: failed to remove local temp file %s: %v\n", localTmp, err)
		}
	}()
	o.vlog("downloading %s:%s to %s", node.Name, remoteOutput, job.OutputPath)
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
				fmt.Fprintf(os.Stderr, "warning: failed to read pid on %s during shutdown: %v\n", p.sl.node.Name, err)
			}
		}
		if pid <= 0 {
			fmt.Fprintf(os.Stderr, "warning: no pid available for interrupted job on %s\n", p.sl.node.Name)
		} else if err := p.sl.w.SignalTERM(ctx, pid); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to stop pid %d on %s: %v\n", pid, p.sl.node.Name, err)
		} else {
			remoteInput, remoteOutput := remoteJobPaths(p.sl.node.TmpDir, p.job)
			if err := p.sl.w.Remove(ctx, p.part, remoteInput, remoteOutput, p.sl.pidFile, p.sl.exitFile, p.sl.stderrLog); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to clean interrupted job on %s: %v\n", p.sl.node.Name, err)
			}
			localTmp := p.job.OutputPath + ".tmp"
			if err := os.Remove(localTmp); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "warning: failed to remove interrupted local temp file %s: %v\n", localTmp, err)
			}
		}
		_ = ld.Set(p.job.InputPath, ledger.StatusPending, "", "interrupted")
	}
}

var nodeColors = []pterm.Color{
	pterm.FgCyan,
	pterm.FgMagenta,
	pterm.FgBlue,
	pterm.FgYellow,
	pterm.FgGreen,
	pterm.FgRed,
}

func getColorForNode(name string) pterm.Color {
	sum := 0
	for _, r := range name {
		sum += int(r)
	}
	return nodeColors[sum%len(nodeColors)]
}

func (o *Orchestrator) vlog(format string, args ...any) {
	pterm.Debug.Printf(format, args...)
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
				fmt.Fprintf(os.Stderr, "skip node %s: user is required for ssh node\n", n.Name)
				continue
			}
			if n.SSHKey == "" {
				fmt.Fprintf(os.Stderr, "skip node %s: ssh_key is required for ssh node\n", n.Name)
				continue
			}
			w = o.workerFactory(n, o.vlog)
		}

		if err := w.Heartbeat(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "skip node %s: heartbeat failed: %v\n", n.Name, err)
			continue
		}
		o.vlog("node %s heartbeat ok", n.Name)

		cmdName := strings.Fields(n.Command)[0]
		if err := w.CheckCommand(ctx, cmdName); err != nil {
			fmt.Fprintf(os.Stderr, "skip node %s: command %q not found: %v\n", n.Name, cmdName, err)
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
					fmt.Fprintf(os.Stderr, "node %s slot %d busy with pid %d\n", n.Name, i, pid)
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
