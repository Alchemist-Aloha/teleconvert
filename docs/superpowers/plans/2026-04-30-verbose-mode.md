# Verbose Mode & Diagnostics Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `--verbose` flag to enable detailed logging and improve remote command diagnostics.

**Architecture:** 
1. Add `Verbose` to `orchestrator.Options` and propagate it.
2. Update `main.go` to parse the new flag.
3. Fix `prefixWriter` in `orchestrator.go` to handle partial lines and prefixing correctly.
4. Add verbose logging to `Orchestrator` methods (`executeJob`, `buildSlots`).
5. Add pre-flight command existence check in `buildSlots`.

**Tech Stack:** Go (Standard Library)

---

### Task 1: Update Options and Main

**Files:**
- Modify: `internal/orchestrator/orchestrator.go`
- Modify: `cmd/teleconvert/main.go`

- [ ] **Step 1: Add Verbose to Options**

In `internal/orchestrator/orchestrator.go`:
```go
type Options struct {
    // ... existing fields
    Verbose       bool
}
```

- [ ] **Step 2: Add flag to main.go**

In `cmd/teleconvert/main.go`:
```go
flag.BoolVar(&opts.Verbose, "verbose", false, "Enable verbose logging")
// ...
flag.BoolVar(&opts.Verbose, "v", false, "Enable verbose logging (shorthand)")
```

- [ ] **Step 3: Commit**

```bash
git add internal/orchestrator/orchestrator.go cmd/teleconvert/main.go
git commit -m "feat: add verbose flag to options and main"
```

### Task 2: Refactor prefixWriter

**Files:**
- Modify: `internal/orchestrator/orchestrator.go`

- [ ] **Step 1: Fix prefixWriter to handle partial lines**

```go
type prefixWriter struct {
	prefix string
	out    io.Writer
	buf    []byte
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	p.buf = append(p.buf, b...)
	for {
		idx := -1
		for i, c := range p.buf {
			if c == '\n' {
				idx = i
				break
			}
		}
		if idx == -1 {
			break
		}
		line := p.buf[:idx+1]
		if _, err := fmt.Fprint(p.out, p.prefix); err != nil {
			return 0, err
		}
		if _, err := p.out.Write(line); err != nil {
			return 0, err
		}
		p.buf = p.buf[idx+1:]
	}
	return len(b), nil
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/orchestrator/orchestrator.go
git commit -m "fix: improve prefixWriter to handle partial lines correctly"
```

### Task 3: Add Verbose Logging to Orchestrator

**Files:**
- Modify: `internal/orchestrator/orchestrator.go`

- [ ] **Step 1: Add logging to executeJob**

Log upload, checksum, command start, and download.

- [ ] **Step 2: Add logging to buildSlots**

Log slot creation and health checks.

- [ ] **Step 3: Commit**

```bash
git add internal/orchestrator/orchestrator.go
git commit -m "feat: add verbose logging to orchestrator"
```

### Task 4: Add Pre-flight Command Check

**Files:**
- Modify: `internal/orchestrator/orchestrator.go`

- [ ] **Step 1: Verify command exists on remote in buildSlots**

Add logic to parse the command name and run `which <cmd>` via the worker.

- [ ] **Step 2: Commit**

```bash
git add internal/orchestrator/orchestrator.go
git commit -m "feat: add pre-flight check for remote command existence"
```
