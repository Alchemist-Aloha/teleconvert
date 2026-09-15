package main

import (
	"path/filepath"
	"testing"

	"teleconvert/internal/config"
)

const validCommand = "ffmpeg -i {{.Input}} {{.Output}}"

func editPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "teleconvert.yaml")
}

func readConfig(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func TestConfigAddSetRemove(t *testing.T) {
	path := editPath(t)

	if err := runConfigCmd([]string{"add", "-config", path, "-name", "a", "-address", "localhost", "-command", validCommand}); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := runConfigCmd([]string{"add", "-config", path, "-name", "b", "-address", "host:22", "-user", "u", "-command", validCommand, "-max-concurrent", "3", "-ssh-key", "/k"}); err != nil {
		t.Fatalf("add b: %v", err)
	}
	cfg := readConfig(t, path)
	if len(cfg.Nodes) != 2 || cfg.Nodes[1].MaxConcurrent != 3 || cfg.Nodes[1].User != "u" {
		t.Fatalf("unexpected nodes after add: %+v", cfg.Nodes)
	}
	if cfg.Nodes[0].MaxConcurrent != 1 || cfg.Nodes[0].TmpDir == "" {
		t.Errorf("add should apply defaults, got %+v", cfg.Nodes[0])
	}

	// set touches only the flags given
	if err := runConfigCmd([]string{"set", "-config", path, "-name", "a", "-max-concurrent", "5"}); err != nil {
		t.Fatalf("set a: %v", err)
	}
	cfg = readConfig(t, path)
	if cfg.Nodes[0].MaxConcurrent != 5 {
		t.Errorf("max_concurrent not updated: %+v", cfg.Nodes[0])
	}
	if cfg.Nodes[0].Address != "localhost" || cfg.Nodes[0].Command != validCommand {
		t.Errorf("set clobbered untouched fields: %+v", cfg.Nodes[0])
	}

	if err := runConfigCmd([]string{"remove", "-config", path, "-name", "b"}); err != nil {
		t.Fatalf("remove b: %v", err)
	}
	cfg = readConfig(t, path)
	if len(cfg.Nodes) != 1 || cfg.Nodes[0].Name != "a" {
		t.Fatalf("unexpected nodes after remove: %+v", cfg.Nodes)
	}
}

func TestConfigEditErrors(t *testing.T) {
	path := editPath(t)
	if err := runConfigCmd([]string{"add", "-config", path, "-name", "a", "-address", "localhost", "-command", validCommand}); err != nil {
		t.Fatal(err)
	}

	cases := [][]string{
		{"add", "-config", path, "-name", "a", "-address", "localhost", "-command", validCommand}, // duplicate
		{"add", "-config", path, "-name", "c", "-address", "localhost"},                           // missing command
		{"add", "-config", path, "-name", "c", "-address", "localhost", "-command", "ffmpeg"},     // missing placeholders
		{"add", "-config", path, "-address", "localhost", "-command", validCommand},               // missing name
		{"set", "-config", path, "-name", "missing", "-max-concurrent", "2"},                      // unknown node
		{"remove", "-config", path, "-name", "missing"},                                           // unknown node
		{"remove", "-config", path, "-name", "a"},                                                 // last node
		{"bogus", "-config", path},                                                                // unknown subcommand
	}
	for _, args := range cases {
		if err := runConfigCmd(args); err == nil {
			t.Errorf("expected error for %v", args)
		}
	}
	if len(readConfig(t, path).Nodes) != 1 {
		t.Errorf("failed edits must not modify the config")
	}
}
