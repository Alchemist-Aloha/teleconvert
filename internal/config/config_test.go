package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	tmpdir := t.TempDir()
	configPath := filepath.Join(tmpdir, "teleconvert.yaml")
	content := `nodes:
  - name: "workstation-01"
    address: "192.168.88.230:22"
    user: "kent"
    ssh_key: "~/.ssh/id_rsa"
    max_concurrent: 2
    command: "ffmpeg -i {{.Input}} -c:v libx265 -crf 22 {{.Output}}"
    tmp_dir: "/tmp/teleconvert"
  - name: "local-node"
    address: "localhost"
    command: "ffmpeg -i {{.Input}} -c:v libx264 {{.Output}}"
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(cfg.Nodes))
	}
	if cfg.Nodes[0].Name != "workstation-01" {
		t.Errorf("expected name workstation-01, got %s", cfg.Nodes[0].Name)
	}
	if cfg.Nodes[0].MaxConcurrent != 2 {
		t.Errorf("expected max_concurrent 2, got %d", cfg.Nodes[0].MaxConcurrent)
	}
	if cfg.Nodes[1].TmpDir == "" {
		t.Errorf("expected default tmp_dir, got empty")
	}
}

func TestValidateNodeMissingName(t *testing.T) {
	cfg := &Config{
		Nodes: []Node{
			{Address: "localhost", Command: "ffmpeg -i {{.Input}} {{.Output}}"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for missing name")
	}
}

func TestValidateNodeMissingCommand(t *testing.T) {
	cfg := &Config{
		Nodes: []Node{
			{Name: "test", Address: "localhost"},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for missing command")
	}
}

func TestValidateNodeMissingPlaceholders(t *testing.T) {
	cfg := &Config{
		Nodes: []Node{
			{
				Name:    "test",
				Address: "localhost",
				Command: "ffmpeg -i /input.mp4",
			},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for missing {{.Input}} or {{.Output}}")
	}
}

func TestValidateDuplicateNodeNames(t *testing.T) {
	cfg := &Config{
		Nodes: []Node{
			{
				Name:    "test",
				Address: "localhost",
				Command: "ffmpeg -i {{.Input}} {{.Output}}",
			},
			{
				Name:    "test",
				Address: "127.0.0.1",
				Command: "ffmpeg -i {{.Input}} {{.Output}}",
			},
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for duplicate node names")
	}
}

func TestValidateDefaults(t *testing.T) {
	cfg := &Config{
		Nodes: []Node{
			{
				Name:    "test",
				Address: "localhost",
				Command: "ffmpeg -i {{.Input}} {{.Output}}",
				MaxConcurrent: 0,
				TmpDir: "",
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if cfg.Nodes[0].MaxConcurrent != 1 {
		t.Errorf("expected MaxConcurrent 1, got %d", cfg.Nodes[0].MaxConcurrent)
	}
	if cfg.Nodes[0].TmpDir != "/tmp/teleconvert" {
		t.Errorf("expected TmpDir /tmp/teleconvert, got %q", cfg.Nodes[0].TmpDir)
	}
}

func TestIsLocalAddress(t *testing.T) {
	tests := []struct {
		addr     string
		expected bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"localhost:22", true},
		{"127.0.0.1:2222", true},
		{"192.168.1.1", false},
		{"example.com", false},
		{"example.com:22", false},
	}
	for _, tt := range tests {
		got := IsLocalAddress(tt.addr)
		if got != tt.expected {
			t.Errorf("IsLocalAddress(%q) = %v, want %v", tt.addr, got, tt.expected)
		}
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	tests := []struct {
		input    string
		contains string
	}{
		{"~/.ssh/id_rsa", home},
		{"/tmp/file", "/tmp/file"},
	}
	for _, tt := range tests {
		got := expandHome(tt.input)
		if !contains(got, tt.contains) {
			t.Errorf("expandHome(%q) = %q, should contain %q", tt.input, got, tt.contains)
		}
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestLoadEmptyConfig(t *testing.T) {
	tmpdir := t.TempDir()
	configPath := filepath.Join(tmpdir, "teleconvert.yaml")
	if err := os.WriteFile(configPath, []byte("nodes: []"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(configPath)
	if err == nil {
		t.Error("expected error for empty nodes list")
	}
}
