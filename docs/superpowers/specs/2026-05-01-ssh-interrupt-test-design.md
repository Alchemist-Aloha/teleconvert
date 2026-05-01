# Design: SSH Interrupt Handling Verification

Date: 2026-05-01
Topic: Robustness / Signal Handling

## Goal
Ensure that `teleconvert` properly handles keyboard interrupts (SIGINT/Ctrl+C) by signaling remote SSH processes to terminate cleanly, preventing orphaned transcoding jobs.

## Proposed Changes

### 1. Orchestrator Refactoring
Modify `internal/orchestrator/orchestrator.go` to support dependency injection for testing.
- Introduce `workerFactory` field to `Orchestrator` to allow mocking worker creation.
- Add an internal `sigNotify` field (defaulting to `signal.Notify`) to allow signal injection in tests.

### 2. Testing Infrastructure
- **MockWorker**: Implement a mock worker that satisfies the `worker.Worker` interface and records calls to `SignalTERM`.
- **TestOrchestratorInterrupt**: A test that starts a job, injects a SIGINT, and verifies that the worker's `SignalTERM` is called.

### 3. SSHWorker Verification
- **TestSSHWorkerSignalTERM**: Verify that `SSHWorker.SignalTERM` generates the correct shell command (`kill -TERM <pid>`) and handles remote execution correctly.

## Success Criteria
- [ ] Injecting `SIGINT` triggers `cleanKill`.
- [ ] `cleanKill` successfully calls `SignalTERM` on the correct worker with the correct PID.
- [ ] SSH worker executes the remote `kill` command.
- [ ] Ledger entry for the interrupted job is marked as "interrupted".

## Out of Scope
- Testing actual SSH network connectivity (using mocks instead).
- Testing signal propagation to child processes of the remote command (relies on shell behavior).
