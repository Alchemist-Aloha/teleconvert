package worker

import (
	"context"
	"io"
	"time"

	"teleconvert/internal/config"
)

type Worker interface {
	Node() config.Node
	Heartbeat(ctx context.Context) error
	CheckCommand(ctx context.Context, cmd string) error
	EnsureDir(ctx context.Context, dir string) error
	ReadPID(ctx context.Context, pidFile string) (int, error)
	IsProcessRunning(ctx context.Context, pid int) (bool, error)
	UploadAtomic(ctx context.Context, localPath, remoteFinalPath string) (remotePartPath string, err error)
	MD5(ctx context.Context, path string) (string, error)
	StartCommand(ctx context.Context, command, pidFile, exitFile, stderrLog string) (int, error)
	WaitForExit(ctx context.Context, pid int, exitFile, stderrLog string, pollInterval time.Duration, stderrSink io.Writer) (int, error)
	FileExistsNonZero(ctx context.Context, path string) (bool, error)
	Download(ctx context.Context, remotePath, localPath string) error
	Remove(ctx context.Context, paths ...string) error
	SignalTERM(ctx context.Context, pid int) error
}
