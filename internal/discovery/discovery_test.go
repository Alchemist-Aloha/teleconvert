package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSingleFile(t *testing.T) {
	tmpdir := t.TempDir()
	videoFile := filepath.Join(tmpdir, "test.mp4")
	if err := os.WriteFile(videoFile, []byte("fake video"), 0o644); err != nil {
		t.Fatal(err)
	}

	jobs, root, err := Discover(videoFile, "", "")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].InputPath != videoFile {
		t.Errorf("expected input %s, got %s", videoFile, jobs[0].InputPath)
	}
	if root != tmpdir {
		t.Errorf("expected root %s, got %s", tmpdir, root)
	}
}

func TestDiscoverDirectory(t *testing.T) {
	tmpdir := t.TempDir()
	subdir := filepath.Join(tmpdir, "videos")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	videos := []string{"video1.mp4", "video2.mkv", "video3.avi"}
	for _, v := range videos {
		path := filepath.Join(subdir, v)
		if err := os.WriteFile(path, []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	jobs, root, err := Discover(subdir, "", "")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(jobs) != 3 {
		t.Errorf("expected 3 jobs, got %d", len(jobs))
	}
	if root != subdir {
		t.Errorf("expected root %s, got %s", subdir, root)
	}
}

func TestDiscoverIgnoresNonVideoFiles(t *testing.T) {
	tmpdir := t.TempDir()
	videoPath := filepath.Join(tmpdir, "video.mp4")
	textPath := filepath.Join(tmpdir, "readme.txt")
	if err := os.WriteFile(videoPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(textPath, []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}

	jobs, _, err := Discover(tmpdir, "", "")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job (video only), got %d", len(jobs))
	}
}

func TestDiscoverCustomOutputExt(t *testing.T) {
	tmpdir := t.TempDir()
	videoPath := filepath.Join(tmpdir, "video.mp4")
	if err := os.WriteFile(videoPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	jobs, _, err := Discover(videoPath, "", ".mkv")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !endsWith(jobs[0].OutputPath, ".mkv") {
		t.Errorf("expected output to end with .mkv, got %s", jobs[0].OutputPath)
	}
}

func TestDiscoverOutputDir(t *testing.T) {
	tmpdir := t.TempDir()
	videoPath := filepath.Join(tmpdir, "video.mp4")
	outdir := filepath.Join(tmpdir, "output")
	if err := os.WriteFile(videoPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	jobs, _, err := Discover(videoPath, outdir, "")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !startsWith(jobs[0].OutputPath, outdir) {
		t.Errorf("expected output in %s, got %s", outdir, jobs[0].OutputPath)
	}
	if _, err := os.Stat(outdir); err != nil {
		t.Errorf("expected output dir to be created: %v", err)
	}
}

func TestDiscoverRecursiveStructure(t *testing.T) {
	tmpdir := t.TempDir()
	subdir := filepath.Join(tmpdir, "season1", "episode1")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(subdir, "episode.mp4")
	if err := os.WriteFile(videoPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	jobs, _, err := Discover(tmpdir, "", ".mkv")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job from nested dir, got %d", len(jobs))
	}
	expectedOutput := filepath.Join(subdir, "converted", "episode.mkv")
	if jobs[0].OutputPath != expectedOutput {
		t.Errorf("expected output %s, got %s", expectedOutput, jobs[0].OutputPath)
	}
}

func TestDiscoverExplicitOutputDirPreservesRecursiveStructure(t *testing.T) {
	tmpdir := t.TempDir()
	inputDir := filepath.Join(tmpdir, "input")
	subdir := filepath.Join(inputDir, "season1", "episode1")
	outputDir := filepath.Join(tmpdir, "output")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(subdir, "episode.mp4")
	if err := os.WriteFile(videoPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	jobs, _, err := Discover(inputDir, outputDir, ".mkv")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	expectedOutput := filepath.Join(outputDir, "season1", "episode1", "episode.mkv")
	if len(jobs) != 1 || jobs[0].OutputPath != expectedOutput {
		t.Fatalf("expected output %s, got %+v", expectedOutput, jobs)
	}
}

func TestDiscoverSkipsConvertedDirWithSiblingStatusFile(t *testing.T) {
	tmpdir := t.TempDir()
	sourceDir := filepath.Join(tmpdir, "A", "B", "C")
	convertedDir := filepath.Join(sourceDir, "converted")
	if err := os.MkdirAll(convertedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(sourceDir, "D.mp4")
	convertedPath := filepath.Join(convertedDir, "D.mkv")
	for _, path := range []string{sourcePath, convertedPath} {
		if err := os.WriteFile(path, []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(sourceDir, ".teleconvert_status.json"), []byte(`{"version":1,"jobs":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	jobs, _, err := Discover(tmpdir, "", ".mkv")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(jobs) != 1 || jobs[0].InputPath != sourcePath {
		t.Fatalf("expected only source media %s, got %+v", sourcePath, jobs)
	}
}

func TestDiscoverIncludesConvertedDirWithoutSiblingStatusFile(t *testing.T) {
	tmpdir := t.TempDir()
	convertedDir := filepath.Join(tmpdir, "archive", "converted")
	if err := os.MkdirAll(convertedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	videoPath := filepath.Join(convertedDir, "original.mp4")
	if err := os.WriteFile(videoPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	jobs, _, err := Discover(tmpdir, "", ".mkv")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(jobs) != 1 || jobs[0].InputPath != videoPath {
		t.Fatalf("expected converted-named source directory to be included, got %+v", jobs)
	}
}

func TestDiscoverNonExistentPath(t *testing.T) {
	_, _, err := Discover("/nonexistent/path/video.mp4", "", "")
	if err == nil {
		t.Error("expected error for nonexistent path")
	}
}

func TestDiscoverMixedVideoFormats(t *testing.T) {
	tmpdir := t.TempDir()
	formats := []string{".mp4", ".mkv", ".mov", ".avi", ".webm", ".ts"}
	for _, fmt := range formats {
		path := filepath.Join(tmpdir, "video"+fmt)
		if err := os.WriteFile(path, []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	jobs, _, err := Discover(tmpdir, "", "")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(jobs) != len(formats) {
		t.Errorf("expected %d jobs for all formats, got %d", len(formats), len(jobs))
	}
}

func endsWith(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
