package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Job struct {
	InputPath  string
	OutputPath string
}

var videoExt = map[string]struct{}{
	".mp4":  {},
	".mkv":  {},
	".mov":  {},
	".avi":  {},
	".wmv":  {},
	".webm": {},
	".m4v":  {},
	".flv":  {},
	".mpg":  {},
	".mpeg": {},
	".ts":   {},
}

func Discover(inputPath, outputDir, outputExt string) ([]Job, string, error) {
	inAbs, err := filepath.Abs(inputPath)
	if err != nil {
		return nil, "", fmt.Errorf("resolve input path: %w", err)
	}
	fi, err := os.Stat(inAbs)
	if err != nil {
		return nil, "", fmt.Errorf("stat input path: %w", err)
	}

	if outputExt == "" {
		outputExt = ".mp4"
	}
	if !strings.HasPrefix(outputExt, ".") {
		outputExt = "." + outputExt
	}

	if fi.IsDir() {
		if outputDir == "" {
			outputDir = filepath.Join(inAbs, "converted")
		}
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return nil, "", fmt.Errorf("create output dir: %w", err)
		}
		jobs, err := discoverDir(inAbs, outputDir, outputExt)
		if err != nil {
			return nil, "", err
		}
		return jobs, inAbs, nil
	}

	if outputDir == "" {
		outputDir = filepath.Dir(inAbs)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create output dir: %w", err)
	}
	base := strings.TrimSuffix(filepath.Base(inAbs), filepath.Ext(inAbs)) + outputExt
	return []Job{{
		InputPath:  inAbs,
		OutputPath: filepath.Join(outputDir, base),
	}}, filepath.Dir(inAbs), nil
}

func discoverDir(inputDir, outputDir, outputExt string) ([]Job, error) {
	jobs := make([]Job, 0, 64)
	err := filepath.WalkDir(inputDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == outputDir {
				return filepath.SkipDir
			}
			return nil
		}
		if _, ok := videoExt[strings.ToLower(filepath.Ext(d.Name()))]; !ok {
			return nil
		}
		rel, err := filepath.Rel(inputDir, path)
		if err != nil {
			return err
		}
		outRel := strings.TrimSuffix(rel, filepath.Ext(rel)) + outputExt
		outPath := filepath.Join(outputDir, outRel)
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		jobs = append(jobs, Job{InputPath: path, OutputPath: outPath})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover videos: %w", err)
	}
	return jobs, nil
}
