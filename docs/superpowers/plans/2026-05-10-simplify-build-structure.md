# Simplify Build Structure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the main entry point to the root directory to enable `go build` to work directly.

**Architecture:** Relocating the `package main` file to the root.

**Tech Stack:** Go

---

### Task 1: Move main.go to Root

**Files:**
- Create: `main.go` (moved from `cmd/teleconvert/main.go`)
- Delete: `cmd/teleconvert/main.go`

- [ ] **Step 1: Move the file**

Run: `mv cmd/teleconvert/main.go main.go`

- [ ] **Step 2: Verify the file exists in root**

Run: `ls main.go`
Expected: `main.go`

- [ ] **Step 3: Commit**

```bash
git add main.go cmd/teleconvert/main.go
git commit -m "refactor: move main entry point to root"
```

### Task 2: Verify Build in Root

**Files:**
- Modify: `teleconvert` (binary will be overwritten)

- [ ] **Step 1: Run go build in root**

Run: `go build`
Expected: Success, creates `teleconvert` binary in root.

- [ ] **Step 2: Verify binary works**

Run: `./teleconvert --help`
Expected: Help output showing flags like `-config`, `-input`, etc.

- [ ] **Step 3: Commit (binary usually not committed, but we track progress)**

```bash
# Usually binaries are ignored, just verify it exists
ls -l teleconvert
```

### Task 3: Cleanup Empty Directory

**Files:**
- Delete: `cmd/`

- [ ] **Step 1: Remove empty directories**

Run: `rmdir cmd/teleconvert cmd/`

- [ ] **Step 2: Verify directory structure**

Run: `ls -d cmd`
Expected: `ls: cannot access 'cmd': No such file or directory`

- [ ] **Step 3: Commit**

```bash
git add .
git commit -m "chore: remove empty cmd directory"
```
