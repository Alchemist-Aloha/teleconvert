package worker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"teleconvert/internal/config"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type SSHWorker struct {
	node   config.Node
	vlog   func(string, ...any)
	runner func(ctx context.Context, command string) (string, error)
}

func NewSSH(node config.Node, vlog func(string, ...any)) *SSHWorker {
	s := &SSHWorker{node: node, vlog: vlog}
	s.runner = s.run
	return s
}

func (s *SSHWorker) Node() config.Node {
	return s.node
}

func (s *SSHWorker) Heartbeat(ctx context.Context) error {
	_, err := s.runner(ctx, "ls /tmp >/dev/null")
	return err
}

func (s *SSHWorker) CheckCommand(ctx context.Context, cmd string) error {
	_, err := s.runner(ctx, "which "+shellQuote(cmd))
	return err
}

func (s *SSHWorker) EnsureDir(ctx context.Context, dir string) error {
	_, err := s.runner(ctx, "mkdir -p "+shellQuote(dir))
	return err
}

func (s *SSHWorker) ReadPID(ctx context.Context, pidFile string) (int, error) {
	out, err := s.runner(ctx, "cat "+shellQuote(pidFile)+" 2>/dev/null || true")
	if err != nil {
		return 0, err
	}
	return parsePID(out)
}

func (s *SSHWorker) IsProcessRunning(ctx context.Context, pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	cmd := fmt.Sprintf("kill -0 %d >/dev/null 2>&1 && echo RUNNING || echo DEAD", pid)
	out, err := s.runner(ctx, cmd)
	if err != nil {
		return false, err
	}
	return strings.Contains(out, "RUNNING"), nil
}

func (s *SSHWorker) UploadAtomic(ctx context.Context, localPath, remoteFinalPath string) (string, error) {
	remotePart := remoteFinalPath + ".part"
	// Ensure remote directory exists
	if err := s.EnsureDir(ctx, filepath.Dir(remoteFinalPath)); err != nil {
		return "", err
	}

	addr := s.node.Address
	if !strings.Contains(addr, ":") {
		addr += ":22"
	}
	host, port, _ := net.SplitHostPort(addr)
	if port == "" {
		port = "22"
	}

	// Use rsync for high-speed transfer
	rsyncCmd := []string{
		"rsync", "-avz",
		"-e", fmt.Sprintf("ssh -i %s -p %s -o StrictHostKeyChecking=no", s.node.SSHKey, port),
		localPath,
		fmt.Sprintf("%s@%s:%s", s.node.User, host, remotePart),
	}

	if s.vlog != nil {
		s.vlog("running rsync: %s", strings.Join(rsyncCmd, " "))
	}

	cmd := exec.CommandContext(ctx, rsyncCmd[0], rsyncCmd[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return remotePart, commandOutputError("rsync upload", err, out)
	}

	// Rename part to final on remote
	if _, err := s.runner(ctx, fmt.Sprintf("mv %s %s", shellQuote(remotePart), shellQuote(remoteFinalPath))); err != nil {
		return remotePart, fmt.Errorf("remote rename failed: %w", err)
	}

	return remotePart, nil
}

type progressWriter struct {
	total   int64
	current int64
	vlog    func(string, ...any)
	lastLog time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.current += int64(len(b))
	if p.vlog != nil && time.Since(p.lastLog) > 5*time.Second {
		pct := float64(0)
		if p.total > 0 {
			pct = float64(p.current) / float64(p.total) * 100
		}
		p.vlog("progress: %.1f%% (%d/%d bytes)", pct, p.current, p.total)
		p.lastLog = time.Now()
	}
	return len(b), nil
}

func (s *SSHWorker) MD5(ctx context.Context, path string) (string, error) {
	cmd := "md5sum " + shellQuote(path) + " | awk '{print $1}'"
	out, err := s.runner(ctx, cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (s *SSHWorker) StartCommand(ctx context.Context, command, pidFile, exitFile, stderrLog string) (int, error) {
	// Capture both output streams. Encoder versions and wrapper scripts vary
	// in whether progress is written to stdout or stderr.
	wrapped := fmt.Sprintf("set -e; rm -f %s %s; nohup setsid sh -c %s >%s 2>&1 </dev/null & pid=$!; echo $pid > %s",
		shellQuote(pidFile),
		shellQuote(exitFile),
		shellQuote(command+"; ec=$?; echo $ec > "+shellQuote(exitFile)+"; exit $ec"),
		shellQuote(stderrLog),
		shellQuote(pidFile),
	)
	if s.vlog != nil {
		s.vlog("wrapped command: %s", wrapped)
	}
	if _, err := s.runner(ctx, wrapped); err != nil {
		return 0, err
	}
	pid, err := s.ReadPID(ctx, pidFile)
	if err != nil {
		return 0, err
	}
	if pid <= 0 {
		return 0, errors.New("remote pid file did not contain a valid pid")
	}
	return pid, nil
}

func (s *SSHWorker) WaitForExit(ctx context.Context, pid int, exitFile, stderrLog string, pollInterval time.Duration, stderrSink io.Writer) (int, error) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	offset := int64(0)
	for {
		if err := s.streamRemoteLog(ctx, stderrLog, stderrSink, &offset); err != nil {
			return 0, err
		}
		running, err := s.IsProcessRunning(ctx, pid)
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

	_ = s.streamRemoteLog(ctx, stderrLog, stderrSink, &offset)
	out, err := s.runner(ctx, "cat "+shellQuote(exitFile)+" 2>/dev/null || true")
	if err != nil {
		return 1, nil
	}
	exitCode, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 1, nil
	}
	return exitCode, nil
}

func (s *SSHWorker) FileExistsNonZero(ctx context.Context, path string) (bool, error) {
	cmd := "test -s " + shellQuote(path) + " && echo OK || true"
	out, err := s.runner(ctx, cmd)
	if err != nil {
		return false, err
	}
	return strings.Contains(out, "OK"), nil
}

func (s *SSHWorker) Download(ctx context.Context, remotePath, localPath string) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	addr := s.node.Address
	if !strings.Contains(addr, ":") {
		addr += ":22"
	}
	host, port, _ := net.SplitHostPort(addr)
	if port == "" {
		port = "22"
	}

	rsyncCmd := []string{
		"rsync", "-avz",
		"-e", fmt.Sprintf("ssh -i %s -p %s -o StrictHostKeyChecking=no", s.node.SSHKey, port),
		fmt.Sprintf("%s@%s:%s", s.node.User, host, remotePath),
		localPath,
	}

	if s.vlog != nil {
		s.vlog("running rsync: %s", strings.Join(rsyncCmd, " "))
	}

	cmd := exec.CommandContext(ctx, rsyncCmd[0], rsyncCmd[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return commandOutputError("rsync download", err, out)
	}
	return nil
}

func commandOutputError(operation string, err error, output []byte) error {
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("%s failed: %w", operation, err)
	}
	return fmt.Errorf("%s failed: %w (%s)", operation, err, detail)
}

func (s *SSHWorker) Remove(ctx context.Context, paths ...string) error {
	quoted := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		quoted = append(quoted, shellQuote(p))
	}
	if len(quoted) == 0 {
		return nil
	}
	_, err := s.runner(ctx, "rm -f "+strings.Join(quoted, " "))
	return err
}

