package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"example.com/openrobot-fleet/internal/agent"
	"example.com/openrobot-fleet/internal/db"
	"example.com/openrobot-fleet/internal/scenario"
	sshc "example.com/openrobot-fleet/internal/ssh"
)

type semesterRequest struct {
	RobotIDs       []int64              `json:"robot_ids"`
	Reinstall      bool                 `json:"reinstall"`
	ResetLogs      bool                 `json:"reset_logs"`
	UpdateRepo     bool                 `json:"update_repo"`
	RunSelfTest    bool                 `json:"run_self_test"`
	RepoConfig     agent.UpdateRepoData `json:"repo_config"`
	ApplyScenarios bool                 `json:"apply_scenarios"`
	ScenarioIDs    []int64              `json:"scenario_ids"`

	// Internal
	ScenarioConfigs []agent.UpdateRepoData `json:"-"`
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

	if req.ApplyScenarios {
		for _, sid := range req.ScenarioIDs {
			s, err := c.DB.GetScenarioByID(r.Context(), sid)
			if err != nil {
				respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid scenario id: %d", sid))
				return
			}
			spec, err := scenario.Parse(s.ConfigYAML)
			if err != nil {
				respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid scenario config for %s: %v", s.Name, err))
				return
			}
			req.ScenarioConfigs = append(req.ScenarioConfigs, spec.Repo.ToUpdateRepo())
		}
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

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

	go c.processSemesterBatch(req, baseURL)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

func (c *Controller) processSemesterBatch(req semesterRequest, baseURL string) {
	defer func() {
		batchStatus.Lock()
		batchStatus.Active = false
		batchStatus.Unlock()
	}()

	ctx := context.Background()
	log.Printf("starting semester batch for %d robots", len(req.RobotIDs))

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
				if robot.InstallConfig == nil || robot.InstallConfig.Address == "" {
					// Try to use default install config if robot-specific one is missing
					defaultCfg, err := c.DB.GetDefaultInstallConfig(ctx)
					if err == nil && defaultCfg != nil {
						if robot.InstallConfig == nil {
							robot.InstallConfig = &db.InstallConfig{}
						}
						if robot.InstallConfig.User == "" {
							robot.InstallConfig.User = defaultCfg.User
						}
						if robot.InstallConfig.SSHKey == "" {
							robot.InstallConfig.SSHKey = defaultCfg.SSHKey
						}
						if robot.InstallConfig.Password == "" {
							robot.InstallConfig.Password = defaultCfg.Password
						}
					}
					// If address is still missing, try to use the robot's IP
					if (robot.InstallConfig == nil || robot.InstallConfig.Address == "") && robot.IP != "" {
						if robot.InstallConfig == nil {
							robot.InstallConfig = &db.InstallConfig{}
						}
						robot.InstallConfig.Address = robot.IP
					}
				}

				if robot.InstallConfig == nil || robot.InstallConfig.Address == "" || robot.InstallConfig.User == "" || (robot.InstallConfig.SSHKey == "" && robot.InstallConfig.Password == "") {
					// If we are in demo mode, we can fake success for reinstall
					if os.Getenv("DEMO_MODE") == "true" {
						log.Printf("semester: demo mode, skipping reinstall for %s", robot.Name)
						// Fall through to other steps
					} else {
						log.Printf("semester: robot %d missing install config (addr=%v, user=%v, key_len=%d, has_pass=%v)", id,
							robot.InstallConfig != nil && robot.InstallConfig.Address != "",
							robot.InstallConfig != nil && robot.InstallConfig.User != "",
							func() int {
								if robot.InstallConfig != nil {
									return len(robot.InstallConfig.SSHKey)
								}
								return 0
							}(),
							robot.InstallConfig != nil && robot.InstallConfig.Password != "")
						batchStatus.Lock()
						batchStatus.Errors[id] = "missing install config"
						batchStatus.Robots[id] = "error"
						batchStatus.Completed++
						batchStatus.Unlock()
						return
					}
				} else {
					log.Printf("semester: reinstalling agent on %s", robot.Name)
					batchStatus.Lock()
					batchStatus.Robots[id] = "installing_agent"
					batchStatus.Unlock()

					addr := robot.InstallConfig.Address
					if robot.IP != "" {
						addr = robot.IP
					}
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
						Password:     robot.InstallConfig.Password,
						UseSudo:      useSudo,
						SudoPassword: sudoPwd,
					}

					arch, err := sshc.DetectArch(host)
					if err != nil {
						log.Printf("semester: failed to detect arch for %s: %v", robot.Name, err)
						batchStatus.Lock()
						batchStatus.Errors[id] = "failed to detect arch: " + err.Error()
						batchStatus.Robots[id] = "error"
						batchStatus.Completed++
						batchStatus.Unlock()
						return
					}

					binaryDir := os.Getenv("AGENT_BINARY_DIR")
					if binaryDir == "" {
						binaryDir = "/app"
					}
					binaryName := "agent-amd64"
					if arch == "arm64" {
						binaryName = "agent-arm64"
					}
					binaryPath := filepath.Join(binaryDir, binaryName)
					binary, err := os.ReadFile(binaryPath)
					if err != nil {
						log.Printf("semester: failed to read agent binary: %v", err)
						batchStatus.Lock()
						batchStatus.Errors[id] = "agent binary unavailable"
						batchStatus.Robots[id] = "error"
						batchStatus.Completed++
						batchStatus.Unlock()
						return
					}

					installStart := time.Now()
					if err := sshc.InstallAgent(host, cfg, binary); err != nil {
						log.Printf("semester: failed to install agent on %s: %v", robot.Name, err)
						batchStatus.Lock()
						msg := fmt.Sprintf("install failed: %v", err)
						if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no route to host") || strings.Contains(err.Error(), "i/o timeout") {
							msg = "Connection failed. Check connection or restart robot."
						}
						batchStatus.Errors[id] = msg
						batchStatus.Robots[id] = "error"
						batchStatus.Completed++
						batchStatus.Unlock()
						return
					}

					// Wait for reconnect
					if req.ResetLogs || req.UpdateRepo || req.ApplyScenarios {
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

			if req.ApplyScenarios {
				log.Printf("semester: applying scenarios for %s", robot.Name)
				batchStatus.Lock()
				batchStatus.Robots[id] = "applying_scenarios"
				batchStatus.Unlock()

				var commands []agent.Command
				for _, config := range req.ScenarioConfigs {
					data, _ := json.Marshal(config)
					commands = append(commands, agent.Command{Type: "update_repo", Data: data})
				}

				batchData := agent.BatchData{Commands: commands}
				batchPayload, _ := json.Marshal(batchData)
				cmd := agent.Command{Type: "batch", Data: batchPayload}

				if _, err := c.queueRobotCommand(ctx, robot, cmd); err != nil {
					log.Printf("semester: failed to queue batch scenarios for %s: %v", robot.Name, err)
					batchStatus.Lock()
					batchStatus.Errors[id] = "failed to queue batch scenarios"
					batchStatus.Robots[id] = "error"
					batchStatus.Completed++
					batchStatus.Unlock()
					return
				}

				// Update DB to reflect the last scenario applied
				if len(req.ScenarioIDs) > 0 {
					lastID := req.ScenarioIDs[len(req.ScenarioIDs)-1]
					if err := c.DB.UpdateRobotScenario(ctx, id, lastID); err != nil {
						log.Printf("semester: failed to update robot scenario for %s: %v", robot.Name, err)
					}
				}
			}

			if req.RunSelfTest {
				log.Printf("semester: running self test for %s", robot.Name)
				batchStatus.Lock()
				batchStatus.Robots[id] = "running_self_test"
				batchStatus.Unlock()

				// Test Drive
				driveData, _ := json.Marshal(agent.TestDriveData{DurationSec: 2})
				cmdDrive := agent.Command{Type: "test_drive", Data: driveData}
				if _, err := c.queueRobotCommand(ctx, robot, cmdDrive); err != nil {
					log.Printf("semester: failed to queue test_drive for %s: %v", robot.Name, err)
					batchStatus.Lock()
					batchStatus.Errors[id] = "failed to queue test_drive"
					batchStatus.Robots[id] = "error"
					batchStatus.Completed++
					batchStatus.Unlock()
					return
				}

				// Capture Image
				uploadURL := fmt.Sprintf("%s/api/robots/%d/upload", baseURL, id)
				captureData, _ := json.Marshal(agent.CaptureImageData{UploadURL: uploadURL})
				cmdCapture := agent.Command{Type: "capture_image", Data: captureData}
				if _, err := c.queueRobotCommand(ctx, robot, cmdCapture); err != nil {
					log.Printf("semester: failed to queue capture_image for %s: %v", robot.Name, err)
					batchStatus.Lock()
					batchStatus.Errors[id] = "failed to queue capture_image"
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
