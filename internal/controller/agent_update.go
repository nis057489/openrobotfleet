package controller

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"example.com/openrobot-fleet/internal/agent"
	"example.com/openrobot-fleet/internal/db"
)

const (
	// agentUpdateRetry spaces out attempts on one device, so a device that
	// cannot be reached over SSH, or that still reports another build after
	// an update, is not reinstalled on every heartbeat.
	agentUpdateRetry   = time.Hour
	agentUpdateTimeout = 10 * time.Minute
)

// agentUpdate is the most recent automatic update of one device's agent.
type agentUpdate struct {
	State string // "updating", "updated" or "failed"
	Error string
	At    time.Time
}

// shippedAgentHashes fingerprints the agent builds this controller installs.
// An agent reporting any other hash, or none (agents that predate the
// field), is out of date. A var so tests can replace it.
var shippedAgentHashes = sync.OnceValue(func() map[string]bool {
	hashes := map[string]bool{}
	for _, arch := range []string{"amd64", "arm64"} {
		if sum := agent.FileSHA256(agentBinaryPath(arch)); sum != "" {
			hashes[sum] = true
		}
	}
	return hashes
})

// EnsureAgentCurrent runs on every heartbeat and reinstalls, over SSH with
// the device's saved credentials, an agent that is not the build this
// controller ships. Updates run one at a time in the background, never on a
// device that is running a job or held by a semester batch. Set
// AGENT_AUTO_UPDATE=false to turn this off.
func (c *Controller) EnsureAgentCurrent(agentID, reportedSHA256 string) {
	if os.Getenv("AGENT_AUTO_UPDATE") == "false" || os.Getenv("DEMO_MODE") == "true" {
		return
	}
	shipped := shippedAgentHashes()
	if len(shipped) == 0 {
		return // no agent builds here to compare with (development)
	}
	robot, err := c.DB.GetRobotByAgentID(context.Background(), agentID)
	if err != nil {
		return
	}
	if shipped[reportedSHA256] {
		c.agentUpdatesMu.Lock()
		delete(c.agentUpdates, robot.ID)
		c.agentUpdatesMu.Unlock()
		return
	}
	c.jobStatesMu.RLock()
	job := c.jobStates[agentID]
	c.jobStatesMu.RUnlock()
	if job.JobStatus == string(agent.JobStatusRunning) || job.JobStatus == string(agent.JobStatusPending) {
		return // retried on a later heartbeat, once the device is idle
	}
	if !c.claimAgentUpdate(robot.ID) {
		return
	}
	go c.runAgentUpdate(robot)
}

// claimAgentUpdate takes the single update slot for robotID, unless this
// device was tried recently or another update is running.
func (c *Controller) claimAgentUpdate(robotID int64) bool {
	c.agentUpdatesMu.Lock()
	defer c.agentUpdatesMu.Unlock()
	if c.agentUpdates == nil {
		c.agentUpdates = map[int64]agentUpdate{}
		c.agentUpdateSlot = make(chan struct{}, 1)
	}
	if last, ok := c.agentUpdates[robotID]; ok && (last.State == "updating" || time.Since(last.At) < agentUpdateRetry) {
		return false
	}
	select {
	case c.agentUpdateSlot <- struct{}{}:
	default:
		return false
	}
	c.agentUpdates[robotID] = agentUpdate{State: "updating", At: time.Now()}
	return true
}

func (c *Controller) runAgentUpdate(robot db.Robot) {
	defer func() { <-c.agentUpdateSlot }()
	ctx, cancel := context.WithTimeout(context.Background(), agentUpdateTimeout)
	defer cancel()

	// Never interleave with a semester batch on this device; if one holds
	// it, step aside and let a later heartbeat try again.
	tryCtx, tryCancel := context.WithTimeout(ctx, time.Second)
	release, err := c.acquireBatchDevice(tryCtx, robot.ID)
	tryCancel()
	if err != nil {
		c.agentUpdatesMu.Lock()
		delete(c.agentUpdates, robot.ID)
		c.agentUpdatesMu.Unlock()
		return
	}
	defer release()

	log.Printf("agent update: %s is running an outdated agent; reinstalling", robot.Name)
	reinstall := c.reinstall
	if reinstall == nil {
		reinstall = c.reinstallAgent
	}
	result := agentUpdate{State: "updated", At: time.Now()}
	if _, err := reinstall(ctx, &robot); err != nil {
		log.Printf("agent update: %s: %v", robot.Name, err)
		result = agentUpdate{State: "failed", Error: err.Error(), At: time.Now()}
	} else {
		log.Printf("agent update: %s updated", robot.Name)
	}
	c.agentUpdatesMu.Lock()
	c.agentUpdates[robot.ID] = result
	c.agentUpdatesMu.Unlock()
}

// agentUpdateFor returns robotID's latest automatic update, if any.
func (c *Controller) agentUpdateFor(robotID int64) (agentUpdate, bool) {
	c.agentUpdatesMu.Lock()
	defer c.agentUpdatesMu.Unlock()
	u, ok := c.agentUpdates[robotID]
	return u, ok
}
