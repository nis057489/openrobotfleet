package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"example.com/openrobot-fleet/internal/agent/behavior"
	mqttc "example.com/openrobot-fleet/internal/mqtt"
	mqttlib "github.com/eclipse/paho.mqtt.golang"
)

const (
	commandStaleThreshold = 10 * time.Minute
)

// durableCommandTypes represent desired state rather than one-shot actions
// and remain in the controller outbox until applied, even after a long
// disconnection. They are exempt from the one-shot command expiry.
var durableCommandTypes = map[string]bool{
	"configure_network": true,
	"set_hostname":      true,
}

type AgentEngine struct {
	Config     Config
	MQTTClient *mqttc.Client
	JobManager *JobManager
	Blackboard *behavior.Blackboard
	Tree       behavior.Node

	cmdChan       chan Command
	lastIP        string
	lastHeartbeat time.Time
}

func NewAgentEngine(cfg Config) *AgentEngine {
	bb := behavior.NewBlackboard()
	jm := NewJobManager()

	engine := &AgentEngine{
		Config:     cfg,
		JobManager: jm,
		Blackboard: bb,
		cmdChan:    make(chan Command, 10),
	}

	// Initialize Blackboard
	bb.Set(behavior.KeyConfig, cfg)
	bb.Set(behavior.KeyJobManager, jm)

	return engine
}

func (e *AgentEngine) Start(ctx context.Context) {
	// 1. Connect MQTT
	e.connectMQTT()

	// 2. Build Tree
	e.Tree = e.buildTree()

	// 3. Loop
	ticker := time.NewTicker(100 * time.Millisecond) // 10Hz Tick
	defer ticker.Stop()

	log.Println("Agent Engine started (Behavior Tree Mode)")

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.Tree.Tick(ctx, e.Blackboard)
		}
	}
}

func (e *AgentEngine) connectMQTT() {
	client := mqttc.NewClientWithCredentials(e.Config.AgentID, e.Config.MQTTBroker, e.Config.MQTTUsername, e.Config.MQTTPassword, nil)
	e.MQTTClient = client
	e.Blackboard.Set(behavior.KeyMQTTClient, client)
	client.Subscribe("lab/commands/"+e.Config.AgentID, e.mqttHandler)
	client.Subscribe("lab/acks/"+e.Config.AgentID, func(_ mqttlib.Client, msg mqttlib.Message) {
		var ids []string
		if json.Unmarshal(msg.Payload(), &ids) == nil {
			e.JobManager.Acknowledge(ids)
		}
	})
}

func (e *AgentEngine) mqttHandler(_ mqttlib.Client, msg mqttlib.Message) {
	// Commands now come from the controller's durable outbox. Never replay a
	// retained command left by an older controller (including reboot).
	if len(msg.Payload()) == 0 || msg.Retained() {
		return
	}
	var cmd Command
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		log.Printf("invalid command: %v", err)
		return
	}
	if cmd.Type == "stop" {
		e.acceptCommand(cmd)
		return
	}
	select {
	case e.cmdChan <- cmd:
	default:
		// No acknowledgement: the controller will retry the stored command.
		log.Printf("command inbox full; awaiting retry of %s", cmd.ID)
	}
}

func (e *AgentEngine) acceptCommand(cmd Command) {
	if cmd.ID == "" {
		log.Printf("rejecting command without ID")
		return
	}
	err := e.JobManager.StartContextJob(cmd.ID, cmd.Type, cmd.Data, func(ctx context.Context) error {
		if !durableCommandTypes[cmd.Type] && (cmd.Timestamp == 0 || time.Since(time.Unix(cmd.Timestamp, 0)) > commandStaleThreshold) {
			return fmt.Errorf("command expired before execution")
		}
		action := e.mapCommandToAction(ctx, cmd)
		if action == nil {
			return fmt.Errorf("unknown command type: %s", cmd.Type)
		}
		return action()
	})
	if err != nil {
		log.Printf("accept command %s: %v", cmd.ID, err)
	}
}

func (e *AgentEngine) buildTree() behavior.Node {
	return &behavior.Parallel{
		Children: []behavior.Node{
			&behavior.ActionNode{Action: e.checkNetwork},
			&behavior.ActionNode{Action: e.maintainConnection},
			&behavior.ActionNode{Action: e.processCommands},
			&behavior.ActionNode{Action: e.sendHeartbeat},
		},
	}
}

func (e *AgentEngine) maintainConnection(ctx context.Context, bb *behavior.Blackboard) behavior.Status {
	// Keep command processing and local safety actions ticking while offline.
	return behavior.StatusSuccess
}

// --- Leaf Nodes ---

func (e *AgentEngine) checkNetwork(ctx context.Context, bb *behavior.Blackboard) behavior.Status {
	currentIP := DetectIPv4()
	if currentIP != e.lastIP {
		if e.lastIP != "" {
			log.Printf("IP changed from %s to %s", e.lastIP, currentIP)
		}
		e.lastIP = currentIP
		bb.Set(behavior.KeyIPAddress, currentIP)
	}
	return behavior.StatusSuccess
}

