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

var version = "v1.2.0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "config" {
		if err := runConfigCmd(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "teleconvert config: %v\n", err)
			os.Exit(1)
		}
		return
	}

	var opts orchestrator.Options
	showVersion := flag.Bool("version", false, "Print version and exit")

	flag.StringVar(&opts.ConfigPath, "config", config.DefaultConfigPath(), "Path to teleconvert YAML config (or run 'teleconvert config' to edit)")
	flag.StringVar(&opts.InputPath, "input", "", "Input file or directory")
	flag.StringVar(&opts.OutputDir, "output-dir", "", "Output directory (default: converted beside each source file)")
	flag.StringVar(&opts.OutputExt, "output-ext", ".mp4", "Output extension")
	flag.BoolVar(&opts.DeleteSource, "delete-source", false, "Delete source file after successful conversion")
	flag.BoolVar(&opts.ContinueOnErr, "continue-on-error", true, "Continue processing remaining jobs when a job fails")
	flag.BoolVar(&opts.Verbose, "verbose", false, "Enable verbose logging")
	flag.BoolVar(&opts.Verbose, "v", false, "Enable verbose logging (shorthand)")
	flag.BoolVar(&opts.LocalOnly, "local", false, "Only use the local machine for conversion")
	poll := flag.Duration("poll-interval", 2*time.Second, "Remote process poll interval")
	flag.Parse()

	if *showVersion {
		fmt.Printf("teleconvert %s\n", version)
		return
	}

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
