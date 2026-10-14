package controller

import (
	"context"
	"testing"

	"example.com/openrobot-fleet/internal/db"
)

func TestFixRobotQueuesResyncThenRestartOnGroupRobot(t *testing.T) {
	c := testController(t)
	ctx := context.Background()
	c.DB.UpsertRobotWithType(ctx, "robot-a", "tb34", "192.168.100.34", "ok", "robot")
	c.DB.UpsertRobotWithType(ctx, "laptop-a", "Tesla", "192.168.100.134", "ok", "laptop")
	c.DB.UpsertRobotWithType(ctx, "laptop-b", "Lonely", "192.168.100.139", "ok", "laptop")
	robot, _ := c.DB.GetRobotByAgentID(ctx, "robot-a")
	laptop, _ := c.DB.GetRobotByAgentID(ctx, "laptop-a")
	if _, err := c.DB.CreateGroup(ctx, db.Group{Name: "34", ROSDomainID: 34, RobotID: &robot.ID, LaptopID: &laptop.ID}); err != nil {
		t.Fatal(err)
	}

	c.HandleDeviceRequests("laptop-b", []string{"fix_robot"}) // no group: nothing
	c.HandleDeviceRequests("laptop-a", []string{"fix_robot"})
	c.HandleDeviceRequests("laptop-a", []string{"fix_robot"}) // pressed twice: rate limited
	jobs, err := c.DB.QueuedJobs(ctx, "robot-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 || jobs[0].Type != "resync_time" || jobs[1].Type != "restart_ros" {
		t.Fatalf("queued %+v", jobs)
	}
}
