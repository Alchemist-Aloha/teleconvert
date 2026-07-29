# teleconvert

`teleconvert` is a small Go CLI for orchestrating robust transcoding jobs across local and SSH workers. It runs arbitrary encoder commands (HandBrakeCLI, ffmpeg, etc.) provided as templates in your YAML node configuration.
Install the latest compiled release on Linux or macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/Alchemist-Aloha/teleconvert/main/install.sh | bash
```

## Features

- Local and remote worker support (`localhost` or SSH nodes)
- Local workers read source media in place; only encoder output is staged in the worker `tmp_dir`.
- Minimum effort on worker setup. Only need SSH and FFmpeg/HandBrakeCLI
- Per-node concurrency slots (`max_concurrent`)
- Node heartbeat and busy-slot detection using PID lock files
- MD5 verification after upload
- Remote command lifecycle tracking with PID files
- Full-window terminal dashboard with separate teleconvert and encoder output
- Overall queue progress and per-worker HandBrakeCLI/ffmpeg progress
- Selectable, isolated encoder logs for every concurrency slot
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
go build -o teleconvert .
```

## Install

Install the latest compiled release on Linux or macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/Alchemist-Aloha/teleconvert/main/install.sh | bash
```

The installer verifies the release checksum, installs the binary to
`~/.local/bin`, and creates the example configuration only when no existing
configuration is present. Override the defaults with
`TELECONVERT_INSTALL_DIR`, `TELECONVERT_CONFIG_DIR`, or
`TELECONVERT_VERSION=v1.2.3`.

GitHub Actions tests and compiles every push and pull request. Pushing a tag
such as `v1.0.0` creates a GitHub Release with checksum-protected Linux and
macOS binaries for AMD64 and ARM64.

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

## Terminal dashboard

When run in an interactive terminal, teleconvert uses the terminal's alternate
screen and restores the previous contents when it exits. The top of the screen
shows total completion and the current job and progress for each worker slot.
Teleconvert lifecycle messages and raw encoder output appear in separate panes.

- `↑` / `↓`, `j` / `k`, or `Tab`: select a worker slot
- `1`–`9`: select a worker slot directly
- `Page Up` / `Page Down` or `u` / `d`: scroll the selected encoder output
- `g` / `Home`: jump to the oldest available encoder output
- `G` / `End`: return to the live encoder output
- `q` or `Ctrl-C`: stop active encoders cleanly and exit

After all jobs finish, the interactive dashboard remains open for review until
you press `Ctrl-C` or `q`. Non-interactive and redirected runs still exit
automatically.

HandBrakeCLI percentage output is recognized directly, including multi-pass
tasks. For ffmpeg, progress is calculated from its reported `Duration` and
`time`/`out_time` fields. Measurable encoding progress includes elapsed time and
an ETA; phases without a reliable percentage use a moving activity indicator
instead of displaying a misleading zero. When output is redirected or
teleconvert is run without a TTY, it automatically uses ordinary line-oriented
output instead.

Notes:

- The `-config` flag defaults to the path above; you can also pass a local YAML such as `example-config.yaml`.
- Each node `command` in the config must include the `{{.Input}}` and `{{.Output}}` placeholders; the CLI will substitute paths per-job.
- See `example-config.yaml` for a working sample configuration.
