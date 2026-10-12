package controller

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"example.com/openrobot-fleet/internal/agent"
	"example.com/openrobot-fleet/internal/db"
)

func TestTimeConformanceQueuesOnlyWhenNeeded(t *testing.T) {
	c := testController(t)
	ctx := context.Background()
	if err := c.DB.UpsertRobotWithType(ctx, "robot-a", "tb30", "192.168.100.30", "ok", "robot"); err != nil {
		t.Fatal(err)
	}
	str := func(s string) *string { return &s }
	jobs := func() []db.Job {
		t.Helper()
		queued, err := c.DB.QueuedJobs(ctx, "robot-a")
		if err != nil {
			t.Fatal(err)
		}
		return queued
	}

	c.EnsureTimeConformance("robot-a", str("ntp.ubuntu.com"), false)
	if n := len(jobs()); n != 0 {
		t.Fatalf("queued %d jobs with no NTP server configured", n)
	}

	if err := c.DB.SaveTimeConfig(ctx, db.TimeConfig{NTPServer: "192.168.100.1"}); err != nil {
		t.Fatal(err)
	}
	c.EnsureTimeConformance("robot-a", nil, false)
	if n := len(jobs()); n != 0 {
		t.Fatalf("queued %d jobs for an agent that cannot apply them", n)
	}

	c.EnsureTimeConformance("robot-a", str(""), false)
	c.EnsureTimeConformance("robot-a", str(""), false) // next heartbeat, job still in flight
	queued := jobs()
	if len(queued) != 1 || queued[0].Type != "configure_time" {
		t.Fatalf("want one configure_time job, got %+v", queued)
	}
	var cmd agent.Command
	var data agent.ConfigureTimeData
	if err := json.Unmarshal([]byte(queued[0].PayloadJSON), &cmd); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(cmd.Data, &data); err != nil || data.NTPServer != "192.168.100.1" {
		t.Fatalf("payload %s: %v", cmd.Data, err)
	}

	// Applied: the device now reports the right server, so nothing more.
	c.ProcessJobResults("robot-a", []agent.Job{{ID: strconv.FormatInt(queued[0].ID, 10), Status: agent.JobStatusSuccess}})
	c.EnsureTimeConformance("robot-a", str("192.168.100.1"), true)
	if n := len(jobs()); n != 0 {
		t.Fatalf("queued %d jobs for a conformant device", n)
	}
}
