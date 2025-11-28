package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"example.com/turtlebot-fleet/internal/agent"
	sshc "example.com/turtlebot-fleet/internal/ssh"
)

type semesterRequest struct {
	RobotIDs   []int64              `json:"robot_ids"`
	Reinstall  bool                 `json:"reinstall"`
	ResetLogs  bool                 `json:"reset_logs"`
	UpdateRepo bool                 `json:"update_repo"`
	RepoConfig agent.UpdateRepoData `json:"repo_config"`
}

type SemesterBatchStatus struct {
	sync.RWMutex
	Active    bool             `json:"active"`
	Total     int              `json:"total"`
	Completed int              `json:"completed"`
	Robots    map[int64]string `json:"robots"`
	Errors    map[int64]string `json:"errors"`
}

var batchStatus = &SemesterBatchStatus{
	Robots: make(map[int64]string),
	Errors: make(map[int64]string),
}

func (c *Controller) GetSemesterStatus(w http.ResponseWriter, r *http.Request) {
	batchStatus.RLock()
	defer batchStatus.RUnlock()
	// Create a copy to avoid race conditions during JSON marshaling if we passed the struct directly with the mutex
	status := struct {
		Active    bool             `json:"active"`
		Total     int              `json:"total"`
		Completed int              `json:"completed"`
		Robots    map[int64]string `json:"robots"`
		Errors    map[int64]string `json:"errors"`
	}{
		Active:    batchStatus.Active,
		Total:     batchStatus.Total,
		Completed: batchStatus.Completed,
		Robots:    make(map[int64]string),
		Errors:    make(map[int64]string),
	}
	for k, v := range batchStatus.Robots {
		status.Robots[k] = v
	}
	for k, v := range batchStatus.Errors {
		status.Errors[k] = v
	}
	respondJSON(w, http.StatusOK, status)
}

