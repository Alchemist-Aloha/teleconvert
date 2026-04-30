package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLedgerCreate(t *testing.T) {
	tmpdir := t.TempDir()
	ld, err := New(tmpdir)
	if err != nil {
		t.Fatalf("new ledger: %v", err)
	}
	if ld == nil {
		t.Error("expected ledger, got nil")
	}
}

func TestLedgerInitJobs(t *testing.T) {
	tmpdir := t.TempDir()
	ld, _ := New(tmpdir)

	jobs := []string{"/path/to/video1.mp4", "/path/to/video2.mp4"}
	if err := ld.InitJobs(jobs); err != nil {
		t.Fatalf("init jobs: %v", err)
	}

	for _, jobPath := range jobs {
		entry, ok := ld.Get(jobPath)
		if !ok {
			t.Errorf("job %s not found in ledger", jobPath)
		}
		if entry.Status != StatusPending {
			t.Errorf("expected status %s, got %s", StatusPending, entry.Status)
		}
	}
}

func TestLedgerPersistence(t *testing.T) {
	tmpdir := t.TempDir()
	ld1, _ := New(tmpdir)
	jobs := []string{"/path/to/video1.mp4"}
	ld1.InitJobs(jobs)
	ld1.Set(jobs[0], StatusDone, "worker1", "")

	ld2, _ := New(tmpdir)
	entry, ok := ld2.Get(jobs[0])
	if !ok {
		t.Error("persisted job not found after reload")
	}
	if entry.Status != StatusDone {
		t.Errorf("expected persisted status %s, got %s", StatusDone, entry.Status)
	}
	if entry.WorkerNode != "worker1" {
		t.Errorf("expected worker1, got %s", entry.WorkerNode)
	}
}

func TestLedgerAtomicWrite(t *testing.T) {
	tmpdir := t.TempDir()
	ld, _ := New(tmpdir)
	jobs := []string{"/path/to/video.mp4"}
	ld.InitJobs(jobs)

	statusPath := filepath.Join(tmpdir, ".teleconvert_status.json")
	if _, err := os.Stat(statusPath); err != nil {
		t.Errorf("ledger file not created: %v", err)
	}

	var state State
	b, _ := os.ReadFile(statusPath)
	json.Unmarshal(b, &state)
	if len(state.Jobs) != 1 {
		t.Errorf("expected 1 job persisted, got %d", len(state.Jobs))
	}
}

func TestLedgerSnapshot(t *testing.T) {
	tmpdir := t.TempDir()
	ld, _ := New(tmpdir)
	jobs := []string{"/path/to/video1.mp4", "/path/to/video2.mp4"}
	ld.InitJobs(jobs)
	ld.Set(jobs[0], StatusWorking, "worker1", "")

	snap := ld.Snapshot()
	if len(snap.Jobs) != 2 {
		t.Errorf("expected 2 jobs in snapshot, got %d", len(snap.Jobs))
	}
	if snap.Jobs[jobs[0]].Status != StatusWorking {
		t.Errorf("expected snapshot to reflect status change")
	}
}

func TestLedgerStatusTransitions(t *testing.T) {
	tmpdir := t.TempDir()
	ld, _ := New(tmpdir)
	jobs := []string{"/path/to/video.mp4"}
	ld.InitJobs(jobs)

	jobPath := jobs[0]
	transitions := []struct {
		status string
		worker string
	}{
		{StatusPending, ""},
		{StatusTransferring, "worker1"},
		{StatusWorking, "worker1"},
		{StatusDone, "worker1"},
	}

	for _, trans := range transitions {
		if err := ld.Set(jobPath, trans.status, trans.worker, ""); err != nil {
			t.Fatalf("set status: %v", err)
		}
		entry, _ := ld.Get(jobPath)
		if entry.Status != trans.status {
			t.Errorf("expected status %s, got %s", trans.status, entry.Status)
		}
	}
}

func TestLedgerRecoveryFromWorking(t *testing.T) {
	tmpdir := t.TempDir()
	ld1, _ := New(tmpdir)
	jobs := []string{"/path/to/video.mp4"}
	ld1.InitJobs(jobs)
	ld1.Set(jobs[0], StatusWorking, "worker1", "")

	ld2, _ := New(tmpdir)
	ld2.InitJobs(jobs)

	entry, _ := ld2.Get(jobs[0])
	if entry.Status != StatusPending {
		t.Errorf("expected stale working job to recover to pending, got %s", entry.Status)
	}
}

func TestLedgerErrorTracking(t *testing.T) {
	tmpdir := t.TempDir()
	ld, _ := New(tmpdir)
	jobs := []string{"/path/to/video.mp4"}
	ld.InitJobs(jobs)

	errMsg := "md5 mismatch"
	ld.Set(jobs[0], StatusPending, "", errMsg)

	entry, _ := ld.Get(jobs[0])
	if entry.LastError != errMsg {
		t.Errorf("expected error msg %q, got %q", errMsg, entry.LastError)
	}
}

func TestLedgerTimestamps(t *testing.T) {
	tmpdir := t.TempDir()
	ld, _ := New(tmpdir)
	jobs := []string{"/path/to/video.mp4"}

	before := time.Now().UTC()
	ld.InitJobs(jobs)
	after := time.Now().UTC()

	entry, _ := ld.Get(jobs[0])
	if entry.UpdatedAt.Before(before) || entry.UpdatedAt.After(after.Add(time.Second)) {
		t.Errorf("timestamp out of range: %v", entry.UpdatedAt)
	}
}

func TestLedgerMultipleJobs(t *testing.T) {
	tmpdir := t.TempDir()
	ld, _ := New(tmpdir)
	jobs := make([]string, 50)
	for i := range jobs {
		jobs[i] = filepath.Join(tmpdir, "video"+string(rune('0'+i))+".mp4")
	}
	ld.InitJobs(jobs)

	snap := ld.Snapshot()
	if len(snap.Jobs) != 50 {
		t.Errorf("expected 50 jobs, got %d", len(snap.Jobs))
	}

	for _, job := range jobs {
		entry, ok := snap.Jobs[job]
		if !ok {
			t.Errorf("job %s missing from snapshot", job)
			continue
		}
		if entry.Status != StatusPending {
			t.Errorf("expected pending, got %s", entry.Status)
		}
	}
}

func TestLedgerConcurrentAccess(t *testing.T) {
	tmpdir := t.TempDir()
	ld, _ := New(tmpdir)
	jobs := []string{"/path/to/video1.mp4", "/path/to/video2.mp4"}
	ld.InitJobs(jobs)

	done := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		go func(idx int) {
			for j := 0; j < 10; j++ {
				ld.Set(jobs[idx], StatusPending, "", "")
				ld.Snapshot()
				ld.Get(jobs[idx])
			}
			done <- true
		}(i)
	}
	<-done
	<-done
}
