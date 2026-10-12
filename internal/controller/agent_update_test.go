package controller

import (
	"context"
	"testing"
	"time"

	"example.com/openrobot-fleet/internal/db"
)

func TestOutdatedAgentsAreReinstalledOneAtATime(t *testing.T) {
	old := shippedAgentHashes
	shippedAgentHashes = func() map[string]bool { return map[string]bool{"current": true} }
	t.Cleanup(func() { shippedAgentHashes = old })

	c := testController(t)
	ctx := context.Background()
	for _, r := range []struct{ agentID, name string }{{"robot-a", "tb30"}, {"robot-b", "tb31"}, {"robot-c", "tb32"}} {
		if err := c.DB.UpsertRobotWithType(ctx, r.agentID, r.name, "192.168.100.30", "ok", "robot"); err != nil {
			t.Fatal(err)
		}
	}
	started := make(chan string, 4)
	finish := make(chan struct{})
	c.reinstall = func(_ context.Context, robot *db.Robot) (time.Time, error) {
		started <- robot.Name
		<-finish
		return time.Now(), nil
	}
	waitStarted := func(want string) {
		t.Helper()
		select {
		case got := <-started:
			if got != want {
				t.Fatalf("reinstalled %s, want %s", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s was not reinstalled", want)
		}
	}
	noneStarted := func() {
		t.Helper()
		select {
		case got := <-started:
			t.Fatalf("unexpected reinstall of %s", got)
		case <-time.After(50 * time.Millisecond):
		}
	}

	c.EnsureAgentCurrent("robot-a", "current")
	noneStarted()

	c.UpdateRobotJobStatus("robot-c", "7", "running", "")
	c.EnsureAgentCurrent("robot-c", "")
	noneStarted() // busy with a job

	c.EnsureAgentCurrent("robot-b", "") // predates the hash field
	waitStarted("tb31")
	c.EnsureAgentCurrent("robot-b", "")  // next heartbeat while updating
	c.EnsureAgentCurrent("robot-a", "x") // slot taken by robot-b
	noneStarted()

	close(finish)
	// Once robot-b's update finishes, robot-a (no attempt recorded) gets the
	// slot on its next heartbeat; robot-b waits out the retry interval.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if u, ok := c.agentUpdateFor(mustRobotID(t, c, "robot-b")); ok && u.State == "updated" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("robot-b update never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.EnsureAgentCurrent("robot-b", "")
	c.EnsureAgentCurrent("robot-a", "x")
	waitStarted("tb30")
	noneStarted()
}

func mustRobotID(t *testing.T, c *Controller, agentID string) int64 {
	t.Helper()
	r, err := c.DB.GetRobotByAgentID(context.Background(), agentID)
	if err != nil {
		t.Fatal(err)
	}
	return r.ID
}
