package orchestrator

import (
	"context"
	"crypto/md5"
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
	opts Options
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

func New(opts Options) *Orchestrator {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 2 * time.Second
	}
	return &Orchestrator{opts: opts}
}

func (o *Orchestrator) Run(ctx context.Context) error {
	cfg, err := config.Load(o.opts.ConfigPath)
	if err != nil {
		return err
	}

	jobs, ledgerRoot, err := discovery.Discover(o.opts.InputPath, o.opts.OutputDir, o.opts.OutputExt)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return errors.New("no video files discovered")
	}

	ld, err := ledger.New(ledgerRoot)
	if err != nil {
		return err
	}
	jobPaths := make([]string, 0, len(jobs))
	for _, j := range jobs {
		jobPaths = append(jobPaths, j.InputPath)
	}
	if err := ld.InitJobs(jobPaths); err != nil {
		return err
	}

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
		fmt.Println("all jobs already marked done in ledger")
		return nil
	}

	fmt.Printf("discovered %d total jobs, %d pending\n", len(jobs), len(pending))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
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
			fmt.Printf("received signal %s, performing clean shutdown...\n", sig)
			cancel()
			o.cleanKill(context.Background(), ld, &activeMu, active)
		case res := <-results:
			inFlight--
			freeSlots = append(freeSlots, res.slot)
			if res.err != nil {
				fmt.Printf("job failed: %s (%v)\n", res.job.InputPath, res.err)
				if !o.opts.ContinueOnErr {
					cancel()
					o.cleanKill(context.Background(), ld, &activeMu, active)
				}
			} else {
				fmt.Printf("job done: %s -> %s\n", res.job.InputPath, res.job.OutputPath)
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

func (o *Orchestrator) runJob(ctx context.Context, job discovery.Job, sl slot, ld *ledger.Ledger, activeMu *sync.Mutex, active map[string]activeProc, results chan<- jobResult) {
	err := o.executeJob(ctx, job, sl, ld, activeMu, active)
	results <- jobResult{job: job, slot: sl, err: err}
}

func (o *Orchestrator) executeJob(ctx context.Context, job discovery.Job, sl slot, ld *ledger.Ledger, activeMu *sync.Mutex, active map[string]activeProc) error {
	w := sl.w
	node := sl.node
	if err := ld.Set(job.InputPath, ledger.StatusTransferring, node.Name, ""); err != nil {
		return err
	}

	if err := w.EnsureDir(ctx, node.TmpDir); err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "ensure tmp dir failed: "+err.Error())
		return err
	}

	jobID := jobIDFromPath(job.InputPath)
	remoteInput := filepath.ToSlash(filepath.Join(node.TmpDir, jobID+".input"))
	remoteOutput := filepath.ToSlash(filepath.Join(node.TmpDir, jobID+".output"))

	o.vlog("uploading %s to %s:%s", job.InputPath, node.Name, remoteInput)
	part, err := w.UploadAtomic(ctx, job.InputPath, remoteInput)
	if err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "upload failed: "+err.Error())
		return err
	}
	o.vlog("upload done for %s", job.InputPath)

	o.vlog("verifying md5 for %s", job.InputPath)
	localMD5, err := fileMD5(job.InputPath)
	if err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "local md5 failed: "+err.Error())
		_ = w.Remove(ctx, part, remoteInput)
		return err
	}
	remoteMD5, err := w.MD5(ctx, remoteInput)
	if err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "remote md5 failed: "+err.Error())
		_ = w.Remove(ctx, part, remoteInput)
		return err
	}
	if !strings.EqualFold(localMD5, remoteMD5) {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "md5 mismatch")
		_ = w.Remove(ctx, part, remoteInput)
		return fmt.Errorf("md5 mismatch for %s", job.InputPath)
	}
	o.vlog("md5 match for %s (%s)", job.InputPath, localMD5)

	cmd, err := renderCommand(node.Command, remoteInput, remoteOutput)
	if err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "render command failed: "+err.Error())
		_ = w.Remove(ctx, part, remoteInput)
		return err
	}

	o.vlog("starting remote command on %s: %s", node.Name, cmd)
	pid, err := w.StartCommand(ctx, cmd, sl.pidFile, sl.exitFile, sl.stderrLog)
	if err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "start command failed: "+err.Error())
		_ = w.Remove(ctx, part, remoteInput)
		return err
	}
	o.vlog("remote command started on %s with pid %d", node.Name, pid)

	activeMu.Lock()
	active[job.InputPath] = activeProc{job: job, sl: sl, pid: pid, part: part}
	activeMu.Unlock()
	defer func() {
		activeMu.Lock()
		delete(active, job.InputPath)
		activeMu.Unlock()
	}()

	if err := ld.Set(job.InputPath, ledger.StatusWorking, node.Name, ""); err != nil {
		return err
	}

	prefixed := &prefixWriter{prefix: "[" + node.Name + "] ", out: os.Stderr}
	exitCode, err := w.WaitForExit(ctx, pid, sl.exitFile, sl.stderrLog, o.opts.PollInterval, prefixed)
	if err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "wait failed: "+err.Error())
		_ = w.Remove(ctx, sl.pidFile)
		return err
	}
	if exitCode != 0 {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", fmt.Sprintf("remote exit code %d", exitCode))
		_ = w.Remove(ctx, sl.pidFile)
		return fmt.Errorf("remote command exit code %d", exitCode)
	}

	ok, err := w.FileExistsNonZero(ctx, remoteOutput)
	if err != nil {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "verify output failed: "+err.Error())
		return err
	}
	if !ok {
		_ = ld.Set(job.InputPath, ledger.StatusPending, "", "remote output missing or empty")
		return errors.New("remote output missing or empty")
	}

	localTmp := job.OutputPath + ".tmp"
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

	if err := w.Remove(ctx, part, remoteInput, remoteOutput, sl.pidFile, sl.exitFile, sl.stderrLog); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cleanup failed on %s: %v\n", node.Name, err)
	}

	if o.opts.DeleteSource {
		if err := os.Remove(job.InputPath); err != nil && !os.IsNotExist(err) {
			_ = ld.Set(job.InputPath, ledger.StatusFailed, node.Name, "delete source failed: "+err.Error())
			return err
		}
	}

	return ld.Set(job.InputPath, ledger.StatusDone, node.Name, "")
}

