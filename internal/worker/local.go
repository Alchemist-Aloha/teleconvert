package worker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"teleconvert/internal/config"
)

type LocalWorker struct {
	node config.Node
}

func NewLocal(node config.Node) *LocalWorker {
	return &LocalWorker{node: node}
}

func (l *LocalWorker) Node() config.Node {
	return l.node
}

func (l *LocalWorker) Heartbeat(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "sh", "-lc", "ls /tmp >/dev/null")
	return cmd.Run()
}

func (l *LocalWorker) EnsureDir(ctx context.Context, dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func (l *LocalWorker) ReadPID(ctx context.Context, pidFile string) (int, error) {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return parsePID(string(b))
}

func (l *LocalWorker) IsProcessRunning(ctx context.Context, pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false, err
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false, nil
	}
	if strings.Contains(err.Error(), "no such process") {
		return false, nil
	}
	return false, err
}

func (l *LocalWorker) UploadAtomic(ctx context.Context, localPath, remoteFinalPath string) (string, error) {
	remotePart := remoteFinalPath + ".part"
	if err := copyFile(localPath, remotePart); err != nil {
		return "", err
	}
	if err := os.Rename(remotePart, remoteFinalPath); err != nil {
		return "", err
	}
	return remotePart, nil
}

func (l *LocalWorker) MD5(ctx context.Context, path string) (string, error) {
	return localMD5(path)
}

func (l *LocalWorker) StartCommand(ctx context.Context, command, pidFile, exitFile, stderrLog string) (int, error) {
	if err := os.MkdirAll(filepath.Dir(stderrLog), 0o755); err != nil {
		return 0, err
	}
	wrapped := fmt.Sprintf("set -e; rm -f %s %s; nohup sh -lc %s >/dev/null 2>%s </dev/null & pid=$!; echo $pid > %s",
		shellQuote(pidFile),
		shellQuote(exitFile),
		shellQuote(command+"; ec=$?; echo $ec > "+shellQuote(exitFile)+"; exit $ec"),
		shellQuote(stderrLog),
		shellQuote(pidFile),
	)
	cmd := exec.CommandContext(ctx, "sh", "-lc", wrapped)
	if out, err := cmd.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("start local command: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	pid, err := l.ReadPID(ctx, pidFile)
	if err != nil {
		return 0, err
	}
	if pid <= 0 {
		return 0, errors.New("local pid file did not contain a valid pid")
	}
	return pid, nil
}

func (l *LocalWorker) WaitForExit(ctx context.Context, pid int, exitFile, stderrLog string, pollInterval time.Duration, stderrSink io.Writer) (int, error) {
	var offset int64
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if err := streamNewLog(stderrLog, stderrSink, &offset); err != nil {
			return 0, err
		}
		running, err := l.IsProcessRunning(ctx, pid)
		if err != nil {
			return 0, err
		}
		if !running {
			break
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
		}
	}

	_ = streamNewLog(stderrLog, stderrSink, &offset)
	b, err := os.ReadFile(exitFile)
	if err != nil {
		return 1, nil
	}
	exitCode, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 1, nil
	}
	return exitCode, nil
}

func (l *LocalWorker) FileExistsNonZero(ctx context.Context, path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return fi.Size() > 0, nil
}

func (l *LocalWorker) Download(ctx context.Context, remotePath, localPath string) error {
	return copyFile(remotePath, localPath)
}

func (l *LocalWorker) Remove(ctx context.Context, paths ...string) error {
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (l *LocalWorker) SignalTERM(ctx context.Context, pid int) error {
	if pid <= 0 {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := p.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func streamNewLog(logPath string, sink io.Writer, offset *int64) error {
	if sink == nil {
		return nil
	}
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	if _, err := f.Seek(*offset, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReader(f)
	written := int64(0)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			n, wErr := io.WriteString(sink, line)
			if wErr != nil {
				return wErr
			}
			written += int64(n)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	*offset += written
	return nil
}
