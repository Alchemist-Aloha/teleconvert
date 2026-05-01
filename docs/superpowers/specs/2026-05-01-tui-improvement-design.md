# Design Spec: TUI Improvement with pterm

## Overview
This specification details the transition of the `teleconvert` CLI from standard `fmt` output to a modern, styled scrollable TUI using the `github.com/pterm/pterm` library.

## Goals
- Add a visual "WordArt" banner on startup.
- Improve status reporting for file discovery and job completion.
- Enhance the readability of concurrent worker output.
- Maintain a scrollable (non-fullscreen) terminal experience.

## Proposed Changes

### 1. Dependencies
Add `github.com/pterm/pterm/v2` to `go.mod`.

### 2. Startup Banner
In `cmd/teleconvert/main.go`:
- Use `pterm.DefaultBigText.WithLetters(pterm.NewLettersFromString("TELECONVERT")).Render()` at the entry point.
- Display version or sub-heading using `pterm.DefaultSection.WithLevel(2)`.

### 3. Orchestration & Logging
In `internal/orchestrator/orchestrator.go`:
- **File Discovery:** Replace the initial print with a `pterm.DefaultSpinner` while `discovery.Discover` and `ld.InitJobs` are running.
- **Job Status:**
    - Use `pterm.Info` for the total job count summary.
    - Use `pterm.Success` for "job done" messages.
    - Use `pterm.Error` for "job failed" messages.
- **Verbose Logging:** 
    - Enable/Disable `pterm.EnableDebugMessages()` based on `opts.Verbose`.
    - Replace `o.vlog` internal calls with `pterm.Debug.Printf`.

### 4. Worker Output Refactoring
In `internal/orchestrator/orchestrator.go`:
- Refactor `prefixWriter` to use `pterm` styles.
- Each node will be assigned a unique color (from a fixed pool) to its prefix label.
- The prefix will be formatted as a styled tag (e.g., `pterm.BgBlue.Sprint(" node-1 ")`).

## Verification Plan

### Automated Tests
- No new automated tests for the UI, but ensure existing orchestrator tests still pass (mocking output if necessary, though `pterm` usually writes to stdout/stderr).

### Manual Verification
1. Run `teleconvert` with invalid flags to ensure banner shows before exit.
2. Run with valid input to observe:
    - Spinner during discovery.
    - BigText banner.
    - Colored job completion logs.
    - Colored worker prefixes in stderr output.
3. Verify `-verbose` output uses the `Debug` style.
