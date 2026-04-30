package worker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"teleconvert/internal/config"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type SSHWorker struct {
	node config.Node
}

func NewSSH(node config.Node) *SSHWorker {
	return &SSHWorker{node: node}
}

func (s *SSHWorker) Node() config.Node {
	return s.node
}

func (s *SSHWorker) Heartbeat(ctx context.Context) error {
	_, err := s.run(ctx, "ls /tmp >/dev/null")
	return err
}

func (s *SSHWorker) CheckCommand(ctx context.Context, cmd string) error {
	_, err := s.run(ctx, "which "+shellQuote(cmd))
	return err
}

func (s *SSHWorker) EnsureDir(ctx context.Context, dir string) error {
	_, err := s.run(ctx, "mkdir -p "+shellQuote(dir))
	return err
}

func (s *SSHWorker) ReadPID(ctx context.Context, pidFile string) (int, error) {
	out, err := s.run(ctx, "cat "+shellQuote(pidFile)+" 2>/dev/null || true")
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
	out, err := s.run(ctx, cmd)
	if err != nil {
		return false, err
	}
	return strings.Contains(out, "RUNNING"), nil
}

func (s *SSHWorker) UploadAtomic(ctx context.Context, localPath, remoteFinalPath string) (string, error) {
	client, sftpClient, err := s.connectSFTP(ctx)
	if err != nil {
		return "", err
	}
	defer client.Close()
	defer sftpClient.Close()

	remotePart := remoteFinalPath + ".part"
	if err := sftpClient.MkdirAll(filepath.ToSlash(filepath.Dir(remotePart))); err != nil {
		return "", err
	}

	in, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	defer in.Close()

	out, err := sftpClient.Create(filepath.ToSlash(remotePart))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if err := sftpClient.Rename(filepath.ToSlash(remotePart), filepath.ToSlash(remoteFinalPath)); err != nil {
		return "", err
	}
	return remotePart, nil
}

func (s *SSHWorker) MD5(ctx context.Context, path string) (string, error) {
	cmd := "md5sum " + shellQuote(path) + " | awk '{print $1}'"
	out, err := s.run(ctx, cmd)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (s *SSHWorker) StartCommand(ctx context.Context, command, pidFile, exitFile, stderrLog string) (int, error) {
	wrapped := fmt.Sprintf("set -e; rm -f %s %s; nohup sh -c %s >/dev/null 2>%s </dev/null & pid=$!; echo $pid > %s",
		shellQuote(pidFile),
		shellQuote(exitFile),
		shellQuote(command+"; ec=$?; echo $ec > "+shellQuote(exitFile)+"; exit $ec"),
		shellQuote(stderrLog),
		shellQuote(pidFile),
	)
	if _, err := s.run(ctx, wrapped); err != nil {
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
	out, err := s.run(ctx, "cat "+shellQuote(exitFile)+" 2>/dev/null || true")
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
	out, err := s.run(ctx, cmd)
	if err != nil {
		return false, err
	}
	return strings.Contains(out, "OK"), nil
}

func (s *SSHWorker) Download(ctx context.Context, remotePath, localPath string) error {
	client, sftpClient, err := s.connectSFTP(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	defer sftpClient.Close()

	in, err := sftpClient.Open(filepath.ToSlash(remotePath))
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	out, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
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
	_, err := s.run(ctx, "rm -f "+strings.Join(quoted, " "))
	return err
}

func (s *SSHWorker) SignalTERM(ctx context.Context, pid int) error {
	if pid <= 0 {
		return nil
	}
	_, err := s.run(ctx, fmt.Sprintf("kill -TERM %d >/dev/null 2>&1 || true", pid))
	return err
}

func (s *SSHWorker) streamRemoteLog(ctx context.Context, logPath string, sink io.Writer, offset *int64) error {
	if sink == nil {
		return nil
	}
	cmd := fmt.Sprintf("if [ -f %s ]; then tail -c +%d %s; fi", shellQuote(logPath), *offset+1, shellQuote(logPath))
	out, err := s.run(ctx, cmd)
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
