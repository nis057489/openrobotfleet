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

func TestOnlyNewestDurableCommandDelivered(t *testing.T) {
	c := testController(t)
	ctx := context.Background()
	robot := db.Robot{AgentID: "robot-a"}
	var ids []int64
	for _, cmd := range []agent.Command{
		{Type: "configure_network", Data: json.RawMessage(`{"ros_domain_id":1}`)},
		{Type: "set_hostname", Data: json.RawMessage(`{"hostname":"a"}`)},
		{Type: "configure_network", Data: json.RawMessage(`{"ros_domain_id":2}`)},
	} {
		job, err := c.queueRobotCommand(ctx, robot, cmd)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, job.ID)
	}
	c.DeliverQueuedJobs("robot-a")
	want := []string{"failed", "queued", "queued"}
	for i, id := range ids {
		job, err := c.DB.GetJob(ctx, id)
		if err != nil || job.Status != want[i] {
			t.Fatalf("job %d: got %q (%v), want %q", i, job.Status, err, want[i])
		}
	}
}

func TestCompletedDurableCommandSupersedesOlderOutboxEntry(t *testing.T) {
	c := testController(t)
	ctx := context.Background()
	robot := db.Robot{AgentID: "robot-a"}
	old, err := c.queueRobotCommand(ctx, robot, agent.Command{Type: "configure_network", Data: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := c.queueRobotCommand(ctx, robot, agent.Command{Type: "configure_network", Data: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	c.ProcessJobResults(robot.AgentID, []agent.Job{{ID: strconv.FormatInt(latest.ID, 10), Status: agent.JobStatusSuccess}})
	c.DeliverQueuedJobs(robot.AgentID)
	stored, err := c.DB.GetJob(ctx, old.ID)
	if err != nil || stored.Status != "failed" || !strings.Contains(stored.Error, "superseded") {
		t.Fatalf("old state remains deliverable: %+v %v", stored, err)
	}
}

func TestOfflineDeliveryStillExpiresJobsBehindFirstPublish(t *testing.T) {
	c := testController(t)
	ctx := context.Background()
	robot := db.Robot{AgentID: "robot-a"}
	if _, err := c.queueRobotCommand(ctx, robot, agent.Command{Type: "identify"}); err != nil {
		t.Fatal(err)
	}
	expired, err := c.queueRobotCommand(ctx, robot, agent.Command{Type: "test_drive"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.DB.SQL.Exec(`UPDATE jobs SET created_at=? WHERE id=?`, time.Now().UTC().Add(-time.Hour), expired.ID); err != nil {
		t.Fatal(err)
	}
	c.DeliverQueuedJobs(robot.AgentID)
	stored, err := c.DB.GetJob(ctx, expired.ID)
	if err != nil || stored.Status != "failed" || !strings.Contains(stored.Error, "expired") {
		t.Fatalf("expired movement remains queued: %+v %v", stored, err)
	}
}

func TestBatchDeviceReservationWaitsAndHonoursCancellation(t *testing.T) {
	c := testController(t)
	release, err := c.acquireBatchDevice(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	other, err := c.acquireBatchDevice(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	other()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if unlock, err := c.acquireBatchDevice(ctx, 1); err == nil {
		unlock()
		t.Fatal("overlapping workflow acquired busy device")
	}
	release()
	unlock, err := c.acquireBatchDevice(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if unlock, err := c.acquireBatchDevice(ctx, 1); err == nil {
		unlock()
		t.Fatal("cancelled workflow acquired free device")
	}
}

func TestOverlappingSemesterBatchesDoNotInterleave(t *testing.T) {
	c := testController(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.DB.UpsertRobotStatus(ctx, "batch-robot", "batch-robot", "127.0.0.1", "ok", "robot"); err != nil {
		t.Fatal(err)
	}
	robot, err := c.DB.GetRobotByAgentID(ctx, "batch-robot")
	if err != nil {
		t.Fatal(err)
	}
	req := semesterRequest{RobotIDs: []int64{robot.ID}, ResetLogs: true, ResetBashrc: true}
	first := batches.add("first", req.RobotIDs, cancel)
	firstDone := make(chan struct{})
	go func() { c.processSemesterBatch(ctx, req, "http://controller", first); close(firstDone) }()
	waitJobs := func(count int) []db.Job {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			jobs, err := c.DB.ListJobs(ctx, robot.AgentID)
			if err != nil {
				t.Fatal(err)
			}
			if len(jobs) >= count {
				return jobs
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("wanted %d jobs", count)
		return nil
	}
	firstJobs := waitJobs(1)
	secondCtx, secondCancel := context.WithCancel(ctx)
	defer secondCancel()
	second := batches.add("second", req.RobotIDs, secondCancel)
	secondDone := make(chan struct{})
	go func() { c.processSemesterBatch(secondCtx, req, "http://controller", second); close(secondDone) }()
	// Confirm the second worker reached the reservation before advancing first.
	deadline := time.Now().Add(time.Second)
	waiting := false
	for time.Now().Before(deadline) && !waiting {
		for _, run := range batches.snapshot() {
			if run.ID == second.ID && run.Robots[robot.ID] == "waiting_for_device" {
				waiting = true
			}
		}
		time.Sleep(time.Millisecond)
	}
	if !waiting {
		t.Fatal("second workflow did not wait for the first")
	}
	c.ProcessJobResults(robot.AgentID, []agent.Job{{ID: strconv.FormatInt(firstJobs[0].ID, 10), Status: agent.JobStatusSuccess}})
	jobs := waitJobs(2)
	if len(jobs) != 2 {
		t.Fatalf("workflows interleaved: %+v", jobs)
	}
	var next db.Job
	for _, job := range jobs {
		if job.ID != firstJobs[0].ID {
			next = job
		}
	}
	if next.Type != "reset_bashrc" {
		t.Fatalf("second workflow overtook first: %+v", next)
	}
	// Cancelling a workflow waiting for the device must enqueue no commands.
	batches.cancelRun(second.ID)
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("waiting workflow did not cancel")
	}
	c.ProcessJobResults(robot.AgentID, []agent.Job{{ID: strconv.FormatInt(next.ID, 10), Status: agent.JobStatusSuccess}})
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first workflow did not finish")
	}
	jobs, err = c.DB.ListJobs(ctx, robot.AgentID)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("cancelled workflow queued work: %+v %v", jobs, err)
	}
}
