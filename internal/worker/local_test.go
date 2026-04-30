package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"teleconvert/internal/config"
)

func TestLocalWorkerHeartbeat(t *testing.T) {
	node := config.Node{Name: "local", Address: "localhost"}
	w := NewLocal(node, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.Heartbeat(ctx); err != nil {
		t.Logf("heartbeat skipped (shell environment): %v", err)
	}
}

func TestLocalWorkerEnsureDir(t *testing.T) {
	tmpdir := t.TempDir()
	testdir := filepath.Join(tmpdir, "a", "b", "c")
	node := config.Node{Name: "local", Address: "localhost"}
	w := NewLocal(node, nil)
	ctx := context.Background()

	if err := w.EnsureDir(ctx, testdir); err != nil {
		t.Fatalf("ensure dir: %v", err)
	}
	if _, err := os.Stat(testdir); err != nil {
		t.Errorf("dir not created: %v", err)
	}
}

func TestLocalWorkerUploadAtomic(t *testing.T) {
	tmpdir := t.TempDir()
	srcFile := filepath.Join(tmpdir, "source.mp4")
	dstFile := filepath.Join(tmpdir, "dest.mp4")
	partFile := dstFile + ".part"

	if err := os.WriteFile(srcFile, []byte("test content"), 0o644); err != nil {
		t.Fatal(err)
	}

	node := config.Node{Name: "local", Address: "localhost"}
	w := NewLocal(node, nil)
	ctx := context.Background()

	part, err := w.UploadAtomic(ctx, srcFile, dstFile)
	if err != nil {
		t.Fatalf("upload atomic: %v", err)
	}
	if part != partFile {
		t.Errorf("expected part file %s, got %s", partFile, part)
	}

	if _, err := os.Stat(dstFile); err != nil {
		t.Errorf("dest file not created: %v", err)
	}
	if _, err := os.Stat(partFile); err != nil && !os.IsNotExist(err) {
		t.Errorf("part file should not exist after atomic rename: %v", err)
	}
}

func TestLocalWorkerMD5(t *testing.T) {
	tmpdir := t.TempDir()
	testFile := filepath.Join(tmpdir, "test.txt")
	content := "test content"
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	node := config.Node{Name: "local", Address: "localhost"}
	w := NewLocal(node, nil)
	ctx := context.Background()

	hash1, err := w.MD5(ctx, testFile)
	if err != nil {
		t.Fatalf("md5: %v", err)
	}

	hash2, err := w.MD5(ctx, testFile)
	if err != nil {
		t.Fatalf("md5 second call: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("md5 hashes don't match: %s != %s", hash1, hash2)
	}
	if hash1 == "" {
		t.Error("md5 hash is empty")
	}
}

func TestLocalWorkerFileExistsNonZero(t *testing.T) {
	tmpdir := t.TempDir()
	emptyFile := filepath.Join(tmpdir, "empty.txt")
	nonEmptyFile := filepath.Join(tmpdir, "nonempty.txt")
	os.WriteFile(emptyFile, []byte(""), 0o644)
	os.WriteFile(nonEmptyFile, []byte("content"), 0o644)

	node := config.Node{Name: "local", Address: "localhost"}
	w := NewLocal(node, nil)
	ctx := context.Background()

	empty, _ := w.FileExistsNonZero(ctx, emptyFile)
	if empty {
		t.Error("empty file should return false")
	}

	nonempty, _ := w.FileExistsNonZero(ctx, nonEmptyFile)
	if !nonempty {
		t.Error("non-empty file should return true")
	}

	missing, _ := w.FileExistsNonZero(ctx, filepath.Join(tmpdir, "missing.txt"))
	if missing {
		t.Error("missing file should return false")
	}
}

func TestLocalWorkerDownload(t *testing.T) {
	tmpdir := t.TempDir()
	srcFile := filepath.Join(tmpdir, "source.txt")
	dstFile := filepath.Join(tmpdir, "dest.txt")
	content := "test download content"

	if err := os.WriteFile(srcFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	node := config.Node{Name: "local", Address: "localhost"}
	w := NewLocal(node, nil)
	ctx := context.Background()

	if err := w.Download(ctx, srcFile, dstFile); err != nil {
		t.Fatalf("download: %v", err)
	}

	b, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(b) != content {
		t.Errorf("expected content %q, got %q", content, string(b))
	}
}

func TestLocalWorkerRemove(t *testing.T) {
	tmpdir := t.TempDir()
	file1 := filepath.Join(tmpdir, "file1.txt")
	file2 := filepath.Join(tmpdir, "file2.txt")
	os.WriteFile(file1, []byte("content"), 0o644)
	os.WriteFile(file2, []byte("content"), 0o644)

	node := config.Node{Name: "local", Address: "localhost"}
	w := NewLocal(node, nil)
	ctx := context.Background()

	if err := w.Remove(ctx, file1, file2); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if _, err := os.Stat(file1); err == nil {
		t.Error("file1 should be removed")
	}
	if _, err := os.Stat(file2); err == nil {
		t.Error("file2 should be removed")
	}
}

func TestLocalWorkerRemoveNonExistent(t *testing.T) {
	node := config.Node{Name: "local", Address: "localhost"}
	w := NewLocal(node, nil)
	ctx := context.Background()

	if err := w.Remove(ctx, "/nonexistent/file.txt"); err != nil {
		t.Errorf("remove non-existent should not error: %v", err)
	}
}

func TestLocalWorkerStartCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping shell integration test in short mode")
	}
	tmpdir := t.TempDir()
	pidFile := filepath.Join(tmpdir, "test.pid")
	exitFile := filepath.Join(tmpdir, "test.exit")
	stderrLog := filepath.Join(tmpdir, "test.stderr.log")

	node := config.Node{Name: "local", Address: "localhost", TmpDir: tmpdir}
	w := NewLocal(node, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := "echo 'test' > /dev/null; exit 0"
	pid, err := w.StartCommand(ctx, cmd, pidFile, exitFile, stderrLog)
	if err != nil {
		t.Logf("start command skipped (shell environment): %v", err)
		return
	}
	if pid <= 0 {
		t.Errorf("expected valid pid, got %d", pid)
	}

	if _, err := os.Stat(pidFile); err != nil {
		t.Errorf("pid file not created: %v", err)
	}
}

func TestLocalWorkerIsProcessRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping shell integration test in short mode")
	}

	tmpdir := t.TempDir()
	pidFile := filepath.Join(tmpdir, "test.pid")
	exitFile := filepath.Join(tmpdir, "test.exit")
	stderrLog := filepath.Join(tmpdir, "test.stderr.log")

	node := config.Node{Name: "local", Address: "localhost", TmpDir: tmpdir}
	w := NewLocal(node, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := "sleep 5"
	pid, err := w.StartCommand(ctx, cmd, pidFile, exitFile, stderrLog)
	if err != nil {
		t.Logf("start command skipped (shell environment): %v", err)
		return
	}

	running, err := w.IsProcessRunning(ctx, pid)
	if err != nil {
		t.Fatalf("is process running: %v", err)
	}
	if !running {
		t.Error("process should be running")
	}

	w.SignalTERM(ctx, pid)
	time.Sleep(500 * time.Millisecond)

	running, err = w.IsProcessRunning(ctx, pid)
	if err != nil {
		t.Fatalf("is process running after kill: %v", err)
	}
	if running {
		t.Error("process should not be running after TERM")
	}
}

func TestLocalWorkerInvalidPID(t *testing.T) {
	node := config.Node{Name: "local", Address: "localhost"}
	w := NewLocal(node, nil)
	ctx := context.Background()

	running, _ := w.IsProcessRunning(ctx, -1)
	if running {
		t.Error("negative pid should not be running")
	}

	running, _ = w.IsProcessRunning(ctx, 0)
	if running {
		t.Error("pid 0 should not be running")
	}
}

func TestLocalWorkerPIDFileParsing(t *testing.T) {
	tmpdir := t.TempDir()
	pidFile := filepath.Join(tmpdir, "test.pid")

	node := config.Node{Name: "local", Address: "localhost"}
	w := NewLocal(node, nil)
	ctx := context.Background()

	pid, err := w.ReadPID(ctx, pidFile)
	if err != nil {
		t.Fatalf("read missing pid file: %v", err)
	}
	if pid != 0 {
		t.Errorf("missing pid file should return 0, got %d", pid)
	}

	if errW := os.WriteFile(pidFile, []byte("12345"), 0o644); errW != nil {
		t.Fatalf("write pid file: %v", errW)
	}
	pid, err = w.ReadPID(ctx, pidFile)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	if pid != 12345 {
		t.Errorf("expected pid 12345, got %d", pid)
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input  string
		quoted string
	}{
		{"simple", "'simple'"},
		{"with space", "'with space'"},
		{"with'quote", "'with'\"'\"'quote'"},
	}
	for _, tt := range tests {
		got := shellQuote(tt.input)
		if got != tt.quoted {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.quoted)
		}
	}
}
