package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
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
			"ffmpeg -i '/input/video.mp4' -o '/output/video.mp4'",
		},
		{
			"HandBrakeCLI -i {{.Input}} -o {{.Output}} --preset 'High Profile'",
			"/input/video.mp4",
			"/output/video.mp4",
			"HandBrakeCLI -i '/input/video.mp4' -o '/output/video.mp4' --preset 'High Profile'",
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

func TestOrchestratorSlotStress(t *testing.T) {
	tmpdir := t.TempDir()

	// Create some input files
	inputDir := filepath.Join(tmpdir, "input")
	os.MkdirAll(inputDir, 0755)
	for i := 0; i < 100; i++ {
		os.WriteFile(filepath.Join(inputDir, fmt.Sprintf("file%d.mp4", i)), []byte("data"), 0644)
	}

	configPath := filepath.Join(tmpdir, "config.yaml")
	content := `nodes:
  - name: "worker1"
    address: "localhost"
    command: "ffmpeg -i {{.Input}} {{.Output}}"
    max_concurrent: 2
    tmp_dir: "/tmp"
`
	os.WriteFile(configPath, []byte(content), 0644)

	mw := &mockWorker{
		node: config.Node{Name: "worker1", MaxConcurrent: 2, TmpDir: "/tmp"},
		md5Func: func(ctx context.Context, path string) (string, error) {
			return fileMD5(path)
		},
	}

	o := New(Options{
		ConfigPath:    configPath,
		InputPath:     inputDir,
		PollInterval:  10 * time.Millisecond,
		ContinueOnErr: true,
		Verbose:       true,
	})
	o.workerFactory = func(node config.Node, vlog func(string, ...any)) worker.Worker {
		return mw
	}

	// We need to mock signal notify to avoid hanging
	o.sigNotify = func(c chan<- os.Signal, sig ...os.Signal) {}

	err := o.Run(context.Background())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Wait a bit for filesystem to catch up
	time.Sleep(200 * time.Millisecond)

	// Verify all jobs are done in ledger
	ld, _ := ledger.New(inputDir)
	snap := ld.Snapshot()
	t.Logf("Discovered %d jobs in ledger", len(snap.Jobs))
	if len(snap.Jobs) != 100 {
		t.Errorf("expected 100 jobs in ledger, got %d", len(snap.Jobs))
	}
	statusPath := filepath.Join(inputDir, ".teleconvert_status.json")
	if _, err := os.Stat(statusPath); err != nil {
		t.Errorf("expected ledger in source folder at %s: %v", statusPath, err)
	}
	convertedStatusPath := filepath.Join(inputDir, "converted", ".teleconvert_status.json")
	if _, err := os.Stat(convertedStatusPath); !os.IsNotExist(err) {
		t.Errorf("ledger must not be stored in converted folder: %s", convertedStatusPath)
	}
	for path, entry := range snap.Jobs {
		if entry.Status != ledger.StatusDone {
			t.Errorf("job %s expected done, got %s (Error: %q)", path, entry.Status, entry.LastError)
		}
	}

	if t.Failed() {
		b, _ := os.ReadFile(statusPath)
		t.Logf("Raw Ledger file: %s", string(b))
	}
}

func TestJobIDFromPath(t *testing.T) {
	first := jobIDFromPath("/path/to/video.mp4")
	second := jobIDFromPath("/other/path/to/video.mp4")
	if first == second {
		t.Fatal("different paths must produce different job IDs")
	}
	for _, id := range []string{first, second} {
		if len(id) != len("teleconvert-")+32 || !strings.HasPrefix(id, "teleconvert-") {
			t.Errorf("expected short stable hashed job ID, got %q", id)
		}
	}
}

func TestRenderCommandShellQuotesSpecialCharacters(t *testing.T) {
	got, err := renderCommand(
		"ffmpeg -i {{.Input}} {{.Output}}",
		"/tmp/a file's $(touch BAD).mp4",
		"/tmp/out file.mkv",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "ffmpeg -i '/tmp/a file'\"'\"'s $(touch BAD).mp4' '/tmp/out file.mkv'"
	if got != want {
		t.Errorf("renderCommand got %q, want %q", got, want)
	}
}

func TestRemoteJobPathsPreserveSafeExtensions(t *testing.T) {
	job := discovery.Job{
		InputPath:  "/media/a very long 'name' [1080p].MP4",
		OutputPath: "/media/converted/a very long 'name' [1080p].mkv",
	}
	input, output := remoteJobPaths("/tmp/worker files", job)
	if !strings.HasSuffix(input, ".input.mp4") {
		t.Errorf("input extension not preserved: %s", input)
	}
	if !strings.HasSuffix(output, ".output.mkv") {
		t.Errorf("output extension not preserved: %s", output)
	}
	if len(filepath.Base(input)) > 80 || len(filepath.Base(output)) > 80 {
		t.Errorf("remote names should remain short: %s, %s", input, output)
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
	md5Func      func(ctx context.Context, path string) (string, error)
	uploadFunc   func(ctx context.Context, local, remote string) (string, error)
	removeFunc   func(ctx context.Context, paths ...string) error
	startFunc    func(ctx context.Context, command, pidFile, exitFile, stderrLog string) (int, error)
}

func (m *mockWorker) Node() config.Node                                           { return m.node }
func (m *mockWorker) Heartbeat(ctx context.Context) error                         { return nil }
func (m *mockWorker) CheckCommand(ctx context.Context, cmd string) error          { return nil }
func (m *mockWorker) EnsureDir(ctx context.Context, dir string) error             { return nil }
func (m *mockWorker) ReadPID(ctx context.Context, pidFile string) (int, error)    { return 1, nil }
func (m *mockWorker) IsProcessRunning(ctx context.Context, pid int) (bool, error) { return false, nil }
func (m *mockWorker) StartCommand(ctx context.Context, command, pidFile, exitFile, stderrLog string) (int, error) {
	if m.startFunc != nil {
		return m.startFunc(ctx, command, pidFile, exitFile, stderrLog)
	}
	return 1, nil
}
func (m *mockWorker) WaitForExit(ctx context.Context, pid int, exitFile, stderrLog string, pollInterval time.Duration, stderrSink io.Writer) (int, error) {
	return 0, nil
}
func (m *mockWorker) FileExistsNonZero(ctx context.Context, path string) (bool, error) {
	return true, nil
}
func (m *mockWorker) Download(ctx context.Context, remote, local string) error {
	return os.WriteFile(local, []byte("output"), 0644)
}
func (m *mockWorker) Remove(ctx context.Context, paths ...string) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, paths...)
	}
	return nil
}
func (m *mockWorker) SignalTERM(ctx context.Context, pid int) error {
	if m.signalCalled != nil {
		m.signalCalled <- pid
	}
	return nil
}
func (m *mockWorker) UploadAtomic(ctx context.Context, local, remote string) (string, error) {
	if m.uploadFunc != nil {
		return m.uploadFunc(ctx, local, remote)
	}
	data, err := os.ReadFile(local)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(remote, data, 0644); err != nil {
		return "", err
	}
	return remote + ".part", nil
}
func (m *mockWorker) MD5(ctx context.Context, path string) (string, error) {
	if m.md5Func != nil {
		return m.md5Func(ctx, path)
	}
	return "", nil
}

