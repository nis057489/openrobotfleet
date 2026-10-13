package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"example.com/openrobot-fleet/internal/agent"
	"example.com/openrobot-fleet/internal/scenario"
)

type semesterRequest struct {
	RobotIDs             []int64              `json:"robot_ids"`
	Reinstall            bool                 `json:"reinstall"`
	ResetLogs            bool                 `json:"reset_logs"`
	UpdateRepo           bool                 `json:"update_repo"`
	RunSelfTest          bool                 `json:"run_self_test"`
	InstallCameraSupport bool                 `json:"install_camera_support"`
	ResetBashrc          bool                 `json:"reset_bashrc"`
	SyncTime             bool                 `json:"sync_time"`
	TurtleBot3Model      string               `json:"turtlebot3_model"`
	QtQPAPlatform        string               `json:"qt_qpa_platform"`
	SystemUpgrade        bool                 `json:"system_upgrade"`
	InstallPackages      bool                 `json:"install_packages"`
	InstallNav2          bool                 `json:"install_nav2"` // laptops only
	Packages             []string             `json:"packages"`
	RepoConfig           agent.UpdateRepoData `json:"repo_config"`
	ApplyScenarios       bool                 `json:"apply_scenarios"`
	ScenarioIDs          []int64              `json:"scenario_ids"`
	FactoryReset         bool                 `json:"factory_reset"`
	RebootIfRequired     bool                 `json:"reboot_if_required"`

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
		switch pkgs[0] {
		case "sudo", "apt", "apt-get":
			respondError(w, http.StatusBadRequest, "list package names only, not an apt command (e.g. \"vim htop\", not \"sudo apt install vim htop\")")
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

	// A repeated selection must not execute maintenance or self-test twice.
	seen := make(map[int64]bool)
	unique := make([]int64, 0, len(req.RobotIDs))
	for _, id := range req.RobotIDs {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	req.RobotIDs = unique

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, r.Host)

	// Batches run concurrently across devices. Each device is held for its
	// entire workflow so overlapping batches cannot interleave their steps.
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

	var wg sync.WaitGroup
	for _, id := range req.RobotIDs {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()

			batches.setRobotState(run, id, "waiting_for_device")
			release, err := c.acquireBatchDevice(ctx, id)
			if err != nil {
				batches.failRobot(run, id, err.Error())
				return
			}
			defer release()

			batches.setRobotState(run, id, "processing")

			robot, err := c.DB.GetRobotByID(ctx, id)
			if err != nil {
				log.Printf("semester: failed to get robot %d: %v", id, err)
				batches.failRobot(run, id, "robot not found")
				return
			}

			if req.Reinstall {
				log.Printf("semester: reinstalling agent on %s", robot.Name)
				batches.setRobotState(run, id, "installing_agent")
				installStart, err := c.reinstallAgent(ctx, &robot)
				switch {
				case errors.Is(err, errMissingInstallConfig) && os.Getenv("DEMO_MODE") == "true":
					// In demo mode, fake success for reinstall and fall
					// through to the other steps.
					log.Printf("semester: demo mode, skipping reinstall for %s", robot.Name)
				case err != nil:
					batches.failRobot(run, id, err.Error())
					return
				case req.SyncTime || req.ResetLogs || req.UpdateRepo || req.ApplyScenarios:
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

			// Before any other command, so later steps run with the
			// device's clock matching the controller's.
			if req.SyncTime {
				log.Printf("semester: syncing time for %s", robot.Name)
				batches.setRobotState(run, id, "syncing_time")

				cmd := agent.Command{Type: "sync_time", Data: []byte("{}")}
				if _, err := c.runRobotCommand(ctx, robot, cmd); err != nil {
					log.Printf("semester: command failed: sync_time for %s: %v", robot.Name, err)
					batches.failRobot(run, id, err.Error())
					return
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

				data, _ := json.Marshal(agent.ResetBashrcData{TurtleBot3Model: req.TurtleBot3Model, QtQPAPlatform: req.QtQPAPlatform})
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
			packages := req.Packages
			if req.InstallNav2 && robot.Type == "laptop" {
				packages = append(append([]string{}, packages...), agent.Nav2LaptopPackages...)
			}
			if req.SystemUpgrade || len(packages) > 0 {
				log.Printf("semester: updating system packages for %s", robot.Name)
				batches.setRobotState(run, id, "updating_system")

				data, _ := json.Marshal(agent.SystemUpdateData{Upgrade: req.SystemUpgrade, Packages: packages})
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

			// Last, so no later step runs into the reboot. A factory reset
			// reboots by itself.
			if req.RebootIfRequired && !req.FactoryReset {
				setState := func(state string) { batches.setRobotState(run, id, state) }
				if err := c.rebootIfRequired(ctx, robot, setState); err != nil {
					log.Printf("semester: reboot failed for %s: %v", robot.Name, err)
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
