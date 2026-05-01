# SSH Interrupt Handling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement unit tests to verify that `teleconvert` correctly handles `SIGINT` and signals remote SSH processes to terminate.

**Architecture:** Refactor `Orchestrator` to support dependency injection of workers and signal notifications. Implement a `MockWorker` to verify signal propagation.

**Tech Stack:** Go, standard `testing` package.

---

### Task 1: Refactor Orchestrator for Testability

**Files:**
- Modify: `internal/orchestrator/orchestrator.go`

- [ ] **Step 1: Add injection fields to Orchestrator struct**

```go
type Orchestrator struct {
	opts Options
	// New fields for testing
	workerFactory func(node config.Node, vlog func(string, ...any)) worker.Worker
	sigNotify     func(c chan<- os.Signal, sigs ...os.Signal)
}
```

- [ ] **Step 2: Update New() to initialize defaults**

```go
func New(opts Options) *Orchestrator {
	if opts.PollInterval == 0 {
		opts.PollInterval = 2 * time.Second
	}
	return &Orchestrator{
		opts: opts,
		workerFactory: func(node config.Node, vlog func(string, ...any)) worker.Worker {
			if node.Address == "localhost" {
				return worker.NewLocal(node, vlog)
			}
			return worker.NewSSH(node, vlog)
		},
		sigNotify: signal.Notify,
	}
}
```

- [ ] **Step 3: Use injected fields in Run() and buildSlots()**

In `Run()`:
```go
	sigCh := make(chan os.Signal, 1)
	o.sigNotify(sigCh, os.Interrupt, syscall.SIGTERM) // Use o.sigNotify
```

In `buildSlots()`:
```go
			// Replace direct NewLocal/NewSSH calls with o.workerFactory
			w := o.workerFactory(n, o.vlog)
```

- [ ] **Step 4: Verify project still compiles**

Run: `go build ./...`
Expected: PASS

---

### Task 2: Implement MockWorker and Interrupt Test

**Files:**
- Modify: `internal/orchestrator/orchestrator_test.go`

- [ ] **Step 1: Add MockWorker implementation to test file**

```go
type mockWorker struct {
	worker.Worker // Embedding for interface satisfaction
	signalCalled  chan int
	lastPID       int
	mu            sync.Mutex
}

func (m *mockWorker) Node() config.Node { return config.Node{Name: "mock"} }
func (m *mockWorker) SignalTERM(ctx context.Context, pid int) error {
	m.mu.Lock()
	m.lastPID = pid
	m.mu.Unlock()
	m.signalCalled <- pid
	return nil
}
func (m *mockWorker) Remove(ctx context.Context, paths ...string) error { return nil }
// ... other no-ops as needed
```

- [ ] **Step 2: Write TestOrchestratorInterrupt**

```go
func TestOrchestratorInterrupt(t *testing.T) {
	// 1. Setup Orchestrator with mock sigNotify and workerFactory
	// 2. Mock a job starting
	// 3. Trigger signal
	// 4. Verify mockWorker.SignalTERM was called
}
```

- [ ] **Step 3: Run the new test**

Run: `go test -v internal/orchestrator/orchestrator_test.go`
Expected: PASS

---

### Task 3: Test SSHWorker SignalTERM

**Files:**
- Create: `internal/worker/ssh_test.go`

- [ ] **Step 1: Implement TestSSHWorkerSignalTERM with mocked runner**

We need to mock the `run` method or the SSH client. Since `SSHWorker` uses a `run` helper, we might need a small refactor or just verify the command string construction.

```go
func TestSSHWorkerSignalTERM(t *testing.T) {
	// Verify that SignalTERM(ctx, 123) executes "kill -TERM 123 ..."
}
```

- [ ] **Step 2: Run the test**

Run: `go test -v internal/worker/ssh_test.go`
Expected: PASS

---

### Task 4: Final Verification and Commit

- [ ] **Step 1: Run all tests**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 2: Commit changes**

```bash
git add .
git commit -m "test: add unit tests for SSH interrupt handling and orchestrator signal routing"
```
