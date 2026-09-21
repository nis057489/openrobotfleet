package agent

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func awaitJob(t *testing.T, jm *JobManager, id string) *Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job := jm.GetJob(id)
		if job != nil && (job.Status == JobStatusSuccess || job.Status == JobStatusFailed) {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job %s did not finish", id)
	return nil
}
func TestJobsQueueAndDeduplicate(t *testing.T) {
	jm := NewJobManager()
	release := make(chan struct{})
	calls := atomic.Int32{}
	if err := jm.StartJob("1", "update_repo", nil, func() error { <-release; return nil }); err != nil {
		t.Fatal(err)
	}
	action := func() error { calls.Add(1); return nil }
	jm.StartJob("2", "identify", nil, action)
	jm.StartJob("2", "identify", nil, action)
	if jm.GetJob("2").Status != JobStatusPending {
		t.Fatal("busy command was not queued")
	}
	snapshot := jm.GetCurrentJob()
	snapshot.Status = JobStatusFailed
	if jm.GetJob("1").Status != JobStatusRunning {
		t.Fatal("mutable job state escaped lock")
	}
	close(release)
	awaitJob(t, jm, "2")
	if calls.Load() != 1 {
		t.Fatal("duplicate executed")
	}
}
func TestStopPreemptsMotionAndCancelsQueuedMotion(t *testing.T) {
	jm := NewJobManager()
	started := make(chan struct{})
	cancelled := make(chan struct{})
	stopped := make(chan struct{})
	pendingCalls := atomic.Int32{}
	jm.StartContextJob("drive", "test_drive", nil, func(ctx context.Context) error { close(started); <-ctx.Done(); close(cancelled); return ctx.Err() })
	<-started
	jm.StartJob("queued", "test_drive", nil, func() error { pendingCalls.Add(1); return nil })
	jm.StartJob("stop", "stop", nil, func() error { close(stopped); return nil })
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop blocked")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("drive was not cancelled")
	}
	if awaitJob(t, jm, "queued").Status != JobStatusFailed || pendingCalls.Load() != 0 {
		t.Fatal("queued motion survived stop")
	}
	awaitJob(t, jm, "drive")
	awaitJob(t, jm, "stop")
}
func TestStopRunsDuringNonMotionJob(t *testing.T) {
	jm := NewJobManager()
	release := make(chan struct{})
	defer close(release)
	jm.StartJob("apt", "system_update", nil, func() error { <-release; return nil })
	jm.StartJob("stop", "stop", nil, func() error { return nil })
	if awaitJob(t, jm, "stop").Status != JobStatusSuccess {
		t.Fatal("stop failed")
	}
	if jm.GetJob("apt").Status != JobStatusRunning {
		t.Fatal("maintenance unexpectedly interrupted")
	}
}
func TestResultsPersistUntilAcknowledged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	jm := NewJobManager()
	if err := jm.LoadState(path); err != nil {
		t.Fatal(err)
	}
	jm.StartJob("failed", "update_repo", nil, func() error { return errors.New("clone failed") })
	awaitJob(t, jm, "failed")
	reloaded := NewJobManager()
	if err := reloaded.LoadState(path); err != nil {
		t.Fatal(err)
	}
	if results := reloaded.Results(); len(results) != 1 || results[0].Error != "clone failed" {
		t.Fatalf("lost failure: %+v", results)
	}
	reloaded.Acknowledge([]string{"failed"})
	if len(reloaded.Results()) != 0 {
		t.Fatal("acknowledged result repeated")
	}
	var calls atomic.Int32
	reloaded.StartJob("failed", "update_repo", nil, func() error { calls.Add(1); return nil })
	if calls.Load() != 0 || len(reloaded.Results()) != 1 {
		t.Fatal("retry did not replay result safely")
	}
}
func TestFailureAppearsInHeartbeat(t *testing.T) {
	e := NewAgentEngine(Config{})
	e.JobManager.StartJob("17", "update_repo", nil, func() error { return errors.New("failed deployment") })
	awaitJob(t, e.JobManager, "17")
	var payload struct {
		JobID    string `json:"job_id"`
		JobError string `json:"job_error"`
		Results  []Job  `json:"results"`
	}
	if err := json.Unmarshal(e.buildStatusPayload(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.JobID != "17" || payload.JobError != "failed deployment" || len(payload.Results) != 1 {
		t.Fatalf("failure not reported: %+v", payload)
	}
}
func TestRestartDoesNotReplayInterruptedWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	jm := NewJobManager()
	jm.LoadState(path)
	release := make(chan struct{})
	jm.StartJob("reboot", "reboot", nil, func() error { <-release; return nil })
	reloaded := NewJobManager()
	if err := reloaded.LoadState(path); err != nil {
		t.Fatal(err)
	}
	if reloaded.GetJob("reboot").Status != JobStatusFailed {
		t.Fatal("interrupted job was not marked unconfirmed")
	}
	close(release)
	awaitJob(t, jm, "reboot")
}

func TestDelayedMotionCannotOvertakeStop(t *testing.T) {
	jm := NewJobManager()
	jm.StartJob("20", "stop", nil, func() error { return nil })
	awaitJob(t, jm, "20")
	var calls atomic.Int32
	jm.StartJob("19", "test_drive", nil, func() error { calls.Add(1); return nil })
	if awaitJob(t, jm, "19").Status != JobStatusFailed || calls.Load() != 0 {
		t.Fatal("delayed movement executed after stop")
	}
	jm.StartJob("21", "test_drive", nil, func() error { calls.Add(1); return nil })
	if awaitJob(t, jm, "21").Status != JobStatusSuccess || calls.Load() != 1 {
		t.Fatal("new movement rejected")
	}
}

func TestStopDoesNotDependOnWritableJournal(t *testing.T) {
	jm := NewJobManager()
	dir := t.TempDir()
	blocked := filepath.Join(dir, "not-a-directory")
	writeTestFile(t, blocked, "file")
	jm.statePath = filepath.Join(blocked, "jobs.json")
	called := make(chan struct{})
	if err := jm.StartJob("stop", "stop", nil, func() error { close(called); return nil }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("journal failure blocked stop")
	}
	awaitJob(t, jm, "stop")
	if err := jm.StartJob("ordinary", "update_repo", nil, func() error { t.Error("ordinary job ran without journal"); return nil }); err == nil {
		t.Fatal("ordinary job accepted without durable journal")
	}
}

func TestDurableCommandsSupersedePendingAndLateDelivery(t *testing.T) {
	jm := NewJobManager()
	release := make(chan struct{})
	jm.StartJob("1", "update_repo", nil, func() error { <-release; return nil })
	var oldCalls, newCalls atomic.Int32
	jm.StartJob("2", "configure_network", nil, func() error { oldCalls.Add(1); return nil })
	jm.StartJob("3", "configure_network", nil, func() error { newCalls.Add(1); return nil })
	close(release)
	if awaitJob(t, jm, "2").Status != JobStatusFailed {
		t.Fatal("obsolete pending configuration executed")
	}
	awaitJob(t, jm, "3")
	jm.StartJob("0", "configure_network", nil, func() error { oldCalls.Add(1); return nil })
	if awaitJob(t, jm, "0").Status != JobStatusFailed || oldCalls.Load() != 0 || newCalls.Load() != 1 {
		t.Fatal("late configuration overwrote newer state")
	}
}

func TestPruningPreservesDurableAndStopBarriersAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	jm := NewJobManager()
	if err := jm.LoadState(path); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct{ id, kind string }{{"10", "configure_network"}, {"11", "stop"}} {
		jm.StartJob(entry.id, entry.kind, nil, func() error { return nil })
		awaitJob(t, jm, entry.id)
	}
	jm.mu.Lock()
	for _, job := range jm.jobs {
		job.UpdatedAt = time.Now().Add(-48 * time.Hour)
	}
	jm.mu.Unlock()
	jm.Acknowledge([]string{"10", "11"})
	reloaded := NewJobManager()
	if err := reloaded.LoadState(path); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	action := func() error { calls.Add(1); return nil }
	reloaded.StartJob("10", "configure_network", nil, action)
	reloaded.StartJob("9", "configure_network", nil, action)
	reloaded.StartJob("8", "test_drive", nil, action)
	awaitJob(t, reloaded, "9")
	awaitJob(t, reloaded, "8")
	if calls.Load() != 0 {
		t.Fatal("pruning allowed stale configuration or movement to execute")
	}
}