func (s *SSHWorker) SignalTERM(ctx context.Context, pid int) error {
	if pid <= 0 {
		return nil
	}
	// StartCommand creates a separate session whose process group ID is the
	// recorded PID. Signal the group so child encoders do not survive their
	// wrapper shell. Escalate after five seconds if a process ignores SIGTERM.
	command := fmt.Sprintf(
		"kill -TERM -- -%d >/dev/null 2>&1 || kill -TERM %d >/dev/null 2>&1 || true; "+
			"i=0; while kill -0 -- -%d >/dev/null 2>&1 && [ $i -lt 50 ]; do sleep 0.1; i=$((i+1)); done; "+
			"kill -KILL -- -%d >/dev/null 2>&1 || true",
		pid, pid, pid, pid,
	)
	_, err := s.runner(ctx, command)
	return err
}

func (s *SSHWorker) streamRemoteLog(ctx context.Context, logPath string, sink io.Writer, offset *int64) error {
	if sink == nil {
		return nil
	}
	cmd := fmt.Sprintf("if [ -f %s ]; then tail -c +%d %s; fi", shellQuote(logPath), *offset+1, shellQuote(logPath))
	out, err := s.runner(ctx, cmd)
	if err != nil {
		return err
	}
	if out == "" {
		return nil
	}
	n, err := io.WriteString(sink, out)
	if err != nil {
		return err
	}
	*offset += int64(n)
	return nil
}

func (s *SSHWorker) connect(ctx context.Context) (*ssh.Client, error) {
	key, err := os.ReadFile(s.node.SSHKey)
	if err != nil {
		return nil, fmt.Errorf("read ssh key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse ssh key: %w", err)
	}
	cfg := &ssh.ClientConfig{
		User:            s.node.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	addr := s.node.Address
	if !strings.Contains(addr, ":") {
		addr += ":22"
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func (s *SSHWorker) connectSFTP(ctx context.Context) (*ssh.Client, *sftp.Client, error) {
	client, err := s.connect(ctx)
	if err != nil {
		return nil, nil, err
	}
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, nil, err
	}
	return client, sftpClient, nil
}

func (s *SSHWorker) run(ctx context.Context, command string) (string, error) {
	if s.vlog != nil {
		s.vlog("executing remote command: %s", command)
	}
	client, err := s.connect(ctx)
	if err != nil {
		return "", err
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()

	var b strings.Builder
	sess.Stdout = &b
	sess.Stderr = &b

	if err := sess.Start("sh -c " + shellQuote(command)); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() {
		done <- sess.Wait()
	}()

	select {
	case <-ctx.Done():
		_ = sess.Close()
		return "", ctx.Err()
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("remote command failed: %w (%s)", err, strings.TrimSpace(b.String()))
		}
		return b.String(), nil
	}
}

func readLines(r io.Reader) ([]string, error) {
	s := bufio.NewScanner(r)
	lines := make([]string, 0, 16)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}