func (e *AgentEngine) processCommands(ctx context.Context, bb *behavior.Blackboard) behavior.Status {
	select {
	case cmd := <-e.cmdChan:
		e.acceptCommand(cmd)
	default:
	}
	return behavior.StatusSuccess
}

func (e *AgentEngine) sendHeartbeat(ctx context.Context, bb *behavior.Blackboard) behavior.Status {
	if time.Since(e.lastHeartbeat) < 10*time.Second {
		return behavior.StatusSuccess
	}

	payload := e.buildStatusPayload()
	if e.MQTTClient != nil && e.MQTTClient.Client != nil && e.MQTTClient.Client.IsConnected() {
		topic := "lab/status/" + e.Config.AgentID
		if err := e.MQTTClient.Publish(topic, 1, false, payload); err == nil {
			e.lastHeartbeat = time.Now()
		}
	}

	return behavior.StatusSuccess
}

func (e *AgentEngine) buildStatusPayload() []byte {
	type status struct {
		Status    string `json:"status"`
		TS        string `json:"ts"`
		IP        string `json:"ip"`
		Type      string `json:"type,omitempty"`
		Name      string `json:"name,omitempty"`
		JobID     string `json:"job_id,omitempty"`
		JobStatus string `json:"job_status,omitempty"`
		JobError  string `json:"job_error,omitempty"`
		Camera    string `json:"camera,omitempty"`
		Results   []Job  `json:"results,omitempty"`
	}

	s := status{
		Status:  "ok",
		TS:      time.Now().Format(time.RFC3339),
		IP:      e.lastIP,
		Type:    e.Config.Type,
		Name:    e.Config.AgentID,
		Camera:  CameraServiceState(),
		Results: e.JobManager.Results(),
	}
	if hn, err := os.Hostname(); err == nil && hn != "" {
		s.Name = hn
	}

	// Add Job info
	if job := e.JobManager.GetCurrentJob(); job != nil {
		s.JobID = job.ID
		s.JobStatus = string(job.Status)
		s.JobError = job.Error
		if job.Status == JobStatusFailed {
			s.Status = "error"
		}

		// install_camera_support can take several minutes (building
		// libcamera/camera_ros from source) with no other feedback in the
		// UI, so surface it via the same status field the robot list/detail
		// pages already render -- no controller or frontend wiring needed,
		// since status flows through untouched end to end.
		if job.Type == "install_camera_support" && job.Status == JobStatusRunning {
			s.Status = "setting_up"
		}
	}

	buf, _ := json.Marshal(s)
	return buf
}

func (e *AgentEngine) mapCommandToAction(ctx context.Context, cmd Command) func() error {
	cfg := e.Config

	switch cmd.Type {
	case "set_hostname":
		var payload SetHostnameData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return HandleSetHostname(payload) }
	case "configure_network":
		var payload ConfigureNetworkData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return HandleConfigureNetwork(cfg, payload) }
	case "update_repo":
		var payload UpdateRepoData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return HandleUpdateRepo(cfg, payload) }
	case "reset_logs":
		var payload ResetLogsData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return HandleResetLogs(cfg, payload) }
	case "reset_bashrc":
		var payload ResetBashrcData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return HandleResetBashrc(cfg, payload) }
	case "system_update":
		var payload SystemUpdateData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return HandleSystemUpdate(payload) }
	case "restart_ros":
		return func() error { return HandleRestartROS(cfg) }
	case "wifi_profile":
		var payload WifiProfileData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return HandleWifiProfile(payload) }
	case "test_drive":
		var payload TestDriveData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return HandleTestDriveContext(ctx, cfg, payload) }
	case "stop":
		return func() error { return HandleStop(cfg) }
	case "capture_image":
		var payload CaptureImageData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return HandleCaptureImage(cfg, payload) }
	case "camera_start":
		return func() error { return HandleCameraService(true) }
	case "camera_stop":
		return func() error { return HandleCameraService(false) }
	case "install_camera_support":
		return func() error { return HandleInstallCameraSupport(cfg) }
	case "identify":
		var payload IdentifyData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return HandleIdentify(cfg, payload) }
	case "reboot":
		return func() error { return HandleReboot(cfg) }
	case "factory_reset":
		return func() error { return HandleFactoryReset(cfg) }
	case "batch":
		var payload BatchData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return e.HandleBatch(ctx, payload) }
	default:
		log.Printf("unknown command type: %s", cmd.Type)
		return nil
	}
}

func (e *AgentEngine) HandleBatch(ctx context.Context, data BatchData) error {
	for i, cmd := range data.Commands {
		log.Printf("batch: executing command %d/%d: %s", i+1, len(data.Commands), cmd.Type)
		if err := ctx.Err(); err != nil {
			return err
		}
		action := e.mapCommandToAction(ctx, cmd)
		if action == nil {
			return fmt.Errorf("unknown command in batch: %s", cmd.Type)
		}
		if err := action(); err != nil {
			return fmt.Errorf("batch failed at %s: %w", cmd.Type, err)
		}
	}
	return nil
}
