# TUI Improvement with pterm Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transform the teleconvert CLI into a modern, styled TUI using pterm, including a WordArt banner and themed status reporting.

**Architecture:** Integrate pterm into the entry point for branding and the orchestrator for operational status. Refactor worker output prefixing to use color-coded labels.

**Tech Stack:** Go, github.com/pterm/pterm/v2

---

### Task 1: Add pterm Dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add dependency to go.mod**

Run: `go get github.com/pterm/pterm/v2`

- [ ] **Step 2: Verify dependency**

Run: `go mod tidy`

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add pterm dependency"
```

---

### Task 2: Implement Startup Banner

**Files:**
- Modify: `cmd/teleconvert/main.go`

- [ ] **Step 1: Add banner to main.go**

```go
package main

import (
	// ... existing imports
	"github.com/pterm/pterm/v2"
)

func main() {
	pterm.DefaultBigText.WithLetters(pterm.NewLettersFromString("TELECONVERT")).Render()
	pterm.DefaultSection.WithLevel(2).Println("Video Conversion Orchestrator")
    
    // ... rest of main
}
```

- [ ] **Step 2: Build and verify banner**

Run: `go build ./cmd/teleconvert && ./teleconvert -h`
Expected: BigText "TELECONVERT" followed by "Video Conversion Orchestrator" and then help text.

- [ ] **Step 3: Commit**

```bash
git add cmd/teleconvert/main.go
git commit -m "feat: add startup banner"
```

---

### Task 3: Refactor Orchestrator Logging & Spinner

**Files:**
- Modify: `internal/orchestrator/orchestrator.go`

- [ ] **Step 1: Update imports and init pterm**

```go
import (
    // ...
    "github.com/pterm/pterm/v2"
)

func (o *Orchestrator) Run(ctx context.Context) error {
    if o.opts.Verbose {
        pterm.EnableDebugMessages()
    }
    // ...
}
```

- [ ] **Step 2: Implement Spinner for Discovery**

```go
	spinner, _ := pterm.DefaultSpinner.Start("Discovering jobs...")
	jobs, ledgerRoot, err := discovery.Discover(o.opts.InputPath, o.opts.OutputDir, o.opts.OutputExt)
	if err != nil {
		spinner.Fail(err.Error())
		return err
	}
	// ... after InitJobs
	spinner.Success(fmt.Sprintf("Discovered %d total jobs", len(jobs)))
```

- [ ] **Step 3: Replace vlog with pterm.Debug**

```go
func (o *Orchestrator) vlog(format string, args ...any) {
	pterm.Debug.Printf(format, args...)
}
```

- [ ] **Step 4: Update Job Status Messages**

```go
// Replace fmt.Printf in Run loop
pterm.Info.Printf("pending: %d jobs\n", len(pending))

// In select case res := <-results:
if res.err != nil {
    pterm.Error.Printf("job failed: %s (%v)\n", res.job.InputPath, res.err)
} else {
    pterm.Success.Printf("job done: %s -> %s\n", res.job.InputPath, res.job.OutputPath)
}
```

- [ ] **Step 5: Run tests to ensure no regressions**

Run: `go test ./internal/orchestrator/...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/orchestrator/orchestrator.go
git commit -m "feat: integrate pterm logging and spinner"
```

---

### Task 4: Enhance Worker Output Prefixes

**Files:**
- Modify: `internal/orchestrator/orchestrator.go`

- [ ] **Step 1: Assign colors to nodes**

```go
var nodeColors = []pterm.Color{
	pterm.FgCyan,
	pterm.FgMagenta,
	pterm.FgBlue,
	pterm.FgYellow,
	pterm.FgGreen,
	pterm.FgRed,
}

func getColorForNode(name string) pterm.Color {
	sum := 0
	for _, r := range name {
		sum += int(r)
	}
	return nodeColors[sum%len(nodeColors)]
}
```

- [ ] **Step 2: Update prefixWriter to use pterm styles**

```go
func (o *Orchestrator) executeJob(...) error {
    // ...
    color := getColorForNode(node.Name)
	prefix := pterm.Color(color).Sprint(" " + node.Name + " ")
	prefixed := &prefixWriter{prefix: "[" + prefix + "] ", out: os.Stderr}
    // ...
}
```

- [ ] **Step 3: Manual Verification**

Run a job and verify the node prefix in stderr is colored.

- [ ] **Step 4: Commit**

```bash
git add internal/orchestrator/orchestrator.go
git commit -m "feat: color-coded worker output prefixes"
```