func TestOrchestratorMD5Mismatch(t *testing.T) {
	tmpdir := t.TempDir()
	inputPath := filepath.Join(tmpdir, "test.mp4")
	os.WriteFile(inputPath, []byte("video data"), 0644)

	mw := &mockWorker{
		node: config.Node{Name: "worker1", TmpDir: "/tmp"},
		md5Func: func(ctx context.Context, path string) (string, error) {
			return "wrong-md5", nil
		},
	}

	o := New(Options{})
	o.workerFactory = func(node config.Node, vlog func(string, ...any)) worker.Worker {
		return mw
	}

	ld, _ := ledger.New(tmpdir)
	job := discovery.Job{InputPath: inputPath, OutputPath: inputPath + ".out"}

	active := make(map[string]activeProc)
	var activeMu sync.Mutex

	err := o.executeJob(context.Background(), job, slot{w: mw, node: mw.node}, ld, &activeMu, active)
	if err == nil {
		t.Fatal("expected error for MD5 mismatch")
	}
	if !strings.Contains(err.Error(), "md5 mismatch") {
		t.Errorf("expected error to contain 'md5 mismatch', got %v", err)
	}

	entry, _ := ld.Get(inputPath)
	if !strings.Contains(entry.LastError, "md5 mismatch") {
		t.Errorf("expected ledger to record md5 mismatch, got %q", entry.LastError)
	}
}

