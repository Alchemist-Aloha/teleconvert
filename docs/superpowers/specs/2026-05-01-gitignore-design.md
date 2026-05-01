# Design: Updated .gitignore

Date: 2026-05-01
Topic: .gitignore optimization

## Goal
Organize the `.gitignore` file and include standard Go-specific ignore patterns while maintaining existing project-specific ignores.

## Proposed Changes

### 1. Binaries
Ignore the main compiled binary and common platform-specific executable formats.
- `teleconvert`
- `*.exe`
- `*.exe~`
- `*.dll`
- `*.so`
- `*.dylib`

### 2. Go Artifacts
Ignore development and testing artifacts.
- `*.test`
- `*.out`
- `go.work`
- `go.work.sum`

### 3. Project Specific
Maintain existing ignores for local test configuration and specific documentation.
- `test-config.yaml`
- `docs/CONFIG_VALIDATION.md`
- `docs/HANDBRAKE_CONVERSION.md`

## Out of Scope
- IDE-specific files (.vscode, .idea)
- OS-specific files (.DS_Store, Thumbs.db)
- Dependency vendoring (relying on `go.mod`/`go.sum`)
