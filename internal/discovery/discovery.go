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
		useLocalConvertedDir := outputDir == ""
		if outputDir == "" {
			outputDir = filepath.Join(inAbs, "converted")
		}
		if !useLocalConvertedDir {
			if err := os.MkdirAll(outputDir, 0o755); err != nil {
				return nil, "", fmt.Errorf("create output dir: %w", err)
			}
		}
		jobs, err := discoverDir(inAbs, outputDir, outputExt, useLocalConvertedDir)
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

func discoverDir(inputDir, outputDir, outputExt string, useLocalConvertedDir bool) ([]Job, error) {
	jobs := make([]Job, 0, 64)
	err := filepath.WalkDir(inputDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if !useLocalConvertedDir && path == outputDir {
				return filepath.SkipDir
			}
			if d.Name() == "converted" {
				statusPath := filepath.Join(filepath.Dir(path), ".teleconvert_status.json")
				if _, err := os.Stat(statusPath); err == nil {
					return filepath.SkipDir
				} else if !os.IsNotExist(err) {
					return fmt.Errorf("stat status file %s: %w", statusPath, err)
				}
			}
			return nil
		}
		if _, ok := videoExt[strings.ToLower(filepath.Ext(d.Name()))]; !ok {
			return nil
		}
		var outPath string
		if useLocalConvertedDir {
			base := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())) + outputExt
			outPath = filepath.Join(filepath.Dir(path), "converted", base)
		} else {
			rel, err := filepath.Rel(inputDir, path)
			if err != nil {
				return err
			}
			outRel := strings.TrimSuffix(rel, filepath.Ext(rel)) + outputExt
			outPath = filepath.Join(outputDir, outRel)
		}
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
