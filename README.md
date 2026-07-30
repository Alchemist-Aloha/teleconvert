# teleconvert

Got a rack of machines sitting idle while your laptop burns through a transcode queue all weekend? **teleconvert** turns any SSH-accessible machine into a transcode worker with zero setup — no daemon, no agent, no Docker, no nothing. Just SSH access and `ffmpeg` or `HandBrakeCLI` on the PATH. Define your nodes in a YAML file, point it at your media, and let the idle hardware earn its keep.
Install the latest compiled release on Linux or macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/Alchemist-Aloha/teleconvert/main/install.sh | bash
```

## Features

- **No agent required** — workers only need SSH access and `ffmpeg` or `HandBrakeCLI` on the PATH. Nothing to install, no daemon to run.
- **Live TUI dashboard** — full-screen terminal UI with per-worker progress bars, scrollable encoder logs, and keyboard navigation
- **Resilient by design** — clean shutdown on Ctrl-C/SIGTERM, automatic job recovery on interrupt, atomic remote uploads with checksum verification
- **Parallel workers** — per-node concurrency control, heartbeat monitoring, and automatic busy-slot detection
- **Encoder agnostic** — works with HandBrakeCLI, ffmpeg, or any command via configurable templates with `{{.Input}}` / `{{.Output}}` placeholders

## Terminal dashboard

When run in an interactive terminal, teleconvert uses the terminal's alternate
screen and restores the previous contents when it exits. The top of the screen
shows total completion and the current job and progress for each worker slot.
Teleconvert lifecycle messages and raw encoder output appear in separate panes.

```
┌─ TELECONVERT  [=========         ]  7/15 done  0 failed ──────────┐
┌─ Workers (↑/↓, j/k, tab or 1-9 to select) ───────────────────────┐
│ > remote         encoding   [==============]  42.3%  elapsed 01:23  video1.mkv│
│   remote         encoding   [====            ]  12.1%  elapsed 00:47  video2.mkv│
│   local          idle                                                  │
│   local          encoding   [=============== ]  78.5%  elapsed 02:07  video4.mkv│
└──────────────────────────────────────────────────────────────────────┘
┌─ Teleconvert output ───────────────────────────────────────────────┐
│ 14:23:01  remote:video1.mkv encoding started                         │
│ 14:23:15  local:video4.mkv encoding started                          │
│ 14:23:30  remote:video2.mkv encoding started                         │
│ 14:23:45  remote:video3.mkv error: no subtitle track 1, skipping     │
│ 14:23:46  local:video4.mkv pass 1 (1/2)  58.2%                       │
└──────────────────────────────────────────────────────────────────────┘
┌─ Encoder output — remote  [live] ──────────────────────────────────┐
│ Encoding: task 1 of 1, 42.37 % (12.00 fps)                           │
│ [14:23:01] 1: encoding started                                       │
│ [14:23:15] 1: sync: audio 0x1 -> audio 0x0, bitrate 160             │
└──────────────────────────────────────────────────────────────────────┘
 PgUp/PgDn or u/d: scroll • g/G: oldest/live • q/Ctrl-C: clean shutdown
```

The top bar shows the queue title and overall progress. The **Workers** panel
lists each slot with a selection marker (`>` for the selected slot), node name,
status, progress bar + percentage, elapsed time, and current file. The
**Teleconvert output** panel logs lifecycle messages with timestamps. The
**Encoder output** panel shows the raw encoder log for the currently selected
worker, with scrollback support.

- `↑` / `↓`, `j` / `k`, or `Tab`: select a worker slot
- `1`–`9`: select a worker slot directly
- `Page Up` / `Page Down` or `u` / `d`: scroll the selected encoder output
- `g` / `Home`: jump to the oldest available encoder output
- `G` / `End`: return to the live encoder output
- `q` or `Ctrl-C`: stop active encoders cleanly and exit

After all jobs finish, the interactive dashboard remains open for review until
you press `Ctrl-C` or `q`. Non-interactive and redirected runs still exit
automatically.

## Usage

```bash
teleconvert \
  -input /path/to/videos \
  -output-dir /path/to/output \
  -output-ext .mp4 \
  -config ~/.config/teleconvert/teleconvert.yaml
```

Flags:

- `-config`: path to config file (default: `~/.config/teleconvert/teleconvert.yaml`)
- `-input`: input file or directory (required)
- `-output-dir`: destination directory (default: a `converted` directory beside each source file)
- `-output-ext`: output extension (default `.mp4`)
- `-delete-source`: delete source files when each job succeeds
- `-continue-on-error`: keep running after per-job failures (default `true`)
- `-verbose` / `-v`: enable verbose logging
- `-poll-interval`: worker monitor interval (default `2s`)
- `-version`: print the embedded build version and exit

## Config

Default config path:

`~/.config/teleconvert/teleconvert.yaml` (see `internal/config.DefaultConfigPath()`)

Each node `command` in the config must include the `{{.Input}}` and `{{.Output}}` placeholders; the CLI will substitute paths per-job.


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

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/Alchemist-Aloha/teleconvert/main/install.sh | bash
```

Installs the binary to `~/.local/bin` and creates a default config if none exists.
Override with `TELECONVERT_INSTALL_DIR`, `TELECONVERT_CONFIG_DIR`, or
`TELECONVERT_VERSION=v1.2.3`.

## Build

```bash
go mod tidy
go build -o teleconvert .
```
