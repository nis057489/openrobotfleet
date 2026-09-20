package controller

import (
	"context"
	"encoding/json"
	"example.com/openrobot-fleet/internal/agent"
	"example.com/openrobot-fleet/internal/db"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testController(t *testing.T) *Controller {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "controller.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.SQL.Close() })
	return New(database, nil)
}
func TestResultsPersistAndRequireCorrectRobot(t *testing.T) {
	c := testController(t)
	ctx := context.Background()
	job, err := c.queueRobotCommand(ctx, db.Robot{AgentID: "robot-a"}, agent.Command{Type: "update_repo", Data: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if queued, err := c.DB.QueuedJobs(ctx, "robot-a"); err != nil || len(queued) != 1 {
		t.Fatal("offline job was not stored")
	}
	id := strconv.FormatInt(job.ID, 10)
	result := agent.Job{ID: id, Status: agent.JobStatusFailed, Error: "clone failed"}
	c.ProcessJobResults("robot-b", []agent.Job{result})
	stored, _ := c.DB.GetJob(ctx, job.ID)
	if stored.Status != "queued" {
		t.Fatal("another robot changed the job")
	}
	c.ProcessJobResults("robot-a", []agent.Job{result})
	c.ProcessJobResults("robot-a", []agent.Job{{ID: id, Status: agent.JobStatusRunning}})
	stored, err = c.DB.GetJob(ctx, job.ID)
	if err != nil || stored.Status != "failed" || stored.Error != "clone failed" {
		t.Fatalf("lost terminal result: %+v %v", stored, err)
	}
	if err := c.waitForJob(ctx, job.ID); err == nil || !strings.Contains(err.Error(), "clone failed") {
		t.Fatalf("failure not propagated: %v", err)
	}
}
func TestWaitForJobRequiresExecutionConfirmation(t *testing.T) {
	c := testController(t)
	ctx := context.Background()
	job, err := c.queueRobotCommand(ctx, db.Robot{AgentID: "robot-a"}, agent.Command{Type: "identify", Data: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	timeout, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	if err := c.waitForJob(timeout, job.ID); err == nil {
		t.Fatal("queued job reported success")
	}
	c.ProcessJobResults("robot-a", []agent.Job{{ID: strconv.FormatInt(job.ID, 10), Status: agent.JobStatusSuccess}})
	if err := c.waitForJob(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
}
func TestScenarioRecordedOnlyAfterSuccess(t *testing.T) {
	c := testController(t)
	ctx := context.Background()
	if err := c.DB.UpsertRobotStatus(ctx, "robot-a", "robot-a", "127.0.0.1", "ok", "robot"); err != nil {
		t.Fatal(err)
	}
	robot, err := c.DB.GetRobotByAgentID(ctx, "robot-a")
	if err != nil {
		t.Fatal(err)
	}
	sid, err := c.DB.CreateScenario(ctx, db.Scenario{Name: "new", ConfigYAML: "repo:\n  url: unused"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := c.queueRobotCommand(ctx, robot, agent.Command{Type: "update_repo", ScenarioID: sid, Data: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := c.DB.GetRobotByID(ctx, robot.ID)
	if before.LastScenario != nil {
		t.Fatal("scenario applied before execution")
	}
	c.ProcessJobResults("robot-a", []agent.Job{{ID: strconv.FormatInt(job.ID, 10), Status: agent.JobStatusSuccess}})
	after, _ := c.DB.GetRobotByID(ctx, robot.ID)
	if after.LastScenario == nil || after.LastScenario.ID != sid {
		t.Fatal("confirmed scenario not recorded")
	}
}

func TestSemesterStopsAtExecutionFailure(t *testing.T) {
	c := testController(t)
	ctx := context.Background()
	c.DB.UpsertRobotStatus(ctx, "semester-robot", "semester-robot", "127.0.0.1", "ok", "robot")
	robot, err := c.DB.GetRobotByAgentID(ctx, "semester-robot")
	if err != nil {
		t.Fatal(err)
	}
	req := semesterRequest{RobotIDs: []int64{robot.ID}, ResetLogs: true, UpdateRepo: true}
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	run := batches.add(batchLabel(req), req.RobotIDs, cancelRun)
	done := make(chan struct{})
	go func() {
		c.processSemesterBatch(runCtx, req, "http://controller", run)
		close(done)
	}()
	var jobs []db.Job
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		jobs, _ = c.DB.ListJobs(ctx, robot.AgentID)
		if len(jobs) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(jobs) != 1 {
		t.Fatal("first command was not queued")
	}
	select {
	case <-done:
		t.Fatal("semester finished without confirmation")
	default:
	}
	c.ProcessJobResults(robot.AgentID, []agent.Job{{ID: strconv.FormatInt(jobs[0].ID, 10), Status: agent.JobStatusFailed, Error: "permission denied"}})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("semester ignored failure")
	}
	jobs, _ = c.DB.ListJobs(ctx, robot.AgentID)
	if len(jobs) != 1 {
		t.Fatal("later step ran despite failure")
	}
	snap := batches.snapshot()
	var got *BatchRun
	for i := range snap {
		if snap[i].ID == run.ID {
			got = &snap[i]
			break
		}
	}
	if got == nil {
		t.Fatal("batch run missing from registry")
	}
	if got.Robots[robot.ID] != "error" || !strings.Contains(got.Errors[robot.ID], "permission denied") {
		t.Fatalf("failure hidden: %+v", got.Errors)
	}
}
