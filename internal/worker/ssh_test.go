package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"teleconvert/internal/config"
)

func TestSSHWorkerSignalTERM(t *testing.T) {
	var capturedCmd string
	w := NewSSH(config.Node{Name: "test"}, nil)
	w.runner = func(ctx context.Context, command string) (string, error) {
		capturedCmd = command
		return "", nil
	}

	ctx := context.Background()
	err := w.SignalTERM(ctx, 1234)
	if err != nil {
		t.Fatalf("SignalTERM failed: %v", err)
	}

	for _, expected := range []string{"kill -TERM -- -1234", "kill -KILL -- -1234"} {
		if !strings.Contains(capturedCmd, expected) {
			t.Errorf("expected command to contain %q, got %q", expected, capturedCmd)
		}
	}
}

func TestSSHWorkerStartCommandCreatesProcessGroup(t *testing.T) {
	var commands []string
	w := NewSSH(config.Node{Name: "test"}, nil)
	w.runner = func(ctx context.Context, command string) (string, error) {
		commands = append(commands, command)
		if strings.HasPrefix(command, "cat ") {
			return "1234\n", nil
		}
		return "", nil
	}

	pid, err := w.StartCommand(context.Background(), "ffmpeg -i input output", "/tmp/job.pid", "/tmp/job.exit", "/tmp/job.log")
	if err != nil {
		t.Fatalf("StartCommand failed: %v", err)
	}
	if pid != 1234 {
		t.Fatalf("expected pid 1234, got %d", pid)
	}
	if len(commands) == 0 || !strings.Contains(commands[0], "nohup setsid sh -c") {
		t.Fatalf("expected command to start a separate process group, got %q", commands)
	}
}

func TestCommandOutputErrorPreservesCapturedRsyncDetail(t *testing.T) {
	err := commandOutputError(
		"rsync upload",
		errors.New("exit status 23"),
		[]byte("sending incremental file list\npermission denied\n"),
	)
	for _, want := range []string{"rsync upload failed", "exit status 23", "permission denied"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not contain %q", err, want)
		}
	}
}
