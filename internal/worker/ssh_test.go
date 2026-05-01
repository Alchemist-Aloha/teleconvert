package worker

import (
	"context"
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

	expected := "kill -TERM 1234"
	if !strings.Contains(capturedCmd, expected) {
		t.Errorf("expected command to contain %q, got %q", expected, capturedCmd)
	}
}
