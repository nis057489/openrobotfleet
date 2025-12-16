package agent

import (
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
	ID        string
	Type      string
	Data      []byte
	Status    JobStatus
	Error     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type JobManager struct {
	mu   sync.RWMutex
	jobs map[string]*Job
	// currentJob is a pointer to the currently running job, if any
	currentJob *Job
}

func NewJobManager() *JobManager {
	return &JobManager{
		jobs: make(map[string]*Job),
	}
}

func (jm *JobManager) StartJob(id, jobType string, data []byte, action func() error) {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	if jm.currentJob != nil && jm.currentJob.Status == JobStatusRunning {
		// For now, reject if busy.
		return
	}

	job := &Job{
		ID:        id,
		Type:      jobType,
		Data:      data,
		Status:    JobStatusRunning,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	jm.jobs[id] = job
	jm.currentJob = job

	go func() {
		err := action()
		jm.mu.Lock()
		defer jm.mu.Unlock()

		job.UpdatedAt = time.Now()
		if err != nil {
			job.Status = JobStatusFailed
			job.Error = err.Error()
		} else {
			job.Status = JobStatusSuccess
		}

		if jm.currentJob == job {
			jm.currentJob = nil
		}
	}()
}

func (jm *JobManager) GetJob(id string) *Job {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	return jm.jobs[id]
}

func (jm *JobManager) GetCurrentJob() *Job {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	return jm.currentJob
}
