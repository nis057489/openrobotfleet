package controller

import (
	"context"
	"strconv"
	"testing"
	"time"

	"example.com/openrobot-fleet/internal/agent"
	"example.com/openrobot-fleet/internal/db"
)

func TestRebootIfRequired(t *testing.T) {
	c := testController(t)
	ctx := context.Background()
	robot := db.Robot{Name: "tb30", AgentID: "robot-a"}
	yes, no := true, false
	var states []string
	setState := func(s string) { states = append(states, s) }

	c.RecordDeviceReport("robot-a", &no, "boot-1")
	if err := c.rebootIfRequired(ctx, robot, setState); err != nil || len(states) != 0 {
		t.Fatalf("rebooted a device that did not need it: %v %v", err, states)
	}

	// Simulated agent: acknowledges the delayed_reboot job, then comes back
	// on a new boot.
	c.RecordDeviceReport("robot-a", &yes, "boot-1")
	go func() {
		for {
			jobs, _ := c.DB.QueuedJobs(ctx, "robot-a")
			if len(jobs) == 1 && jobs[0].Type == "delayed_reboot" {
				c.RecordDeviceReport("robot-a", &yes, "boot-1")
				c.ProcessJobResults("robot-a", []agent.Job{{ID: strconv.FormatInt(jobs[0].ID, 10), Status: agent.JobStatusSuccess}})
				time.Sleep(100 * time.Millisecond)
				c.RecordDeviceReport("robot-a", &no, "boot-2")
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	done := make(chan error, 1)
	go func() { done <- c.rebootIfRequired(ctx, robot, setState) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("never saw the device come back")
	}
	if len(states) != 2 || states[0] != "rebooting" || states[1] != "waiting_for_reboot" {
		t.Fatalf("states = %v", states)
	}
}
