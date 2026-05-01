# Comprehensive Edge-Case Testing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add robust edge-case coverage to ensure the tool handles unusual configurations and system errors gracefully.

**Architecture:** Use Go's `testing` package to add unit and integration tests. Mock file system and worker behavior where necessary.

**Tech Stack:** Go, testing, testify (if available, otherwise standard library)

---

### Task 1: Configuration Edge Cases

**Files:**
- Modify: `internal/config/config_test.go`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Write failing tests for MaxConcurrent and TmpDir defaults**

```go
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
```

- [ ] **Step 2: Run test to verify it fails (if logic is missing)**

Run: `go test -v ./internal/config -run TestValidateDefaults`

- [ ] **Step 3: Ensure logic is in config.go (Minimal implementation)**

Already implemented in `config.go`, but ensure it works as expected.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config_test.go
git commit -m "test: add config default validation tests"
```

---

### Task 2: Ledger Robustness

**Files:**
- Modify: `internal/ledger/ledger_test.go`
- Modify: `internal/ledger/ledger.go`

- [ ] **Step 1: Write failing test for malformed ledger file**

```go
func TestLedgerMalformedJSON(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ledger.json")
	os.WriteFile(path, []byte("{invalid json"), 0644)
	
	_, err := New(tmp)
	if err == nil {
		t.Error("expected error for malformed ledger JSON")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/ledger -run TestLedgerMalformedJSON`

- [ ] **Step 3: Implement minimal error handling in ledger.go**

```go
func New(root string) (*Ledger, error) {
    // ... load file ...
    if err := json.Unmarshal(b, &l.data); err != nil {
        return nil, fmt.Errorf("malformed ledger: %w", err)
    }
    // ...
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/ledger/ledger.go internal/ledger/ledger_test.go
git commit -m "feat: handle malformed ledger files"
```

---

### Task 3: Orchestrator Error Handling (MD5 Mismatch)

**Files:**
- Modify: `internal/orchestrator/orchestrator_test.go`
- Modify: `internal/orchestrator/orchestrator.go`

- [ ] **Step 1: Write failing test for MD5 mismatch**

```go
func (m *mockWorker) MD5(ctx context.Context, path string) (string, error) {
    return "mismatch", nil
}

func TestOrchestratorMD5Mismatch(t *testing.T) {
    // Setup orchestrator with mockWorker that returns wrong MD5
    // Verify job fails with "md5 mismatch" error in results
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/orchestrator -run TestOrchestratorMD5Mismatch`

- [ ] **Step 3: Verify executeJob handles mismatch (Minimal implementation)**

Already implemented, ensure it returns correct error.

- [ ] **Step 4: Commit**

```bash
git add internal/orchestrator/orchestrator_test.go
git commit -m "test: verify orchestrator handles md5 mismatch"
```

---

### Task 4: Worker Slot Race Stress Test

**Files:**
- Modify: `internal/orchestrator/orchestrator_test.go`

- [ ] **Step 1: Write stress test for concurrent jobs**

```go
func TestOrchestratorSlotStress(t *testing.T) {
    // Run 100 jobs with 2 slots
    // Use waitgroups to ensure all complete
}
```

- [ ] **Step 2: Run stress test with race detector**

Run: `go test -race -v ./internal/orchestrator -run TestOrchestratorSlotStress`

- [ ] **Step 3: Commit**

```bash
git add internal/orchestrator/orchestrator_test.go
git commit -m "test: add orchestrator slot stress test"
```