func (o *Orchestrator) cleanKill(ctx context.Context, ld *ledger.Ledger, activeMu *sync.Mutex, active map[string]activeProc) {
	activeMu.Lock()
	defer activeMu.Unlock()

	for _, p := range active {
		_ = p.sl.w.SignalTERM(ctx, p.pid)
		_ = p.sl.w.Remove(ctx, p.part, p.sl.pidFile)
		_ = ld.Set(p.job.InputPath, ledger.StatusPending, "", "interrupted")
	}
}

func (o *Orchestrator) vlog(format string, args ...any) {
	if o.opts.Verbose {
		fmt.Printf("[verbose] "+format+"\n", args...)
	}
}

func (o *Orchestrator) buildSlots(ctx context.Context, cfg *config.Config) ([]slot, error) {
	all := make([]slot, 0, len(cfg.Nodes))
	for _, n := range cfg.Nodes {
		o.vlog("checking node %s (%s)", n.Name, n.Address)
		var w worker.Worker
		if config.IsLocalAddress(n.Address) {
			o.vlog("node %s is local", n.Name)
			w = worker.NewLocal(n)
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
			w = worker.NewSSH(n)
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
	t, err := template.New("command").Parse(tpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	data := struct {
		Input  string
		Output string
	}{
		Input:  input,
		Output: output,
	}
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

func jobIDFromPath(path string) string {
	repl := strings.NewReplacer("/", "_", "\\", "_", ":", "_")
	return repl.Replace(path)
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
