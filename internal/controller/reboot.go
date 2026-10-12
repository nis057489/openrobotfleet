package controller

import (
	"context"
	"errors"
	"log"
	"time"

	"example.com/openrobot-fleet/internal/agent"
	"example.com/openrobot-fleet/internal/db"
)

// rebootTimeout bounds how long a batch waits for a device to come back.
const rebootTimeout = 10 * time.Minute

// deviceReport is boot state from a device's latest heartbeat.
type deviceReport struct {
	RebootRequired bool
	BootID         string
}

// RecordDeviceReport stores boot state from a heartbeat. rebootRequired is
// nil from agents that predate it. Call it before the heartbeat's job results
// are processed, so whoever sees a job finish also sees the state the device
// reported at that moment.
func (c *Controller) RecordDeviceReport(agentID string, rebootRequired *bool, bootID string) {
	if rebootRequired == nil {
		return
	}
	c.deviceReportsMu.Lock()
	defer c.deviceReportsMu.Unlock()
	if c.deviceReports == nil {
		c.deviceReports = map[string]deviceReport{}
	}
	c.deviceReports[agentID] = deviceReport{RebootRequired: *rebootRequired, BootID: bootID}
}

func (c *Controller) deviceReport(agentID string) (deviceReport, bool) {
	c.deviceReportsMu.RLock()
	defer c.deviceReportsMu.RUnlock()
	r, ok := c.deviceReports[agentID]
	return r, ok
}

// rebootIfRequired reboots a device that reports a pending reboot (after a
// kernel, libc or driver upgrade) and waits until it is back on a new boot.
func (c *Controller) rebootIfRequired(ctx context.Context, robot db.Robot, setState func(string)) error {
	report, ok := c.deviceReport(robot.AgentID)
	if !ok {
		log.Printf("semester: %s does not report whether it needs a reboot; skipping", robot.Name)
		return nil
	}
	if !report.RebootRequired {
		return nil
	}
	log.Printf("semester: rebooting %s to finish its upgrade", robot.Name)
	setState("rebooting")
	cmd := agent.Command{Type: "delayed_reboot", Data: []byte("{}")}
	if _, err := c.runRobotCommand(ctx, robot, cmd); err != nil {
		return err
	}
	setState("waiting_for_reboot")
	deadline := time.After(rebootTimeout)
	for {
		select {
		case <-ctx.Done():
			return errors.New("cancelled while waiting for reboot")
		case <-deadline:
			return errors.New("did not come back within 10 minutes of rebooting")
		case <-time.After(2 * time.Second):
		}
		if r, ok := c.deviceReport(robot.AgentID); ok && r.BootID != "" && r.BootID != report.BootID {
			log.Printf("semester: %s is back after rebooting", robot.Name)
			return nil
		}
	}
}