func TestOrchestratorSuccessfulJobCleansTemporaryFiles(t *testing.T) {
	tmpdir := t.TempDir()
	inputPath := filepath.Join(tmpdir, "test.mp4")
	outputPath := filepath.Join(tmpdir, "converted", "test.mkv")
	remoteDir := filepath.Join(tmpdir, "remote")
	for _, dir := range []string{filepath.Dir(outputPath), remoteDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(inputPath, []byte("video data"), 0o644); err != nil {
		t.Fatal(err)
	}

	var removed []string
	mw := &mockWorker{
		node: config.Node{Name: "worker1", TmpDir: remoteDir, Command: "ffmpeg {{.Input}} {{.Output}}"},
		md5Func: func(ctx context.Context, path string) (string, error) {
			return fileMD5(path)
		},
		removeFunc: func(ctx context.Context, paths ...string) error {
			removed = append(removed, paths...)
			return nil
		},
	}
	o := New(Options{})
	ld, _ := ledger.New(tmpdir)
	job := discovery.Job{InputPath: inputPath, OutputPath: outputPath}
	sl := slot{
		w:         mw,
		node:      mw.node,
		pidFile:   filepath.Join(remoteDir, "job.pid"),
		exitFile:  filepath.Join(remoteDir, "job.exit"),
		stderrLog: filepath.Join(remoteDir, "job.log"),
	}
	active := make(map[string]activeProc)
	var activeMu sync.Mutex

	if err := o.executeJob(context.Background(), job, sl, ld, &activeMu, active); err != nil {
		t.Fatalf("execute job: %v", err)
	}

	remoteInput, remoteOutput := remoteJobPaths(remoteDir, job)
	expectedRemoved := []string{
		remoteInput + ".part",
		remoteInput,
		remoteOutput,
		sl.pidFile,
		sl.exitFile,
		sl.stderrLog,
	}
	for _, expected := range expectedRemoved {
		if !containsString(removed, expected) {
			t.Errorf("expected temporary file %s to be removed; got %v", expected, removed)
		}
	}
	if _, err := os.Stat(outputPath + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("local temporary output was not removed: %s.tmp", outputPath)
	}
}

func TestLocalEncoderUsesSourceFileWithoutStagingCopy(t *testing.T) {
	tmpdir := t.TempDir()
	inputPath := filepath.Join(tmpdir, "source media's file.mp4")
	outputPath := filepath.Join(tmpdir, "converted", "source media's file.mkv")
	workerTmp := filepath.Join(tmpdir, "worker-tmp")
	for _, dir := range []string{filepath.Dir(outputPath), workerTmp} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(inputPath, []byte("video data"), 0o644); err != nil {
		t.Fatal(err)
	}

	uploadCalls := 0
	md5Calls := 0
	var command string
	var removed []string
	mw := &mockWorker{
		node: config.Node{
			Name:    "local-encoder",
			Address: "localhost",
			TmpDir:  workerTmp,
			Command: "ffmpeg -i {{.Input}} {{.Output}}",
		},
		uploadFunc: func(ctx context.Context, local, remote string) (string, error) {
			uploadCalls++
			return "", errors.New("local input must not be uploaded")
		},
		md5Func: func(ctx context.Context, path string) (string, error) {
			md5Calls++
			return "", errors.New("local input must not be checksummed as a transfer")
		},
		startFunc: func(ctx context.Context, cmd, pidFile, exitFile, stderrLog string) (int, error) {
			command = cmd
			return 1, nil
		},
		removeFunc: func(ctx context.Context, paths ...string) error {
			removed = append(removed, paths...)
			return nil
		},
	}
	o := New(Options{})
	ld, _ := ledger.New(tmpdir)
	job := discovery.Job{InputPath: inputPath, OutputPath: outputPath}
	sl := slot{
		w:         mw,
		node:      mw.node,
		pidFile:   filepath.Join(workerTmp, "job.pid"),
		exitFile:  filepath.Join(workerTmp, "job.exit"),
		stderrLog: filepath.Join(workerTmp, "job.log"),
	}
	active := make(map[string]activeProc)
	var activeMu sync.Mutex

	if err := o.executeJob(context.Background(), job, sl, ld, &activeMu, active); err != nil {
		t.Fatalf("execute local job: %v", err)
	}
	if uploadCalls != 0 {
		t.Errorf("local worker staged the source %d times", uploadCalls)
	}
	if md5Calls != 0 {
		t.Errorf("local worker performed %d transfer checksums", md5Calls)
	}
	if !strings.Contains(command, shellQuote(inputPath)) {
		t.Errorf("local command does not use source path directly: %s", command)
	}
	_, expectedTmpOutput := remoteJobPaths(workerTmp, job)
	if !strings.Contains(command, shellQuote(expectedTmpOutput)) {
		t.Errorf("local command does not write to worker tmp directory: %s", command)
	}
	if containsString(removed, inputPath) {
		t.Errorf("source path must never be included in temporary cleanup: %v", removed)
	}
	if !containsString(removed, expectedTmpOutput) {
		t.Errorf("temporary encoder output was not cleaned: %v", removed)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

func TestOrchestratorInterruptRecoversPIDFromFile(t *testing.T) {
	active := make(map[string]activeProc)
	var activeMu sync.Mutex
	sigCalled := make(chan int, 1)
	mw := &mockWorker{
		node:         config.Node{Name: "mockNode"},
		signalCalled: sigCalled,
	}

	active["test.mp4"] = activeProc{
		job: discovery.Job{InputPath: "test.mp4"},
		sl:  slot{w: mw, pidFile: "/tmp/teleconvert.pid"},
	}

	o := New(Options{})
	ld, _ := ledger.New(t.TempDir())
	o.cleanKill(context.Background(), ld, &activeMu, active)

	select {
	case pid := <-sigCalled:
		if pid != 1 {
			t.Errorf("expected recovered pid 1, got %d", pid)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SignalTERM")
	}
}
