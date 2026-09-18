package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"example.com/openrobot-fleet/internal/agent"
	"example.com/openrobot-fleet/internal/db"
)

func (c *Controller) publishJob(job db.Job) error {
	var cmd agent.Command
	if err := json.Unmarshal([]byte(job.PayloadJSON), &cmd); err != nil {
		return err
	}
	cmd.ID = strconv.FormatInt(job.ID, 10)
	payload, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	return c.MQTT.Publish("lab/commands/"+job.TargetRobot, 1, false, payload)
}

func (c *Controller) ProcessJobResults(agentID string, results []agent.Job) {
	ctx := context.Background()
	ack := []string{}
	for _, result := range results {
		id, err := strconv.ParseInt(result.ID, 10, 64)
		if err != nil {
			continue
		}
		if err = c.DB.RecordJobResult(ctx, agentID, id, string(result.Status), result.Error); err != nil {
			log.Printf("record job %s from %s: %v", result.ID, agentID, err)
			continue
		}
		if result.Status == agent.JobStatusSuccess || result.Status == agent.JobStatusFailed {
			ack = append(ack, result.ID)
		}
	}
	if len(ack) > 0 {
		data, _ := json.Marshal(ack)
		if err := c.MQTT.Publish("lab/acks/"+agentID, 1, false, data); err != nil {
			log.Printf("ack jobs: %v", err)
		}
	}
}

func (c *Controller) DeliverQueuedJobs(agentID string) {
	jobs, err := c.DB.QueuedJobs(context.Background(), agentID)
	if err != nil {
		log.Printf("read outbox: %v", err)
		return
	}
	for _, job := range jobs {
		if job.Type != "configure_network" && job.Type != "set_hostname" && time.Since(job.CreatedAt) > 10*time.Minute {
			_ = c.DB.RecordJobResult(context.Background(), agentID, job.ID, "failed", "command expired before delivery")
			continue
		}
		if err := c.publishJob(job); err != nil {
			log.Printf("deliver job %d: %v", job.ID, err)
			return
		}
	}
}

func (c *Controller) waitForJob(ctx context.Context, id int64) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		job, err := c.DB.GetJob(ctx, id)
		if err != nil {
			return err
		}
		switch job.Status {
		case "success":
			return nil
		case "failed":
			return fmt.Errorf("%s failed: %s", job.Type, job.Error)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("job %d completion unconfirmed: %w", id, ctx.Err())
		case <-ticker.C:
		}
	}
}
func (c *Controller) runRobotCommand(ctx context.Context, robot db.Robot, cmd agent.Command) (db.Job, error) {
	job, err := c.queueRobotCommand(ctx, robot, cmd)
	if err != nil {
		return job, err
	}
	waitCtx, cancel := context.WithTimeout(ctx, 60*time.Minute)
	defer cancel()
	return job, c.waitForJob(waitCtx, job.ID)
}
