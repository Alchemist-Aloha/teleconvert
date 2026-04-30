# Config Validation Report

✅ **Both configurations are fully compatible with the current teleconvert implementation.**

## Full Config (test-config.yaml)

```yaml
nodes:
  - name: "workstation-01"
    address: "192.168.88.230:22"
    user: "kent"
    ssh_key: "~/.ssh/id_rsa"
    max_concurrent: 1
    command: "ffmpeg -i {{.Input}} -c:v libx265 -crf 22 {{.Output}}"
    tmp_dir: "/tmp/teleconvert"

  - name: "home-server"
    address: "localhost"
    max_concurrent: 1
    command: "HandBrakeCLI -i {{.Input}} -o {{.Output}} --preset 'High Profile'"
    tmp_dir: "/tmp/teleconvert"
```

### Validation Results ✓

| Field | Node | Status | Notes |
|-------|------|--------|-------|
| `name` | workstation-01 | ✓ | Required, must be unique |
| `address` | 192.168.88.230:22 | ✓ | SSH address with port |
| `user` | kent | ✓ | Required for SSH nodes |
| `ssh_key` | ~/.ssh/id_rsa | ✓ | Path with ~ expansion supported |
| `max_concurrent` | 1 | ✓ | Controls worker slots |
| `command` | ffmpeg... | ✓ | Has {{.Input}} and {{.Output}} placeholders |
| `tmp_dir` | /tmp/teleconvert | ✓ | Remote working directory |
| `name` | home-server | ✓ | Required, unique name |
| `address` | localhost | ✓ | Recognized as local node |
| `user` | (omitted) | ✓ | Optional for local nodes |
| `ssh_key` | (omitted) | ✓ | Optional for local nodes |
| `command` | HandBrakeCLI... | ✓ | Has {{.Input}} and {{.Output}} placeholders |

---

## Local-Only Config (test-config-local.yaml)

```yaml
nodes:
  - name: "local-encoder"
    address: "localhost"
    max_concurrent: 2
    command: "ffmpeg -i {{.Input}} -c:v libx264 -preset fast {{.Output}}"
    tmp_dir: "/tmp/teleconvert"
```

### Validation Results ✓

| Field | Value | Status | Notes |
|-------|-------|--------|-------|
| `name` | local-encoder | ✓ | Required, unique |
| `address` | localhost | ✓ | Recognized as local (also accepts 127.0.0.1, ::1) |
| `max_concurrent` | 2 | ✓ | Multiple concurrent jobs supported |
| `command` | ffmpeg... | ✓ | Valid placeholder syntax |
| `tmp_dir` | /tmp/teleconvert | ✓ | Local working directory |

---

## Configuration Features Verified

### ✓ Node Types
- **SSH Remote**: Full support with user, address:port, ssh_key
- **Local Node**: Support with localhost/127.0.0.1/::1 addresses
- **SSH Key Expansion**: ~/.ssh paths automatically expanded

### ✓ Required Fields
- `nodes`: Array of worker nodes (required)
- `name`: Unique node identifier (required)
- `address`: SSH host:port or localhost (required)
- `command`: Transcoding command with placeholders (required)

### ✓ Optional Fields
- `user`: Only required for SSH nodes
- `ssh_key`: Only required for SSH nodes
- `max_concurrent`: Defaults to 1 if omitted
- `tmp_dir`: Defaults to /tmp/teleconvert if omitted

### ✓ Command Validation
Both configs have valid command templates:
- Must contain `{{.Input}}` placeholder
- Must contain `{{.Output}}` placeholder
- Templates are rendered per-job with actual file paths

### ✓ Concurrency Control
- `max_concurrent: 1` → Single job at a time per node
- `max_concurrent: 2` → Up to 2 concurrent jobs per node
- Each node gets independent slots

### ✓ Working Directories
- Remote tmp_dir: Used to stage files during transcoding
- Local ledger: `.teleconvert_status.json` in source folder
- Local temp: `.tmp` suffix for in-progress outputs

---

## Tested Behaviors

### Config Loading
- ✓ YAML parsing and validation
- ✓ Node name uniqueness enforcement
- ✓ Command placeholder validation ({{.Input}}, {{.Output}})
- ✓ Path expansion (~/ prefix)
- ✓ Default value population

### Address Detection
- ✓ SSH: 192.168.88.230:22 → Remote worker
- ✓ Local: localhost → Local worker
- ✓ Local: 127.0.0.1 → Local worker (also works)
- ✓ Local: ::1 → Local worker (IPv6 also works)

### Error Handling
- ✓ Empty nodes list → Rejected
- ✓ Missing name → Rejected
- ✓ Duplicate names → Rejected
- ✓ Missing address → Rejected
- ✓ Missing command → Rejected
- ✓ Invalid placeholders → Rejected

---

## Quick Start

```bash
# Create config directory
mkdir -p ~/.config/teleconvert

# Copy test config (full setup)
cp test-config.yaml ~/.config/teleconvert/teleconvert.yaml

# Or use local-only (no SSH setup needed)
cp test-config-local.yaml ~/.config/teleconvert/teleconvert.yaml

# Run with default config path
./teleconvert -input /path/to/videos -output-dir /path/to/output

# Or specify config explicitly
./teleconvert -config ./test-config.yaml -input /path/to/videos
```

---

## Summary

**Configuration compatibility: 100% ✅**

Both test configurations fully validate and are compatible with:
- ✓ Current config.go implementation
- ✓ Node discovery and heartbeat checks
- ✓ Worker slot allocation (max_concurrent)
- ✓ Command rendering with {{.Input}}/{{.Output}}
- ✓ SSH key expansion
- ✓ Local address detection
- ✓ Default value population
- ✓ State persistence and resume
