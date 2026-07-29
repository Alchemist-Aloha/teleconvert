# Repository Guide for Agents

## Purpose

`teleconvert` is a Go CLI that distributes video conversion jobs across local
and SSH workers. Preserve the conversion, ledger, cleanup, and process-control
semantics when changing presentation code.

## Commands

Prefix shell commands with `rtk`, as required by the local environment.

The default Go cache may be read-only in the sandbox. Use:

```bash
GOCACHE=/tmp/teleconvert-go-cache rtk go test ./...
GOCACHE=/tmp/teleconvert-go-cache rtk go test -race ./internal/tui ./internal/orchestrator ./internal/worker
GOCACHE=/tmp/teleconvert-go-cache rtk go vet ./...
rtk gofmt -w <changed-go-files>
rtk git diff --check
```

Run the focused package test while iterating, then run the full validation set
before handing off.

## Code Map

- `main.go`: flags and process entry point.
- `internal/config`: YAML configuration and node validation.
- `internal/discovery`: input discovery and output-path mapping.
- `internal/ledger`: persistent per-source job state.
- `internal/orchestrator`: scheduling, job lifecycle, cleanup, and signals.
- `internal/worker`: local and SSH execution, transfer, and log streaming.
- `internal/tui`: alternate-screen dashboard, input handling, progress parsing,
  scrollback, and terminal rendering.

## TUI Invariants

- The dashboard is the only component allowed to write to the terminal while
  interactive mode is active.
- Never connect worker subprocess stdout or stderr directly to `os.Stdout` or
  `os.Stderr`. Capture it and route it through the dashboard or a returned
  error.
- Keep non-TTY and redirected execution line-oriented and non-blocking.
- Normalize untrusted log text before rendering. Embedded newlines, carriage
  returns, ANSI escapes, and control characters must not escape panel borders.
- Use differential rendering; avoid unconditional full-screen redraws.
- Progress telemetry updates the progress model but must not flood persistent
  encoder scrollback.
- Keyboard state and scrollback are per worker where appropriate. Preserve the
  selected worker and historical view when new output arrives.
- After successful interactive completion, keep the dashboard open until
  `Ctrl-C` or `q`. Non-interactive runs must exit automatically.

## Worker and Orchestrator Safety

- Preserve PID/exit/log file handling and process-group termination behavior.
- Interrupted jobs return to `pending`; do not mark them complete.
- Do not delete source files unless `DeleteSource` is enabled and conversion
  has succeeded.
- Remote uploads remain atomic and checksum-verified.
- Do not change the `Worker` interface casually; it has local, SSH, and mock
  implementations.
- Avoid terminal or UI dependencies inside worker implementations.

## Testing Expectations

- Add focused tests for every parser, terminal-layout, keyboard, or lifecycle
  regression.
- Use synthetic encoder output in unit tests; do not require ffmpeg,
  HandBrakeCLI, SSH hosts, or network access.
- Run race tests for changes involving dashboard state, goroutines, channels,
  signals, or worker output.
- Preserve existing user changes in the worktree and avoid unrelated cleanup.

