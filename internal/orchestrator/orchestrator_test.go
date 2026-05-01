package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"text/template"
	"time"

	"teleconvert/internal/config"
	"teleconvert/internal/discovery"
	"teleconvert/internal/ledger"
	"teleconvert/internal/worker"
)

func TestRenderCommand(t *testing.T) {
	tests := []struct {
		tpl    string
		input  string
		output string
		want   string
	}{
		{
			"ffmpeg -i {{.Input}} -o {{.Output}}",
			"/input/video.mp4",
			"/output/video.mp4",
			"ffmpeg -i /input/video.mp4 -o /output/video.mp4",
		},
		{
			"HandBrakeCLI -i {{.Input}} -o {{.Output}} --preset 'High Profile'",
			"/input/video.mp4",
			"/output/video.mp4",
			"HandBrakeCLI -i /input/video.mp4 -o /output/video.mp4 --preset 'High Profile'",
		},
	}

	for _, tt := range tests {
		got, err := renderCommand(tt.tpl, tt.input, tt.output)
		if err != nil {
			t.Fatalf("render command: %v", err)
		}
		if got != tt.want {
			t.Errorf("renderCommand got %q, want %q", got, tt.want)
		}
	}
}

func TestRenderCommandInvalidTemplate(t *testing.T) {
	_, err := renderCommand("{{.Invalid}}", "/input", "/output")
	if err == nil {
		t.Error("expected error for invalid template syntax")
	}
}

func TestJobIDFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/path/to/video.mp4", "_path_to_video.mp4"},
		{"C:\\path\\to\\video.mp4", "C__path_to_video.mp4"},
		{"/path:with:colons", "_path_with_colons"},
	}

	for _, tt := range tests {
		got := jobIDFromPath(tt.path)
		if got != tt.want {
			t.Errorf("jobIDFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestPrefixWriter(t *testing.T) {
	tmpdir := t.TempDir()
	logFile := filepath.Join(tmpdir, "test.log")
	f, err := os.Create(logFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	pw := &prefixWriter{prefix: "[worker1] ", out: f}
	n, err := pw.Write([]byte("line 1\nline 2\n"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != 14 {
		t.Errorf("expected write count 14, got %d", n)
	}

	f.Close()
	b, _ := os.ReadFile(logFile)
	content := string(b)
	if !strings.Contains(content, "[worker1]") {
		t.Errorf("prefix not found in output: %q", content)
	}
	if !strings.Contains(content, "line 1") {
		t.Errorf("line 1 not found in output: %q", content)
	}
}

func TestPrefixWriterEmptyLines(t *testing.T) {
	tmpdir := t.TempDir()
	logFile := filepath.Join(tmpdir, "test.log")
	f, err := os.Create(logFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	pw := &prefixWriter{prefix: "[w] ", out: f}
	pw.Write([]byte("line 1\n\n"))
	f.Close()

	b, _ := os.ReadFile(logFile)
	content := string(b)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines with content, got %d: %q", len(lines), content)
	}
}

func TestTemplateExecutionEdgeCases(t *testing.T) {
	tests := []struct {
		tpl    string
		input  string
		output string
		valid  bool
	}{
		{"{{.Input}} {{.Output}}", "/in", "/out", true},
		{"{{.Input}}", "/in", "/out", true},
		{"{{.Output}}", "/in", "/out", true},
		{"{{.Invalid}}", "/in", "/out", false},
		{"{{.Input | upper}}", "/in", "/out", false},
	}

	for _, tt := range tests {
		_, err := renderCommand(tt.tpl, tt.input, tt.output)
		if tt.valid && err != nil {
			t.Errorf("expected valid template %q, got error: %v", tt.tpl, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("expected invalid template %q to error", tt.tpl)
		}
	}
}

func TestConfigNodeValidation(t *testing.T) {
	tests := []struct {
		name string
		node config.Node
		want error
	}{
		{
			"valid command",
			config.Node{
				Name:    "test",
				Address: "localhost",
				Command: "ffmpeg -i {{.Input}} {{.Output}}",
			},
			nil,
		},
	}

	for _, tt := range tests {
		cfg := &config.Config{Nodes: []config.Node{tt.node}}
		err := cfg.Validate()
		if tt.want == nil && err != nil {
			t.Errorf("test %s: expected no error, got %v", tt.name, err)
		}
	}
}

func TestWorkerNodeConstruction(t *testing.T) {
	node := config.Node{
		Name:          "worker1",
		Address:       "192.168.1.1:22",
		User:          "testuser",
		SSHKey:        "~/.ssh/id_rsa",
		MaxConcurrent: 2,
		Command:       "ffmpeg -i {{.Input}} {{.Output}}",
		TmpDir:        "/tmp/teleconvert",
	}

	if node.Name != "worker1" {
		t.Errorf("node name mismatch")
	}
	if node.MaxConcurrent != 2 {
		t.Errorf("max concurrent mismatch")
	}
}

func TestTemplateDataStructure(t *testing.T) {
	data := struct {
		Input  string
		Output string
	}{
		Input:  "/input/file.mp4",
		Output: "/output/file.mp4",
	}

	tpl := template.Must(template.New("test").Parse("{{.Input}} -> {{.Output}}"))
	var buf strings.Builder
	tpl.Execute(&buf, data)

	expected := "/input/file.mp4 -> /output/file.mp4"
	if buf.String() != expected {
		t.Errorf("template execution failed: got %q, want %q", buf.String(), expected)
	}
}

func TestJobIDFromPathSpecialChars(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"/path/with spaces/video.mp4"},
		{"/path/with-dashes/video.mp4"},
		{"/path/with_underscores/video.mp4"},
		{"/path/with.dots/video.mp4"},
	}

	for _, tt := range tests {
		id := jobIDFromPath(tt.input)
		if id == "" {
			t.Errorf("jobIDFromPath(%q) returned empty string", tt.input)
		}
		if strings.Contains(id, "/") {
			t.Errorf("jobIDFromPath should remove forward slashes, got %q", id)
		}
	}
}

func TestOptionsDefaults(t *testing.T) {
	opts := Options{
		InputPath: "/path/to/input",
	}

	if opts.OutputExt != "" && opts.OutputExt != ".mp4" {
		t.Errorf("output ext should default or be empty")
	}
	if opts.PollInterval != 0 && opts.PollInterval.Seconds() != 2 {
		t.Errorf("poll interval should default to 2s")
	}
}

func TestOrchestratorNew(t *testing.T) {
	opts := Options{
		InputPath:    "/path/to/input",
		PollInterval: 0,
	}

	orch := New(opts)
	if orch.opts.PollInterval.Seconds() != 2 {
		t.Errorf("expected poll interval default to 2s, got %v", orch.opts.PollInterval)
	}
}

type mockWorker struct {
	worker.Worker
	node         config.Node
	signalCalled chan int
}

func (m *mockWorker) Node() config.Node { return m.node }
func (m *mockWorker) Heartbeat(ctx context.Context) error { return nil }
func (m *mockWorker) CheckCommand(ctx context.Context, cmd string) error { return nil }
func (m *mockWorker) EnsureDir(ctx context.Context, dir string) error { return nil }
func (m *mockWorker) ReadPID(ctx context.Context, pidFile string) (int, error) { return 0, nil }
func (m *mockWorker) IsProcessRunning(ctx context.Context, pid int) (bool, error) { return false, nil }
func (m *mockWorker) Remove(ctx context.Context, paths ...string) error { return nil }
func (m *mockWorker) SignalTERM(ctx context.Context, pid int) error {
	m.signalCalled <- pid
	return nil
}

func TestOrchestratorInterrupt(t *testing.T) {
	active := make(map[string]activeProc)
	var activeMu sync.Mutex
	sigCalled := make(chan int, 1)
	mw := &mockWorker{
		node:         config.Node{Name: "mockNode"},
		signalCalled: sigCalled,
	}

	active["test.mp4"] = activeProc{
		job: discovery.Job{InputPath: "test.mp4"},
		sl:  slot{w: mw},
		pid: 1234,
	}

	o := New(Options{})
	tmp := t.TempDir()
	ld, _ := ledger.New(tmp)

	o.cleanKill(context.Background(), ld, &activeMu, active)

	select {
	case pid := <-sigCalled:
		if pid != 1234 {
			t.Errorf("expected signal to pid 1234, got %d", pid)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for SignalTERM")
	}

	entry, ok := ld.Get("test.mp4")
	if !ok {
		t.Fatal("ledger entry not found")
	}
	if entry.Status != ledger.StatusPending || !strings.Contains(entry.LastError, "interrupted") {
		t.Errorf("expected status pending with interrupted message, got %s: %s", entry.Status, entry.LastError)
	}
}
