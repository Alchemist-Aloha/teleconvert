# Design Spec: Simplify Build Structure

## Goal
Enable `go build` to work directly in the project root directory.

## Current State
The project follows a `cmd/teleconvert/main.go` layout. Running `go build` in the root fails because there are no Go files in the root.

## Proposed Change
Move the `main.go` file from `cmd/teleconvert/` to the root directory.

## Impact
- **Simplicity**: Users can run `go build` or `go run .` from the root.
- **Root Directory**: Will now contain `main.go`, `go.mod`, `go.sum`, etc.
- **CMD Directory**: The `cmd/` directory will be removed as it will be empty.

## Verification
- Run `go build` in the root and ensure it produces the `teleconvert` binary.
- Run `./teleconvert --help` to verify the binary works.
