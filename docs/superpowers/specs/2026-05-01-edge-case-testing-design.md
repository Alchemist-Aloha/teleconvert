# Design Spec: Comprehensive Edge-Case Testing

## Overview
This specification outlines the addition of robust edge-case tests across the `teleconvert` codebase to ensure reliability in real-world scenarios, including configuration errors, resource exhaustion, and network instability.

## Goals
- Validate configuration behavior with unusual inputs.
- Ensure the orchestrator handles hardware and filesystem errors gracefully.
- Verify worker behavior during communication and execution failures.

## Proposed Test Cases

### 1. Configuration Validation (`internal/config`)
- **Zero/Negative MaxConcurrent:** Verify that `Validate()` defaults `MaxConcurrent` to 1 if it is <= 0.
- **Missing TmpDir:** Verify that `Validate()` defaults `TmpDir` to `/tmp/teleconvert` if empty.
- **Template Injection/Safety:** Verify that command templates are strictly validated (ensure they contain required placeholders and no obvious shell escapes outside placeholders).

### 2. Orchestrator Robustness (`internal/orchestrator`)
- **Disk Full during Rename:** Mock `os.Rename` failure in `executeJob` to ensure the ledger is updated with a `StatusFailed` and the partial file is handled.
- **Malformed Ledger:** Test `ledger.New` with a file containing invalid JSON and ensure it handles the error or resets gracefully.
- **Worker Slot Race:** Stress test the slot allocation logic by running many small jobs with limited slots.

### 3. Worker Edge Cases (`internal/worker`)
- **MD5 Checksum Mismatch:** Test `executeJob` (which uses `worker.MD5`) by mocking a worker that returns a different checksum than the local file. Verify it fails and cleans up the remote part.
- **Remote Process Hanging:** Test `WaitForExit` by providing a mock exit file that never appears, ensuring it respects the context cancellation.
- **Local File Permissions:** Test `UploadAtomic` failure when the destination directory is unwritable.

## Implementation Strategy (TDD)
For each test case:
1. Write a failing test in the appropriate `_test.go` file.
2. Run the test to confirm failure.
3. Modify the code to handle the edge case correctly.
4. Run the test to confirm it passes.

## Verification Plan

### Automated Tests
- Run `go test ./...` and ensure all 56+ tests pass.
- Use `go test -race ./...` to check for race conditions in the orchestrator.

### Manual Verification
- None required; the automated tests will cover these specific edge cases.