func TestCancelledBatchCannotReportSuccess(t *testing.T) {
	jm := NewJobManager()
	started := make(chan struct{})
	jm.StartContextJob("1", "batch", nil, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return nil // A non-context-aware final step may finish normally after stop.
	})
	<-started
	jm.StartJob("2", "stop", nil, func() error { return nil })
	if job := awaitJob(t, jm, "1"); job.Status != JobStatusFailed || job.Error != context.Canceled.Error() {
		t.Fatalf("cancelled batch reported success: %+v", job)
	}
	awaitJob(t, jm, "2")
}

func TestConcurrentIdentifyRetriesExecuteOnce(t *testing.T) {
	jm := NewJobManager()
	var calls atomic.Int32
	var deliveries sync.WaitGroup
	release := make(chan struct{})
	action := func() error { calls.Add(1); <-release; return nil }
	for i := 0; i < 32; i++ {
		deliveries.Add(1)
		go func() {
			defer deliveries.Done()
			if err := jm.StartJob("42", "identify", nil, action); err != nil {
				t.Error(err)
			}
		}()
	}
	deliveries.Wait()
	close(release)
	awaitJob(t, jm, "42")
	jm.Acknowledge([]string{"42"})
	if err := jm.StartJob("42", "identify", nil, action); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("identify executed %d times", calls.Load())
	}
}