func (c *Controller) HandleSemesterStart(w http.ResponseWriter, r *http.Request) {
	var req semesterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	batchStatus.Lock()
	if batchStatus.Active {
		batchStatus.Unlock()
		respondError(w, http.StatusConflict, "batch already in progress")
		return
	}
	batchStatus.Active = true
	batchStatus.Total = len(req.RobotIDs)
	batchStatus.Completed = 0
	batchStatus.Robots = make(map[int64]string)
	batchStatus.Errors = make(map[int64]string)
	for _, id := range req.RobotIDs {
		batchStatus.Robots[id] = "pending"
	}
	batchStatus.Unlock()

	go c.processSemesterBatch(req)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

func (c *Controller) processSemesterBatch(req semesterRequest) {
	defer func() {
		batchStatus.Lock()
		batchStatus.Active = false
		batchStatus.Unlock()
	}()

	ctx := context.Background()
	log.Printf("starting semester batch for %d robots", len(req.RobotIDs))

	binaryPath := os.Getenv("AGENT_BINARY_PATH")
	if binaryPath == "" {
		binaryPath = "/app/agent"
	}
	var binary []byte
	if req.Reinstall {
		var err error
		binary, err = os.ReadFile(binaryPath)
		if err != nil {
			log.Printf("semester: failed to read agent binary: %v", err)
			return
		}
	}

	workspace := os.Getenv("AGENT_WORKSPACE_PATH")
	if workspace == "" {
		workspace = "/home/ubuntu/ros_ws/src/course"
	}
	broker := agentBrokerURL()

	var wg sync.WaitGroup
	for _, id := range req.RobotIDs {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()

			batchStatus.Lock()
			batchStatus.Robots[id] = "processing"
			batchStatus.Unlock()

			robot, err := c.DB.GetRobotByID(ctx, id)
			if err != nil {
				log.Printf("semester: failed to get robot %d: %v", id, err)
				batchStatus.Lock()
				batchStatus.Errors[id] = "robot not found"
				batchStatus.Robots[id] = "error"
				batchStatus.Completed++
				batchStatus.Unlock()
				return
			}

			if req.Reinstall {
				if robot.InstallConfig == nil {
					log.Printf("semester: robot %d missing install config", id)
					batchStatus.Lock()
					batchStatus.Errors[id] = "missing install config"
					batchStatus.Robots[id] = "error"
					batchStatus.Completed++
					batchStatus.Unlock()
					return
				}

				log.Printf("semester: reinstalling agent on %s", robot.Name)
				batchStatus.Lock()
				batchStatus.Robots[id] = "installing_agent"
				batchStatus.Unlock()

				addr := robot.InstallConfig.Address
				if !strings.Contains(addr, ":") {
					addr = net.JoinHostPort(addr, "22")
				}

				// Default sudo logic from install_agent.go
				useSudo := strings.ToLower(robot.InstallConfig.User) != "root"
				sudoPwd := os.Getenv("AGENT_SUDO_PASSWORD")
				if useSudo && sudoPwd == "" {
					sudoPwd = "ubuntu"
				}

				cfg := agent.Config{
					AgentID:        robot.Name, // Use name as AgentID for consistency
					MQTTBroker:     broker,
					WorkspacePath:  workspace,
					WorkspaceOwner: determineWorkspaceOwner(installAgentRequest{User: robot.InstallConfig.User}),
				}

				host := sshc.HostSpec{
					Addr:         addr,
					User:         robot.InstallConfig.User,
					PrivateKey:   []byte(robot.InstallConfig.SSHKey),
					UseSudo:      useSudo,
					SudoPassword: sudoPwd,
				}

				installStart := time.Now()
				if err := sshc.InstallAgent(host, cfg, binary); err != nil {
					log.Printf("semester: failed to install agent on %s: %v", robot.Name, err)
					batchStatus.Lock()
					batchStatus.Errors[id] = fmt.Sprintf("install failed: %v", err)
					batchStatus.Robots[id] = "error"
					batchStatus.Completed++
					batchStatus.Unlock()
					return
				}

				// Wait for reconnect
				if req.ResetLogs || req.UpdateRepo {
					log.Printf("semester: waiting for %s to reconnect...", robot.Name)
					batchStatus.Lock()
					batchStatus.Robots[id] = "waiting_for_connection"
					batchStatus.Unlock()

					connected := false
					for i := 0; i < 60; i++ {
						time.Sleep(1 * time.Second)
						updated, err := c.DB.GetRobotByID(ctx, id)
						if err == nil && updated.LastSeen.After(installStart) {
							connected = true
							break
						}
					}
					if !connected {
						log.Printf("semester: timeout waiting for %s to reconnect", robot.Name)
						batchStatus.Lock()
						batchStatus.Errors[id] = "reconnect timeout"
						batchStatus.Robots[id] = "error"
						batchStatus.Completed++
						batchStatus.Unlock()
						return
					}
				}
			}

			if req.ResetLogs {
				log.Printf("semester: resetting logs for %s", robot.Name)
				batchStatus.Lock()
				batchStatus.Robots[id] = "resetting_logs"
				batchStatus.Unlock()

				cmd := agent.Command{Type: "reset_logs", Data: []byte("{}")}
				if _, err := c.queueRobotCommand(ctx, robot, cmd); err != nil {
					log.Printf("semester: failed to queue reset_logs for %s: %v", robot.Name, err)
					batchStatus.Lock()
					batchStatus.Errors[id] = "failed to queue reset_logs"
					batchStatus.Robots[id] = "error"
					batchStatus.Completed++
					batchStatus.Unlock()
					return
				}
			}

			if req.UpdateRepo {
				log.Printf("semester: updating repo for %s", robot.Name)
				batchStatus.Lock()
				batchStatus.Robots[id] = "updating_repo"
				batchStatus.Unlock()

				data, _ := json.Marshal(req.RepoConfig)
				cmd := agent.Command{Type: "update_repo", Data: data}
				if _, err := c.queueRobotCommand(ctx, robot, cmd); err != nil {
					log.Printf("semester: failed to queue update_repo for %s: %v", robot.Name, err)
					batchStatus.Lock()
					batchStatus.Errors[id] = "failed to queue update_repo"
					batchStatus.Robots[id] = "error"
					batchStatus.Completed++
					batchStatus.Unlock()
					return
				}
			}

			batchStatus.Lock()
			batchStatus.Robots[id] = "success"
			batchStatus.Completed++
			batchStatus.Unlock()
		}(id)
	}
	wg.Wait()
	log.Printf("semester batch complete")
}
