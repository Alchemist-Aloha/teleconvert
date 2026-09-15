package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"text/tabwriter"

	"teleconvert/internal/config"
)

const configUsage = `Usage:
  teleconvert config list   [-config PATH]
  teleconvert config add    -name NAME -address ADDR [-user U] [-ssh-key KEY]
                            [-max-concurrent N] [-command CMD] [-tmp-dir DIR] [-config PATH]
  teleconvert config set    -name NAME [-address ADDR] [-user U] [-ssh-key KEY]
                            [-max-concurrent N] [-command CMD] [-tmp-dir DIR] [-config PATH]
  teleconvert config remove -name NAME [-config PATH]

Edits worker node definitions in the teleconvert YAML config. Flags omitted on
"set" leave the existing value untouched. Saving rewrites the whole file, so
YAML comments are not preserved.`

func runConfigCmd(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, configUsage)
		return errors.New("missing subcommand")
	}
	switch args[0] {
	case "list", "ls":
		return listNodes(args[1:])
	case "add":
		return mutateNode(args[1:], true)
	case "set", "update", "edit":
		return mutateNode(args[1:], false)
	case "remove", "rm", "delete":
		return removeNode(args[1:])
	case "help", "-h", "--help":
		fmt.Println(configUsage)
		return nil
	}
	fmt.Fprintln(os.Stderr, configUsage)
	return fmt.Errorf("unknown subcommand %q", args[0])
}

func listNodes(args []string) error {
	fs := flag.NewFlagSet("config list", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath(), "Path to teleconvert YAML config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tADDRESS\tUSER\tCONC\tCOMMAND")
	for _, n := range cfg.Nodes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", n.Name, n.Address, n.User, n.MaxConcurrent, n.Command)
	}
	return w.Flush()
}

func mutateNode(args []string, add bool) error {
	fs := flag.NewFlagSet("config mutate", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath(), "Path to teleconvert YAML config")
	name := fs.String("name", "", "Node name")
	address := fs.String("address", "", "Node address (host:port, or localhost)")
	user := fs.String("user", "", "SSH user")
	sshKey := fs.String("ssh-key", "", "SSH private key path")
	maxConcurrent := fs.Int("max-concurrent", 0, "Max concurrent jobs on this node")
	command := fs.String("command", "", "Encoder command template with {{.Input}} and {{.Output}}")
	tmpDir := fs.String("tmp-dir", "", "Remote temp directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return errors.New("-name is required")
	}

	visited := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })
	apply := func(flagName string) bool { return add || visited[flagName] }

	cfg, err := loadForEdit(*cfgPath)
	if err != nil {
		return err
	}

	var node *config.Node
	for i := range cfg.Nodes {
		if cfg.Nodes[i].Name == *name {
			node = &cfg.Nodes[i]
			break
		}
	}
	if add {
		if node != nil {
			return fmt.Errorf("node %q already exists; use \"config set\"", *name)
		}
		cfg.Nodes = append(cfg.Nodes, config.Node{Name: *name})
		node = &cfg.Nodes[len(cfg.Nodes)-1]
	} else if node == nil {
		return fmt.Errorf("no node named %q in %s", *name, *cfgPath)
	}

	if apply("address") {
		node.Address = *address
	}
	if apply("user") {
		node.User = *user
	}
	if apply("ssh-key") {
		node.SSHKey = *sshKey
	}
	if apply("max-concurrent") {
		node.MaxConcurrent = *maxConcurrent
	}
	if apply("command") {
		node.Command = *command
	}
	if apply("tmp-dir") {
		node.TmpDir = *tmpDir
	}

	if err := config.Save(*cfgPath, cfg); err != nil {
		return err
	}
	if add {
		fmt.Printf("added node %q to %s\n", *name, *cfgPath)
	} else {
		fmt.Printf("updated node %q in %s\n", *name, *cfgPath)
	}
	return nil
}

func removeNode(args []string) error {
	fs := flag.NewFlagSet("config remove", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultConfigPath(), "Path to teleconvert YAML config")
	name := fs.String("name", "", "Node name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		return errors.New("-name is required")
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	kept := make([]config.Node, 0, len(cfg.Nodes))
	for _, n := range cfg.Nodes {
		if n.Name != *name {
			kept = append(kept, n)
		}
	}
	if len(kept) == len(cfg.Nodes) {
		return fmt.Errorf("no node named %q in %s", *name, *cfgPath)
	}
	cfg.Nodes = kept
	if err := config.Save(*cfgPath, cfg); err != nil {
		return err
	}
	fmt.Printf("removed node %q from %s\n", *name, *cfgPath)
	return nil
}

// loadForEdit returns the config at path, or an empty config when the file
// does not exist yet (so "add" can create it).
func loadForEdit(path string) (*config.Config, error) {
	cfg, err := config.Load(path)
	if err == nil {
		return cfg, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return &config.Config{}, nil
	}
	return nil, err
}
