build a linux go cli tool teleconvert that handle ffmpeg/handbrake transcoding job to local or remote workers via ssh.follow this design and try to achieve highest robustness.

The Configuration Structure (teleconvert.yaml)
To keep it robust, the config needs to define the capabilities of each node (e.g., does it have an NVIDIA GPU?) and how to reach them.
YAML
nodes:

name: "workstation-01"
address: "192.168.88.230:22"
user: "kent"
ssh_key: "~/.ssh/id_rsa"
max_concurrent: 1

Placeholders: {{.Input}} and {{.Output}}
command: "ffmpeg -i {{.Input}} -c:v libx265 -crf 22 {{.Output}}"
tmp_dir: "/tmp/teleconvert"

name: "home-server"
address: "localhost" # Local node support
command: "HandBrakeCLI -i {{.Input}} -o {{.Output}} --preset 'High Profile'"

High-Level Logic Flow
What this really means is that the tool acts as a State Machine. It doesn't just "fire and forget"; it monitors the lifecycle of the file.
Phase 1: Discovery & Orchestration
Input Parsing: If the input is a folder, the tool crawls it and creates a Job Queue of all video files.

Node Heartbeat: For each node in the config, the tool runs a quick ssh [node] "ls /tmp" to check connectivity.

Status Check (PID): It checks for a specific "lock file" or PID on the remote node (e.g., cat /tmp/teleconvert.pid). If the PID exists and the process is still running, the node is marked Busy.

Phase 2: The "teleconvert" Cycle
For every idle node and every pending job:

Stage (Push): SFTP the source file to the worker’s tmp_dir.

Robustness Check: Compare md5sum locally vs. remotely before proceeding.

Execute: Start the command. Use a Session Shell to capture stderr and stream it back to the local terminal.

PID Management: Store the remote process ID in /tmp/teleconvert.pid immediately so other instances of the tool know the node is occupied.

Monitor: Use a time.Ticker in Go to periodically check if the remote PID is still active.

Phase 3: Collection & Cleanup
Verify: Once the process exits (Exit Code 0), check if the {{.Output}} file exists and has a non-zero size.

Pull: SFTP the finished file back to the server.

Scrub: Delete the remote input and output files from the worker to keep the tmp_dir clean.

Finalize: Move the local .tmp output to the final destination and delete the local source (if configured).

Engineering for "High Robustness"
To ensure this doesn't hang mid-way through a 100-video folder, implement these three safety systems:
A. The "Resume" Ledger
Don't just keep the queue in RAM. Create a hidden .teleconvert_status.json in the source folder.

Record: filename | status (pending/transferring/working/done) | worker_node.

If you hit Ctrl+C and restart, the tool reads the ledger and picks up exactly where it left off.

B. Atomic Transfers
Never SFTP a file directly to its final name.

Push as filename.input.part.

Rename to filename.input only after the transfer is complete.

This prevents Handbrake from trying to transcode a partially uploaded file.

C. Signal Handling (The "Clean Kill")
In Go, catch os.Interrupt (SIGINT). If you kill the local tool, it should attempt to:

Send a SIGTERM to the remote PIDs it is currently managing.

Delete the current remote .part files.

Update the ledger so the jobs stay in "pending" for the next run.