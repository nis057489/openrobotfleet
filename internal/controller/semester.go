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
	RobotIDs             []int64              `json:"robot_ids"`
	Reinstall            bool                 `json:"reinstall"`
	ResetLogs            bool                 `json:"reset_logs"`
	UpdateRepo           bool                 `json:"update_repo"`
	RunSelfTest          bool                 `json:"run_self_test"`
	InstallCameraSupport bool                 `json:"install_camera_support"`
	ResetBashrc          bool                 `json:"reset_bashrc"`
	TurtleBot3Model      string               `json:"turtlebot3_model"`
	SystemUpgrade        bool                 `json:"system_upgrade"`
	InstallPackages      bool                 `json:"install_packages"`
	Packages             []string             `json:"packages"`
	RepoConfig           agent.UpdateRepoData `json:"repo_config"`
	ApplyScenarios       bool                 `json:"apply_scenarios"`
	ScenarioIDs          []int64              `json:"scenario_ids"`
	FactoryReset         bool                 `json:"factory_reset"`

	// Internal
	ScenarioConfigs []agent.UpdateRepoData `json:"-"`
}

// GetSemesterStatus reports progress aggregated across every active run, and
// also returns the individual runs so callers can show them separately.
func (c *Controller) GetSemesterStatus(w http.ResponseWriter, r *http.Request) {
	runs := batches.snapshot()

	active := false
	total, completed := 0, 0
	robots := make(map[int64]string)
	errs := make(map[int64]string)
	for _, run := range runs {
		if !run.Active {
			continue
		}
		active = true
		total += run.Total
		completed += run.Completed
		for k, v := range run.Robots {
			robots[k] = v
		}
		for k, v := range run.Errors {
			errs[k] = v
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"active":    active,
		"total":     total,
		"completed": completed,
		"robots":    robots,
		"errors":    errs,
		"batches":   runs,
	})
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

	if req.InstallPackages {
		var pkgs []string
		for _, p := range req.Packages {
			if p = strings.TrimSpace(p); p != "" {
				pkgs = append(pkgs, p)
			}
		}
		if len(pkgs) == 0 {
			respondError(w, http.StatusBadRequest, "no packages to install")
			return
		}
		if err := agent.ValidatePackageNames(pkgs); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		req.Packages = pkgs
	} else {
		req.Packages = nil
	}

	if len(req.RobotIDs) == 0 {
		respondError(w, http.StatusBadRequest, "no devices selected")
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

	// Batches run concurrently. Overlapping devices are safe because the agent
	// runs one job at a time and queues the rest, so a second batch's commands
	// wait their turn on the device rather than interleaving with the first.
	ctx, cancel := context.WithCancel(context.Background())
	run := batches.add(batchLabel(req), req.RobotIDs, cancel)
	log.Printf("semester: starting batch %s (%s) for %d devices", run.ID, run.Label, len(req.RobotIDs))

	go func() {
		defer cancel()
		c.processSemesterBatch(ctx, req, baseURL, run)
	}()

	respondJSON(w, http.StatusAccepted, map[string]string{
		"status":   "accepted",
		"batch_id": run.ID,
		"label":    run.Label,
	})
}

func (c *Controller) processSemesterBatch(ctx context.Context, req semesterRequest, baseURL string, run *BatchRun) {
	defer batches.finish(run)

	log.Printf("starting semester batch for %d robots", len(req.RobotIDs))

	workspace := os.Getenv("AGENT_WORKSPACE_PATH")
	if workspace == "" {
		workspace = "/home/ubuntu/ros_ws/src/course"
	}
	broker := c.agentBrokerURL(ctx)

	var wg sync.WaitGroup
	for _, id := range req.RobotIDs {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()

			batches.setRobotState(run, id, "processing")

			robot, err := c.DB.GetRobotByID(ctx, id)
			if err != nil {
				log.Printf("semester: failed to get robot %d: %v", id, err)
				batches.failRobot(run, id, "robot not found")
				return
			}

			if req.Reinstall {
				if robot.InstallConfig == nil || robot.InstallConfig.Address == "" {
					// Try to use default install config if robot-specific one is missing
					defaultCfg, err := c.DB.GetDefaultInstallConfigFor(ctx, robot.Type)
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
					}
					// If address is still missing, try to use the robot's IP
					if (robot.InstallConfig == nil || robot.InstallConfig.Address == "") && robot.IP != "" {
						if robot.InstallConfig == nil {
							robot.InstallConfig = &db.InstallConfig{}
						}
						robot.InstallConfig.Address = robot.IP
					}
				}

				if robot.InstallConfig == nil || robot.InstallConfig.Address == "" || robot.InstallConfig.User == "" || robot.InstallConfig.SSHKey == "" {
					// If we are in demo mode, we can fake success for reinstall
					if os.Getenv("DEMO_MODE") == "true" {
						log.Printf("semester: demo mode, skipping reinstall for %s", robot.Name)
						// Fall through to other steps
					} else {
						log.Printf("semester: robot %d missing install config (addr=%v, user=%v, key_len=%d)", id,
							robot.InstallConfig != nil && robot.InstallConfig.Address != "",
							robot.InstallConfig != nil && robot.InstallConfig.User != "",
							func() int {
								if robot.InstallConfig != nil {
									return len(robot.InstallConfig.SSHKey)
								}
								return 0
							}())
						batches.failRobot(run, id, "missing install config")
						return
					}
				} else {
					log.Printf("semester: reinstalling agent on %s", robot.Name)
					batches.setRobotState(run, id, "installing_agent")

					addr := robot.InstallConfig.Address
					if robot.IP != "" {
						addr = robot.IP
					}
					if !strings.Contains(addr, ":") {
						addr = net.JoinHostPort(addr, "22")
					}

					// Sudo password, most specific source first. The saved
					// per-device password wins, then the defaults for this
					// device type (laptops keep their own account and password),
					// then the environment. "ubuntu" is only our own fleet
					// image's convention, so a guess is tracked and reported
					// rather than surfacing an opaque "exit status 1".
					useSudo := strings.ToLower(robot.InstallConfig.User) != "root"
					sudoPwd := robot.InstallConfig.SudoPassword
					if sudoPwd == "" {
						if typeCfg, err := c.DB.GetDefaultInstallConfigFor(ctx, robot.Type); err == nil && typeCfg != nil {
							sudoPwd = typeCfg.SudoPassword
							if sudoPwd == "" {
								sudoPwd = typeCfg.Password
							}
						}
					}
					if sudoPwd == "" {
						sudoPwd = os.Getenv("AGENT_SUDO_PASSWORD")
					}
					sudoPwdGuessed := false
					if useSudo && sudoPwd == "" {
						sudoPwd = "ubuntu"
						sudoPwdGuessed = true
					}

					host := sshc.HostSpec{
						Addr:         addr,
						User:         robot.InstallConfig.User,
						PrivateKey:   []byte(robot.InstallConfig.SSHKey),
						UseSudo:      useSudo,
						SudoPassword: sudoPwd,
					}

					arch, err := sshc.DetectArch(host)
					if err != nil {
						log.Printf("semester: failed to detect arch for %s: %v", robot.Name, err)
						batches.failRobot(run, id, "failed to detect arch: "+err.Error())
						return
					}

					agentID, err := sshc.DetectPrimaryMAC(host)
					if err != nil {
						log.Printf("semester: failed to detect device identity for %s: %v", robot.Name, err)
						batches.failRobot(run, id, "failed to detect device identity: "+err.Error())
						return
					}

					cfg := agent.Config{
						AgentID:        agentID,
						Type:           robot.Type,
						MQTTBroker:     broker,
						MQTTUsername:   os.Getenv("AGENT_MQTT_USERNAME"),
						MQTTPassword:   os.Getenv("AGENT_MQTT_PASSWORD"),
						WorkspacePath:  workspace,
						WorkspaceOwner: determineWorkspaceOwner(installAgentRequest{User: robot.InstallConfig.User}),
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
						batches.failRobot(run, id, "agent binary unavailable")
						return
					}

					installStart := time.Now()
					if err := sshc.InstallAgent(host, cfg, robot.Name, binary); err != nil {
						log.Printf("semester: failed to install agent on %s: %v", robot.Name, err)
						msg := fmt.Sprintf("install failed: %v", err)
						switch {
						case strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no route to host") || strings.Contains(err.Error(), "i/o timeout"):
							msg = "Connection failed. Check connection or restart robot."
						case useSudo && sudoPwdGuessed && strings.Contains(err.Error(), "exited with status 1"):
							msg = "Privileged command failed and no sudo password is saved for this device, so \"ubuntu\" was tried. Set the sudo password in Settings under this device type's install defaults."
						}
						batches.failRobot(run, id, msg)
						return
					}

					// Pin the newly-detected agent_id onto this same row now,
					// rather than waiting for the agent's first heartbeat to
					// do it -- UpsertRobotStatus (heartbeat writes) is keyed
					// by agent_id, so until this row's agent_id column is
					// updated to match what the reinstalled agent will
					// report, a heartbeat can't find it and would instead
					// create a duplicate row.
					robotIP := robot.InstallConfig.Address
					if host, _, err := net.SplitHostPort(addr); err == nil {
						robotIP = host
					}
					// InstallAgent takes no context, so a cancelled batch cannot
					// interrupt it -- the agent is already on the device by now.
					// This pin must therefore still run, or the stale agent_id
					// leaves the duplicate row described above.
					if err := c.DB.UpsertRobotWithType(context.WithoutCancel(ctx), agentID, robot.Name, robotIP, "installed", robot.Type); err != nil {
						log.Printf("semester: failed to pin agent_id for %s: %v", robot.Name, err)
						batches.failRobot(run, id, "failed to update robot: "+err.Error())
						return
					}
					robot.AgentID = agentID
					c.reapplyGroupForRobot(ctx, id)

					// Wait for reconnect
					if req.ResetLogs || req.UpdateRepo || req.ApplyScenarios {
						log.Printf("semester: waiting for %s to reconnect...", robot.Name)
						batches.setRobotState(run, id, "waiting_for_connection")

						connected := false
						for i := 0; i < 60; i++ {
							select {
							case <-ctx.Done():
								batches.failRobot(run, id, "cancelled while waiting for reconnect")
								return
							case <-time.After(1 * time.Second):
							}
							updated, err := c.DB.GetRobotByID(ctx, id)
							if err == nil && updated.LastSeen.After(installStart) {
								connected = true
								break
							}
						}
						if !connected {
							log.Printf("semester: timeout waiting for %s to reconnect", robot.Name)
							batches.failRobot(run, id, "reconnect timeout")
							return
						}
					}
				}
			}

			if req.ResetLogs {
				log.Printf("semester: resetting logs for %s", robot.Name)
				batches.setRobotState(run, id, "resetting_logs")

				cmd := agent.Command{Type: "reset_logs", Data: []byte("{}")}
				if _, err := c.runRobotCommand(ctx, robot, cmd); err != nil {
					log.Printf("semester: command failed: reset_logs for %s: %v", robot.Name, err)
					batches.failRobot(run, id, err.Error())
					return
				}
			}

			if req.ResetBashrc {
				log.Printf("semester: resetting .bashrc for %s", robot.Name)
				batches.setRobotState(run, id, "resetting_bashrc")

				data, _ := json.Marshal(agent.ResetBashrcData{TurtleBot3Model: req.TurtleBot3Model})
				cmd := agent.Command{Type: "reset_bashrc", Data: data}
				if _, err := c.runRobotCommand(ctx, robot, cmd); err != nil {
					log.Printf("semester: command failed: reset_bashrc for %s: %v", robot.Name, err)
					batches.failRobot(run, id, err.Error())
					return
				}
			}

			if req.UpdateRepo {
				log.Printf("semester: updating repo for %s", robot.Name)
				batches.setRobotState(run, id, "updating_repo")

				data, _ := json.Marshal(req.RepoConfig)
				cmd := agent.Command{Type: "update_repo", Data: data}
				if _, err := c.runRobotCommand(ctx, robot, cmd); err != nil {
					log.Printf("semester: command failed: update_repo for %s: %v", robot.Name, err)
					batches.failRobot(run, id, err.Error())
					return
				}
			}

			if req.ApplyScenarios {
				log.Printf("semester: applying scenarios for %s", robot.Name)
				batches.setRobotState(run, id, "applying_scenarios")

				var commands []agent.Command
				for _, config := range req.ScenarioConfigs {
					data, _ := json.Marshal(config)
					commands = append(commands, agent.Command{Type: "update_repo", Data: data})
				}

				batchData := agent.BatchData{Commands: commands}
				batchPayload, _ := json.Marshal(batchData)
				cmd := agent.Command{Type: "batch", Data: batchPayload}

				if _, err := c.runRobotCommand(ctx, robot, cmd); err != nil {
					log.Printf("semester: command failed: batch scenarios for %s: %v", robot.Name, err)
					batches.failRobot(run, id, err.Error())
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
				batches.setRobotState(run, id, "running_self_test")

				// Test Drive
				driveData, _ := json.Marshal(agent.TestDriveData{DurationSec: 2})
				cmdDrive := agent.Command{Type: "test_drive", Data: driveData}
				if _, err := c.runRobotCommand(ctx, robot, cmdDrive); err != nil {
					log.Printf("semester: command failed: test_drive for %s: %v", robot.Name, err)
					batches.failRobot(run, id, err.Error())
					return
				}

				// Capture Image
				uploadURL := fmt.Sprintf("%s/api/robots/%d/upload", baseURL, id)
				captureData, _ := json.Marshal(agent.CaptureImageData{UploadURL: uploadURL})
				cmdCapture := agent.Command{Type: "capture_image", Data: captureData}
				if _, err := c.runRobotCommand(ctx, robot, cmdCapture); err != nil {
					log.Printf("semester: command failed: capture_image for %s: %v", robot.Name, err)
					batches.failRobot(run, id, err.Error())
					return
				}
			}

			// Bundle package installation and camera setup into one acknowledged job.
			cameraBundled := false
			if req.SystemUpgrade || req.InstallPackages {
				log.Printf("semester: updating system packages for %s", robot.Name)
				batches.setRobotState(run, id, "updating_system")

				data, _ := json.Marshal(agent.SystemUpdateData{Upgrade: req.SystemUpgrade, Packages: req.Packages})
				cmd := agent.Command{Type: "system_update", Data: data}
				if req.InstallCameraSupport {
					batchPayload, _ := json.Marshal(agent.BatchData{Commands: []agent.Command{
						cmd,
						{Type: "install_camera_support", Data: []byte("{}")},
					}})
					cmd = agent.Command{Type: "batch", Data: batchPayload}
					cameraBundled = true
				}
				if _, err := c.runRobotCommand(ctx, robot, cmd); err != nil {
					log.Printf("semester: command failed: system_update for %s: %v", robot.Name, err)
					batches.failRobot(run, id, err.Error())
					return
				}
			}

			if req.InstallCameraSupport && !cameraBundled {
				log.Printf("semester: installing camera support for %s", robot.Name)
				batches.setRobotState(run, id, "installing_camera_support")

				cmd := agent.Command{Type: "install_camera_support", Data: []byte("{}")}
				if _, err := c.runRobotCommand(ctx, robot, cmd); err != nil {
					log.Printf("semester: command failed: install_camera_support for %s: %v", robot.Name, err)
					batches.failRobot(run, id, err.Error())
					return
				}
			}

			if req.FactoryReset {
				log.Printf("semester: requesting factory reset for %s", robot.Name)
				batches.setRobotState(run, id, "factory_reset")

				cmd := agent.Command{Type: "factory_reset", Data: []byte("{}")}
				if _, err := c.runRobotCommand(ctx, robot, cmd); err != nil {
					log.Printf("semester: command failed: factory_reset for %s: %v", robot.Name, err)
					batches.failRobot(run, id, err.Error())
					return
				}
			}

			batches.completeRobot(run, id)
		}(id)
	}
	wg.Wait()
	log.Printf("semester batch complete")
}
