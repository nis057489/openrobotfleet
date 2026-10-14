package controller

import (
	"context"
	"log"
	"time"

	"example.com/openrobot-fleet/internal/agent"
)

// fixRobotInterval stops repeated button presses queueing a pile of restarts.
const fixRobotInterval = 30 * time.Second

// HandleDeviceRequests acts on requests an agent relays from its desktop
// session, such as the laptop clock indicator's "fix robot" button.
func (c *Controller) HandleDeviceRequests(agentID string, requests []string) {
	for _, r := range requests {
		switch r {
		case "fix_robot":
			c.fixGroupRobot(agentID)
		default:
			log.Printf("device request: unknown %q from %s", r, agentID)
		}
	}
}

// fixGroupRobot resyncs the clock of the laptop's group robot and then
// restarts its ROS, so its nodes stamp data in the corrected time.
func (c *Controller) fixGroupRobot(laptopAgentID string) {
	ctx := context.Background()
	laptop, err := c.DB.GetRobotByAgentID(ctx, laptopAgentID)
	if err != nil {
		return
	}
	groups, err := c.DB.ListGroups(ctx)
	if err != nil {
		log.Printf("fix robot: list groups: %v", err)
		return
	}
	var robotID *int64
	for _, g := range groups {
		if g.LaptopID != nil && *g.LaptopID == laptop.ID {
			robotID = g.RobotID
			break
		}
	}
	if robotID == nil {
		log.Printf("fix robot: %s is not in a group with a robot", laptop.Name)
		return
	}
	robot, err := c.DB.GetRobotByID(ctx, *robotID)
	if err != nil || robot.AgentID == "" {
		return
	}

	c.fixRequestsMu.Lock()
	if c.fixRequests == nil {
		c.fixRequests = map[int64]time.Time{}
	}
	if time.Since(c.fixRequests[robot.ID]) < fixRobotInterval {
		c.fixRequestsMu.Unlock()
		return
	}
	c.fixRequests[robot.ID] = time.Now()
	c.fixRequestsMu.Unlock()

	log.Printf("fix robot: %s asked to resync %s's clock and restart its ROS", laptop.Name, robot.Name)
	for _, typ := range []string{"resync_time", "restart_ros"} {
		if _, err := c.queueRobotCommand(ctx, robot, agent.Command{Type: typ, Data: []byte("{}")}); err != nil {
			log.Printf("fix robot: queue %s for %s: %v", typ, robot.Name, err)
			return
		}
	}
}
