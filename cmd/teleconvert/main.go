package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"teleconvert/internal/config"
	"teleconvert/internal/orchestrator"
)

func main() {
	var opts orchestrator.Options

	flag.StringVar(&opts.ConfigPath, "config", config.DefaultConfigPath(), "Path to teleconvert YAML config")
	flag.StringVar(&opts.InputPath, "input", "", "Input file or directory")
	flag.StringVar(&opts.OutputDir, "output-dir", "", "Output directory (default: input/converted for directory input)")
	flag.StringVar(&opts.OutputExt, "output-ext", ".mp4", "Output extension")
	flag.BoolVar(&opts.DeleteSource, "delete-source", false, "Delete source file after successful conversion")
	flag.BoolVar(&opts.ContinueOnErr, "continue-on-error", true, "Continue processing remaining jobs when a job fails")
	poll := flag.Duration("poll-interval", 2*time.Second, "Remote process poll interval")
	flag.Parse()

	if opts.InputPath == "" {
		fmt.Fprintln(os.Stderr, "-input is required")
		flag.Usage()
		os.Exit(2)
	}
	opts.PollInterval = *poll

	ctx := context.Background()
	orch := orchestrator.New(opts)
	if err := orch.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "teleconvert: %v\n", err)
		os.Exit(1)
	}
}
