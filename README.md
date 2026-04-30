# teleconvert

`teleconvert` is a Linux Go CLI that orchestrates robust transcoding jobs across local and SSH workers using FFmpeg or HandBrakeCLI.

## Features

- Local and remote worker support (`localhost` or SSH nodes)
- Per-node concurrency slots (`max_concurrent`)
- Node heartbeat and busy-slot detection using PID lock files
- Atomic upload (`.part` -> final rename)
- MD5 verification after upload
- Remote command lifecycle tracking with PID files
- Periodic monitoring and stderr streaming back to local terminal
- Resume ledger (`.teleconvert_status.json`) in the source root
- SIGINT/SIGTERM clean-kill behavior:
  - sends SIGTERM to managed remote/local processes
  - removes active remote `.part` files
  - marks interrupted jobs back to `pending`

## Config

Default config path:

`~/.config/teleconvert/teleconvert.yaml`

Example:

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

## Build

```bash
go mod tidy
go build -o teleconvert ./cmd/teleconvert
```

## Usage

```bash
./teleconvert \
  -input /path/to/videos \
  -output-dir /path/to/output \
  -output-ext .mp4 \
  -config ~/.config/teleconvert/teleconvert.yaml
```

Flags:

- `-input`: input file or directory (required)
- `-output-dir`: destination directory
- `-output-ext`: output extension (default `.mp4`)
- `-delete-source`: delete source files when each job succeeds
- `-continue-on-error`: keep running after per-job failures (default `true`)
- `-poll-interval`: worker monitor interval (default `2s`)
