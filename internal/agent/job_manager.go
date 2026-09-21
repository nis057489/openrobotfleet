package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

type JobStatus string

const (
	JobStatusPending JobStatus = "pending"
	JobStatusRunning JobStatus = "running"
	JobStatusSuccess JobStatus = "success"
	JobStatusFailed  JobStatus = "failed"
)

type Job struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Data         []byte    `json:"data,omitempty"`
	Status       JobStatus `json:"status"`
	Error        string    `json:"error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Acknowledged bool      `json:"acknowledged,omitempty"`
	action       func(context.Context) error
	cancel       context.CancelFunc
}

type JobManager struct {
	mu         sync.RWMutex
	jobs       map[string]*Job
	queue      []*Job
	currentJob *Job
	latest     *Job
	stopping   int
	statePath  string
	lastStopID int64
}

func NewJobManager() *JobManager { return &JobManager{jobs: make(map[string]*Job)} }

// LoadState never replays an action interrupted by a process restart. Report
// its uncertain outcome instead, especially for reboot and movement commands.
func (jm *JobManager) LoadState(path string) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	jm.statePath = path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &jm.jobs); err != nil {
		return err
	}
	if jm.jobs == nil {
		jm.jobs = make(map[string]*Job)
	}
	for _, job := range jm.jobs {
		if job.Type == "stop" {
			if id, err := strconv.ParseInt(job.ID, 10, 64); err == nil && id > jm.lastStopID {
				jm.lastStopID = id
			}
		}
		if job.Status == JobStatusPending || job.Status == JobStatusRunning {
			job.Status = JobStatusFailed
			job.Error = "agent restarted before completion was confirmed"
			job.UpdatedAt = time.Now()
			job.Acknowledged = false
		}
		if jm.latest == nil || job.UpdatedAt.After(jm.latest.UpdatedAt) {
			jm.latest = job
		}
	}
	return jm.saveLocked()
}

func (jm *JobManager) saveLocked() error {
	if jm.statePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(jm.statePath), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(jm.jobs)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(jm.statePath), ".jobs-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), jm.statePath)
}

func (jm *JobManager) StartJob(id, jobType string, data []byte, action func() error) error {
	return jm.StartContextJob(id, jobType, data, func(context.Context) error { return action() })
}

func (jm *JobManager) StartContextJob(id, jobType string, data []byte, action func(context.Context) error) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	if existing := jm.jobs[id]; existing != nil {
		log.Printf("[agent] duplicate delivery of job %s (%s): returning %s without executing again", id, existing.Type, existing.Status)
		existing.Acknowledged = false // Re-send the result if the controller retries.
		return nil
	}
	if len(jm.queue) >= 1000 && jobType != "stop" {
		return errors.New("agent job queue full")
	}
	now := time.Now()
	job := &Job{ID: id, Type: jobType, Data: append([]byte(nil), data...), Status: JobStatusPending, CreatedAt: now, UpdatedAt: now, action: action}
	if sequence, err := strconv.ParseInt(id, 10, 64); err == nil {
		if jobType == "stop" && sequence > jm.lastStopID {
			jm.lastStopID = sequence
		}
		if (jobType == "test_drive" || jobType == "batch") && sequence < jm.lastStopID {
			job.action = func(context.Context) error { return errors.New("cancelled by a newer stop command") }
		}
	}
	jm.jobs[id] = job
	if err := jm.saveLocked(); err != nil {
		if jobType != "stop" {
			delete(jm.jobs, id)
			return err
		}
		// Emergency stopping must still work on a full or read-only filesystem.
		log.Printf("cannot journal emergency stop %s: %v", id, err)
	}
	if jobType == "stop" {
		if jm.currentJob != nil && (jm.currentJob.Type == "test_drive" || jm.currentJob.Type == "batch") {
			jm.currentJob.cancel()
		}
		kept := jm.queue[:0]
		for _, queued := range jm.queue {
			if queued.Type == "test_drive" || queued.Type == "batch" {
				queued.Status = JobStatusFailed
				queued.Error = "cancelled by stop"
				queued.UpdatedAt = now
			} else {
				kept = append(kept, queued)
			}
		}
		jm.queue = kept
		jm.stopping++
		jm.runLocked(job, true)
	} else {
		jm.queue = append(jm.queue, job)
		jm.startNextLocked()
	}
	return nil
}

func (jm *JobManager) startNextLocked() {
	if jm.currentJob != nil || jm.stopping > 0 || len(jm.queue) == 0 {
		return
	}
	job := jm.queue[0]
	jm.queue = jm.queue[1:]
	jm.currentJob = job
	jm.runLocked(job, false)
}

func (jm *JobManager) runLocked(job *Job, emergency bool) {
	if durableCommandTypes[job.Type] {
		if id, err := strconv.ParseInt(job.ID, 10, 64); err == nil && id < jm.latestSequenceLocked(job.Type) {
			job.action = func(context.Context) error { return errors.New("superseded by a newer " + job.Type + " command") }
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel
	job.Status = JobStatusRunning
	job.UpdatedAt = time.Now()
	go func() {
		log.Printf("[agent] executing job %s (%s)", job.ID, job.Type)
		err := job.action(ctx)
		jm.mu.Lock()
		defer jm.mu.Unlock()
		if err == nil {
			err = ctx.Err()
		}
		cancel()
		job.UpdatedAt = time.Now()
		job.Status = JobStatusSuccess
		if err != nil {
			job.Status = JobStatusFailed
			job.Error = err.Error()
		}
		job.action = nil
		job.cancel = nil
		jm.latest = job
		if emergency {
			jm.stopping--
		} else if jm.currentJob == job {
			jm.currentJob = nil
		}
		if err := jm.saveLocked(); err != nil {
			job.Status = JobStatusFailed
			job.Error = fmt.Sprintf("save job result: %v", err)
		}
		jm.startNextLocked()
	}()
}

func (jm *JobManager) latestSequenceLocked(jobType string) int64 {
	var latest int64
	for _, job := range jm.jobs {
		if job.Type == jobType {
			if id, err := strconv.ParseInt(job.ID, 10, 64); err == nil && id > latest {
				latest = id
			}
		}
	}
	return latest
}

func copyJob(job *Job) *Job {
	if job == nil {
		return nil
	}
	snapshot := *job
	snapshot.Data = append([]byte(nil), job.Data...)
	snapshot.action = nil
	snapshot.cancel = nil
	return &snapshot
}
func (jm *JobManager) GetJob(id string) *Job {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	return copyJob(jm.jobs[id])
}
func (jm *JobManager) GetCurrentJob() *Job {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	if jm.currentJob != nil {
		return copyJob(jm.currentJob)
	}
	return copyJob(jm.latest)
}
func (jm *JobManager) Results() []Job {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	results := []Job{}
	for _, job := range jm.jobs {
		if !job.Acknowledged {
			result := *copyJob(job)
			result.Data = nil
			results = append(results, result)
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].CreatedAt.Before(results[j].CreatedAt) })
	return results
}
func (jm *JobManager) Acknowledge(ids []string) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	for _, id := range ids {
		if job := jm.jobs[id]; job != nil && (job.Status == JobStatusSuccess || job.Status == JobStatusFailed) {
			job.Acknowledged = true
		}
	}
	// Retain acknowledged IDs through the command expiry window to suppress
	// delayed deliveries, without letting the journal grow forever.
	keep := map[string]int64{"stop": jm.latestSequenceLocked("stop")}
	for kind := range durableCommandTypes {
		keep[kind] = jm.latestSequenceLocked(kind)
	}
	for id, job := range jm.jobs {
		sequence, _ := strconv.ParseInt(id, 10, 64)
		// Durable commands never expire. Keep their newest ID (and the stop
		// barrier) across pruning/restarts so late delivery cannot undo them.
		barrier, protected := keep[job.Type]
		if protected && sequence == barrier {
			continue
		}
		if job.Acknowledged && time.Since(job.UpdatedAt) > 24*time.Hour {
			delete(jm.jobs, id)
		}
	}
	_ = jm.saveLocked()
}
