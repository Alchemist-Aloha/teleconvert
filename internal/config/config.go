package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v4"
)

type Config struct {
	Nodes []Node `yaml:"nodes"`
}

type Node struct {
	Name          string `yaml:"name"`
	Address       string `yaml:"address"`
	User          string `yaml:"user"`
	SSHKey        string `yaml:"ssh_key"`
	MaxConcurrent int    `yaml:"max_concurrent"`
	Command       string `yaml:"command"`
	TmpDir        string `yaml:"tmp_dir"`
}

func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "teleconvert.yaml"
	}
	return filepath.Join(home, ".config", "teleconvert", "teleconvert.yaml")
}

func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	path = expandHome(path)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	for i := range cfg.Nodes {
		cfg.Nodes[i].SSHKey = expandHome(cfg.Nodes[i].SSHKey)
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	if len(c.Nodes) == 0 {
		return errors.New("config must contain at least one node")
	}
	seen := make(map[string]struct{}, len(c.Nodes))
	for i, n := range c.Nodes {
		if n.Name == "" {
			return fmt.Errorf("nodes[%d].name is required", i)
		}
		if _, ok := seen[n.Name]; ok {
			return fmt.Errorf("duplicate node name: %s", n.Name)
		}
		seen[n.Name] = struct{}{}
		if n.Address == "" {
			return fmt.Errorf("nodes[%d].address is required", i)
		}
		if n.Command == "" {
			return fmt.Errorf("nodes[%d].command is required", i)
		}
		if !strings.Contains(n.Command, "{{.Input}}") || !strings.Contains(n.Command, "{{.Output}}") {
			return fmt.Errorf("nodes[%d].command must contain {{.Input}} and {{.Output}} placeholders", i)
		}
		if n.MaxConcurrent <= 0 {
			c.Nodes[i].MaxConcurrent = 1
		}
		if n.TmpDir == "" {
			c.Nodes[i].TmpDir = "/tmp/teleconvert"
		}
	}
	return nil
}

func IsLocalAddress(addr string) bool {
	a := strings.ToLower(strings.TrimSpace(addr))
	if a == "localhost" || a == "127.0.0.1" || a == "::1" {
		return true
	}
	if strings.HasPrefix(a, "localhost:") || strings.HasPrefix(a, "127.0.0.1:") || strings.HasPrefix(a, "::1:") {
		return true
	}
	return false
}

func expandHome(p string) string {
	if p == "" {
		return p
	}
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[2:])
}
