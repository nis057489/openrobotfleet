package controller

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"example.com/openrobot-fleet/internal/agent"
	"example.com/openrobot-fleet/internal/db"
	sshc "example.com/openrobot-fleet/internal/ssh"
)

// errMissingInstallConfig means the device has no usable SSH details, even
// after falling back to its type's install defaults.
var errMissingInstallConfig = errors.New("missing install config")

// reinstallAgent re-runs the SSH install on an existing device using its
// saved credentials, falling back to its type's install defaults, then pins
// the newly detected agent ID onto its row. It returns when the install
// itself started, so callers can wait for a heartbeat newer than that.
// Errors are worded for display.
func (c *Controller) reinstallAgent(ctx context.Context, robot *db.Robot) (time.Time, error) {
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
		log.Printf("reinstall: robot %d missing install config (addr=%v, user=%v, key_len=%d)", robot.ID,
			robot.InstallConfig != nil && robot.InstallConfig.Address != "",
			robot.InstallConfig != nil && robot.InstallConfig.User != "",
			func() int {
				if robot.InstallConfig != nil {
					return len(robot.InstallConfig.SSHKey)
				}
				return 0
			}())
		return time.Time{}, errMissingInstallConfig
	}

	addr := robot.InstallConfig.Address
	if robot.IP != "" {
		addr = robot.IP
	}
	if !strings.Contains(addr, ":") {
		addr = net.JoinHostPort(addr, "22")
	}

	// Sudo password, most specific source first. The saved per-device
	// password wins, then the defaults for this device type (laptops keep
	// their own account and password), then the environment. "ubuntu" is
	// only our own fleet image's convention, so a guess is tracked and
	// reported rather than surfacing an opaque "exit status 1".
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
		log.Printf("reinstall: failed to detect arch for %s: %v", robot.Name, err)
		return time.Time{}, fmt.Errorf("failed to detect arch: %w", err)
	}

	agentID, err := sshc.DetectPrimaryMAC(host)
	if err != nil {
		log.Printf("reinstall: failed to detect device identity for %s: %v", robot.Name, err)
		return time.Time{}, fmt.Errorf("failed to detect device identity: %w", err)
	}

	workspace := os.Getenv("AGENT_WORKSPACE_PATH")
	if workspace == "" {
		workspace = "/home/ubuntu/ros_ws/src/course"
	}
	cfg := agent.Config{
		AgentID:        agentID,
		Type:           robot.Type,
		MQTTBroker:     c.agentBrokerURL(ctx),
		MQTTUsername:   os.Getenv("AGENT_MQTT_USERNAME"),
		MQTTPassword:   os.Getenv("AGENT_MQTT_PASSWORD"),
		WorkspacePath:  workspace,
		WorkspaceOwner: determineWorkspaceOwner(installAgentRequest{User: robot.InstallConfig.User}),
	}

	binary, err := os.ReadFile(agentBinaryPath(arch))
	if err != nil {
		log.Printf("reinstall: failed to read agent binary: %v", err)
		return time.Time{}, errors.New("agent binary unavailable")
	}

	installStart := time.Now()
	if err := sshc.InstallAgent(host, cfg, robot.Name, binary); err != nil {
		log.Printf("reinstall: failed to install agent on %s: %v", robot.Name, err)
		switch {
		case strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no route to host") || strings.Contains(err.Error(), "i/o timeout"):
			return installStart, errors.New("Connection failed. Check connection or restart robot.")
		case useSudo && sudoPwdGuessed && strings.Contains(err.Error(), "exited with status 1"):
			return installStart, errors.New("Privileged command failed and no sudo password is saved for this device, so \"ubuntu\" was tried. Set the sudo password in Settings under this device type's install defaults.")
		}
		return installStart, fmt.Errorf("install failed: %w", err)
	}

	// Pin the newly-detected agent_id onto this same row now, rather than
	// waiting for the agent's first heartbeat to do it -- UpsertRobotStatus
	// (heartbeat writes) is keyed by agent_id, so until this row's agent_id
	// column is updated to match what the reinstalled agent will report, a
	// heartbeat can't find it and would instead create a duplicate row.
	robotIP := robot.InstallConfig.Address
	if host, _, err := net.SplitHostPort(addr); err == nil {
		robotIP = host
	}
	// InstallAgent takes no context, so a cancelled caller cannot interrupt
	// it -- the agent is already on the device by now. This pin must
	// therefore still run, or the stale agent_id leaves the duplicate row
	// described above.
	if err := c.DB.UpsertRobotWithType(context.WithoutCancel(ctx), agentID, robot.Name, robotIP, "installed", robot.Type); err != nil {
		log.Printf("reinstall: failed to pin agent_id for %s: %v", robot.Name, err)
		return installStart, fmt.Errorf("failed to update robot: %w", err)
	}
	robot.AgentID = agentID
	c.reapplyGroupForRobot(ctx, robot.ID)
	return installStart, nil
}

// agentBinaryPath is where the controller keeps the agent build for arch
// ("amd64" or "arm64").
func agentBinaryPath(arch string) string {
	binaryDir := os.Getenv("AGENT_BINARY_DIR")
	if binaryDir == "" {
		binaryDir = "/app"
	}
	binaryName := "agent-amd64"
	if arch == "arm64" {
		binaryName = "agent-arm64"
	}
	return filepath.Join(binaryDir, binaryName)
}
