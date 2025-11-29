package controller

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"example.com/turtlebot-fleet/internal/agent"
	"example.com/turtlebot-fleet/internal/db"
	sshc "example.com/turtlebot-fleet/internal/ssh"
)

type installAgentRequest struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Address string `json:"address"`
	User    string `json:"user"`
	SSHKey  string `json:"ssh_key"`
	Sudo    bool   `json:"sudo"`
	SudoPwd string `json:"sudo_password"`
}

func (c *Controller) InstallAgent(w http.ResponseWriter, r *http.Request) {
	var req installAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Address == "" || req.User == "" || req.SSHKey == "" {
		respondError(w, http.StatusBadRequest, "name, address, user, and ssh_key required")
		return
	}
	rType := req.Type
	if rType == "" {
		rType = "robot"
	}
	binaryPath := os.Getenv("AGENT_BINARY_PATH")
	if binaryPath == "" {
		binaryPath = "/app/agent"
	}
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		log.Printf("install agent: read binary: %v", err)
		respondError(w, http.StatusInternalServerError, "agent binary unavailable")
		return
	}
	workspace := os.Getenv("AGENT_WORKSPACE_PATH")
	if workspace == "" {
		workspace = "/home/ubuntu/ros_ws/src/course"
	}
	addr := req.Address
	if !strings.Contains(addr, ":") {
		addr = net.JoinHostPort(addr, "22")
	}
	sudoPwd := req.SudoPwd
	if sudoPwd == "" {
		sudoPwd = os.Getenv("AGENT_SUDO_PASSWORD")
	}
	useSudo := req.Sudo || strings.ToLower(req.User) != "root"
	if useSudo && sudoPwd == "" {
		sudoPwd = "ubuntu"
	}
	if useSudo && sudoPwd == "" {
		respondError(w, http.StatusBadRequest, "sudo password required")
		return
	}
	broker := agentBrokerURL()
	cfg := agent.Config{
		AgentID:        req.Name,
		MQTTBroker:     broker,
		WorkspacePath:  workspace,
		WorkspaceOwner: determineWorkspaceOwner(req),
	}
	host := sshc.HostSpec{
		Addr:         addr,
		User:         req.User,
		PrivateKey:   []byte(req.SSHKey),
		UseSudo:      useSudo,
		SudoPassword: sudoPwd,
	}
	if err := sshc.InstallAgent(host, cfg, binary); err != nil {
		log.Printf("install agent: ssh failure: %v", err)
		msg := "failed to install agent"
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no route to host") || strings.Contains(err.Error(), "i/o timeout") {
			msg = "Connection failed. Please check the connection or restart the robot."
		}
		respondError(w, http.StatusInternalServerError, msg)
		return
	}
	robotIP := req.Address
	if err := c.DB.UpdateRobotInstallConfigByName(r.Context(), req.Name, db.InstallConfig{Address: req.Address, User: req.User, SSHKey: req.SSHKey}); err != nil {
		log.Printf("install agent: save install config: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to save robot install config")
		return
	}
	if hostIP, _, err := net.SplitHostPort(addr); err == nil {
		robotIP = hostIP
	}
	if err := c.DB.UpsertRobotWithType(r.Context(), cfg.AgentID, req.Name, robotIP, "installed", rType); err != nil {
		log.Printf("install agent: upsert robot: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to update robot")
		return
	}
	if err := c.DB.UpdateRobotInstallConfigByName(r.Context(), req.Name, db.InstallConfig{
		Address: req.Address,
		User:    req.User,
		SSHKey:  req.SSHKey,
	}); err != nil {
		log.Printf("install agent: persist install config: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to save install settings")
		return
	}
	robot, err := c.DB.GetRobotByName(r.Context(), req.Name)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("install agent: fetch robot: %v", err)
		}
		respondError(w, http.StatusInternalServerError, "failed to fetch robot")
		return
	}
	respondJSON(w, http.StatusCreated, robot)
}

func agentBrokerURL() string {
	if v := os.Getenv("AGENT_MQTT_BROKER"); v != "" {
		return v
	}
	if v := os.Getenv("MQTT_PUBLIC_BROKER"); v != "" {
		return v
	}
	if v := os.Getenv("MQTT_BROKER"); v != "" && !strings.Contains(v, "tcp://mqtt") {
		return v
	}
	return "tcp://192.168.100.122:1883"
}

func determineWorkspaceOwner(req installAgentRequest) string {
	if v := os.Getenv("AGENT_WORKSPACE_OWNER"); v != "" {
		return v
	}
	if strings.TrimSpace(req.User) != "" && strings.ToLower(req.User) != "root" {
		return req.User
	}
	if v := os.Getenv("DEFAULT_WORKSPACE_OWNER"); v != "" {
		return v
	}
	return "ubuntu"
}
