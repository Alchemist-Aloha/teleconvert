package ledger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	StatusPending      = "pending"
	StatusTransferring = "transferring"
	StatusWorking      = "working"
	StatusDone         = "done"
	StatusFailed       = "failed"
)

type Entry struct {
	Status     string    `json:"status"`
	WorkerNode string    `json:"worker_node,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastError  string    `json:"last_error,omitempty"`
}

type State struct {
	Version int              `json:"version"`
	Jobs    map[string]Entry `json:"jobs"`
}

type Ledger struct {
	path  string
	state State
	mu    sync.Mutex
}

func New(root string) (*Ledger, error) {
	path := filepath.Join(root, ".teleconvert_status.json")
	l := &Ledger{
		path: path,
		state: State{
			Version: 1,
			Jobs:    map[string]Entry{},
		},
	}
	if err := l.load(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Ledger) load() error {
	b, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read ledger: %w", err)
	}
	if err := json.Unmarshal(b, &l.state); err != nil {
		return fmt.Errorf("parse ledger: %w", err)
	}
	if l.state.Jobs == nil {
		l.state.Jobs = map[string]Entry{}
	}
	return nil
}

func (l *Ledger) InitJobs(jobPaths []string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, p := range jobPaths {
		e, ok := l.state.Jobs[p]
		if !ok {
			l.state.Jobs[p] = Entry{Status: StatusPending, UpdatedAt: time.Now().UTC()}
			continue
		}
		if e.Status == StatusWorking || e.Status == StatusTransferring {
			e.Status = StatusPending
			e.WorkerNode = ""
			e.LastError = ""
			e.UpdatedAt = time.Now().UTC()
			l.state.Jobs[p] = e
		}
	}
	return l.persistLocked()
}

func (l *Ledger) Set(jobPath, status, worker, lastErr string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.state.Jobs[jobPath] = Entry{
		Status:     status,
		WorkerNode: worker,
		UpdatedAt:  time.Now().UTC(),
		LastError:  lastErr,
	}
	return l.persistLocked()
}

func (l *Ledger) Get(jobPath string) (Entry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.state.Jobs[jobPath]
	return e, ok
}

func (l *Ledger) Snapshot() State {
	l.mu.Lock()
	defer l.mu.Unlock()
	copyJobs := make(map[string]Entry, len(l.state.Jobs))
	for k, v := range l.state.Jobs {
		copyJobs[k] = v
	}
	return State{Version: l.state.Version, Jobs: copyJobs}
}

func (l *Ledger) persistLocked() error {
	tmp := l.path + ".tmp"
	b, err := json.MarshalIndent(l.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ledger: %w", err)
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write ledger temp: %w", err)
	}
	if err := os.Rename(tmp, l.path); err != nil {
		return fmt.Errorf("replace ledger: %w", err)
	}
	return nil
}
