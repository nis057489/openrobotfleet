package controller

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// completedBatchRetention caps how many finished runs are kept for review.
// Runs live in memory only, so this list resets when the controller restarts.
const completedBatchRetention = 20

// BatchRun is a single semester batch. Several can be in flight at once, so
// each run owns its own progress map and cancel function rather than sharing
// one global "is a batch running" flag.
type BatchRun struct {
	ID         string           `json:"id"`
	Label      string           `json:"label"`
	Total      int              `json:"total"`
	Completed  int              `json:"completed"`
	Robots     map[int64]string `json:"robots"`
	Errors     map[int64]string `json:"errors"`
	StartedAt  time.Time        `json:"started_at"`
	FinishedAt *time.Time       `json:"finished_at,omitempty"`
	Cancelled  bool             `json:"cancelled"`
	Active     bool             `json:"active"`

	cancel context.CancelFunc
}

// batchRegistry guards every field of every run it holds. Worker goroutines
// mutate runs only through its methods, which keeps progress writes and the
// JSON snapshot read from racing.
type batchRegistry struct {
	mu     sync.RWMutex
	runs   []*BatchRun // oldest first
	nextID int64
}

var batches = &batchRegistry{}

func (r *batchRegistry) add(label string, ids []int64, cancel context.CancelFunc) *BatchRun {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID++
	run := &BatchRun{
		ID:        fmt.Sprintf("b%d-%d", time.Now().Unix(), r.nextID),
		Label:     label,
		Total:     len(ids),
		Robots:    make(map[int64]string, len(ids)),
		Errors:    make(map[int64]string),
		StartedAt: time.Now(),
		Active:    true,
		cancel:    cancel,
	}
	for _, id := range ids {
		run.Robots[id] = "pending"
	}
	r.runs = append(r.runs, run)
	r.trimLocked()
	return run
}

// trimLocked drops the oldest finished runs once there are more than the
// retention limit. Active runs are never trimmed.
func (r *batchRegistry) trimLocked() {
	finished := 0
	for _, run := range r.runs {
		if !run.Active {
			finished++
		}
	}
	if finished <= completedBatchRetention {
		return
	}
	drop := finished - completedBatchRetention
	kept := r.runs[:0]
	for _, run := range r.runs {
		if !run.Active && drop > 0 {
			drop--
			continue
		}
		kept = append(kept, run)
	}
	r.runs = kept
}

func (r *batchRegistry) get(id string) *BatchRun {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, run := range r.runs {
		if run.ID == id {
			return run
		}
	}
	return nil
}

func (r *batchRegistry) setRobotState(run *BatchRun, id int64, state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run.Robots[id] = state
}

// failRobot records a terminal error for one device and counts it as done.
// A run that was cancelled reports "cancelled" instead, so an operator can
// tell a deliberate stop apart from a real failure.
func (r *batchRegistry) failRobot(run *BatchRun, id int64, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if run.Cancelled {
		run.Robots[id] = "cancelled"
	} else {
		run.Robots[id] = "error"
		run.Errors[id] = msg
	}
	run.Completed++
}

func (r *batchRegistry) completeRobot(run *BatchRun, id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run.Robots[id] = "success"
	run.Completed++
}

func (r *batchRegistry) finish(run *BatchRun) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	run.FinishedAt = &now
	run.Active = false
	r.trimLocked()
}

// cancel stops a run's in-flight waits. Work already handed to a device keeps
// running there -- this stops the controller waiting on it and prevents any
// later steps in the batch from being queued.
func (r *batchRegistry) cancelRun(id string) bool {
	r.mu.Lock()
	var run *BatchRun
	for _, candidate := range r.runs {
		if candidate.ID == id {
			run = candidate
			break
		}
	}
	if run == nil || !run.Active {
		r.mu.Unlock()
		return false
	}
	run.Cancelled = true
	cancel := run.cancel
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return true
}

// snapshot returns deep copies safe to marshal outside the lock.
func (r *batchRegistry) snapshot() []BatchRun {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]BatchRun, 0, len(r.runs))
	// Newest first: the list is append-ordered, so walk it backwards.
	for i := len(r.runs) - 1; i >= 0; i-- {
		run := r.runs[i]
		copied := *run
		copied.cancel = nil
		copied.Robots = make(map[int64]string, len(run.Robots))
		copied.Errors = make(map[int64]string, len(run.Errors))
		for k, v := range run.Robots {
			copied.Robots[k] = v
		}
		for k, v := range run.Errors {
			copied.Errors[k] = v
		}
		out = append(out, copied)
	}
	return out
}

// activityFor reports what an active batch is currently doing to one device,
// so robot and laptop views can show real work instead of a stale "ok".
func (r *batchRegistry) activityFor(robotID int64) (state string, label string, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for i := len(r.runs) - 1; i >= 0; i-- {
		run := r.runs[i]
		if !run.Active {
			continue
		}
		state, present := run.Robots[robotID]
		if !present {
			continue
		}
		// A device already done in this run may still be busy in an older one.
		if state == "success" || state == "error" || state == "cancelled" {
			continue
		}
		return state, run.Label, true
	}
	return "", "", false
}

// anyActive reports whether any run is still going, for the dashboard summary.
func (r *batchRegistry) anyActive() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, run := range r.runs {
		if run.Active {
			return true
		}
	}
	return false
}

// batchLabel summarises the selected actions for display, e.g.
// "System upgrade + Reset .bashrc".
func batchLabel(req semesterRequest) string {
	var parts []string
	if req.Reinstall {
		parts = append(parts, "Reinstall agent")
	}
	if req.ResetLogs {
		parts = append(parts, "Reset logs")
	}
	if req.ResetBashrc {
		parts = append(parts, "Reset .bashrc")
	}
	if req.UpdateRepo {
		parts = append(parts, "Update repo")
	}
	if req.ApplyScenarios {
		parts = append(parts, "Apply scenarios")
	}
	if req.RunSelfTest {
		parts = append(parts, "Self test")
	}
	if req.SystemUpgrade {
		parts = append(parts, "System upgrade")
	}
	if req.InstallPackages {
		parts = append(parts, "Install packages")
	}
	if req.InstallCameraSupport {
		parts = append(parts, "Camera support")
	}
	if req.FactoryReset {
		parts = append(parts, "Factory reset")
	}
	if len(parts) == 0 {
		return "Batch operation"
	}
	return strings.Join(parts, " + ")
}

// ListBatches returns every tracked run, newest first.
func (c *Controller) ListBatches(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, http.StatusOK, map[string]any{"batches": batches.snapshot()})
}

// CancelBatch stops one run by id.
func (c *Controller) CancelBatch(w http.ResponseWriter, r *http.Request) {
	id, err := parseBatchIDFromPath(r.URL.Path)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid batch id")
		return
	}
	if !batches.cancelRun(id) {
		respondError(w, http.StatusNotFound, "no active batch with that id")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "cancelling"})
}

// parseBatchIDFromPath pulls <id> out of /api/batches/<id>/cancel.
func parseBatchIDFromPath(path string) (string, error) {
	tail := strings.TrimPrefix(path, "/api/batches/")
	tail = strings.TrimSuffix(tail, "/cancel")
	tail = strings.Trim(tail, "/")
	if tail == "" || strings.Contains(tail, "/") {
		return "", fmt.Errorf("invalid batch id")
	}
	return tail, nil
}
