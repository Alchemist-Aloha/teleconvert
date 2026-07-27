# teleconvert

`teleconvert` is a small Go CLI for orchestrating robust transcoding jobs across local and SSH workers. It runs arbitrary encoder commands (HandBrakeCLI, ffmpeg, etc.) provided as templates in your YAML node configuratefion.

## Features

- Local and remote worker support (`localhost` or SSH nodes)
- Minimum effort on worker setup. Only need SSH and FFmpeg/HandBrakeCLI
- Per-node concurrency slots (`max_concurrent`)
- Node heartbeat and busy-slot detection using PID lock files
- MD5 verification after upload
- Remote command lifecycle tracking with PID files
- Periodic monitoring and stderr streaming back to local terminal
- SIGINT/SIGTERM clean-kill behavior:
  - sends SIGTERM to managed remote/local processes
  - removes active remote `.part` files
  - marks interrupted jobs back to `pending`
- Jobs and progress are tracked in `.teleconvert_status.json` files written beside the source media in each directory.
- A `converted` directory is excluded from discovery when its parent contains `.teleconvert_status.json`.
- Remote uploads are performed atomically (temporary `.part` files, then rename) and checksummed when configured.

## Config

Default config path:

`~/.config/teleconvert/teleconvert.yaml` (see `internal/config.DefaultConfigPath()`)

Example:

```yaml
nodes:

- name: "remote"
  address: "192.168.1.100:22"
  user: "user"
  ssh_key: "/home/user/.ssh/id_rsa"
  max_concurrent: 1
  #  Placeholders: {{.Input}} and {{.Output}}
  command: "HandBrakeCLI -i {{.Input}} -o {{.Output}} -e x265 -q 27.0 -a 1 -E fdk_aac -B 160 -6 stereo -f mkv --deinterlace -w 1920 -l 1080 -O"
  tmp_dir: "/tmp/teleconvert"

- name: "local-encoder"
  address: "localhost"
  max_concurrent: 1
  command: "HandBrakeCLI -i {{.Input}} -o {{.Output}} -e x265 -q 27.0 -a 1 -E fdk_aac -B 160 -6 stereo -f mkv --deinterlace -w 1920 -l 1080 -O"
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
- `-output-dir`: destination directory (default: a `converted` directory beside each source file)
- `-output-ext`: output extension (default `.mp4`)
- `-delete-source`: delete source files when each job succeeds
- `-continue-on-error`: keep running after per-job failures (default `true`)
- `-verbose` / `-v`: enable verbose logging
- `-poll-interval`: worker monitor interval (default `2s`)

Notes:

- The `-config` flag defaults to the path above; you can also pass a local YAML such as `example-config.yaml`.
- Each node `command` in the config must include the `{{.Input}}` and `{{.Output}}` placeholders; the CLI will substitute paths per-job.
- See `example-config.yaml` for a working sample configuration.
