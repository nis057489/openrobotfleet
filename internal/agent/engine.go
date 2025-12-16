package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"example.com/turtlebot-fleet/internal/agent/behavior"
	mqttc "example.com/turtlebot-fleet/internal/mqtt"
	mqttlib "github.com/eclipse/paho.mqtt.golang"
)

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
	client := mqttc.NewClientWithBroker("agent-"+e.Config.AgentID, e.Config.MQTTBroker)
	e.MQTTClient = client
	e.Blackboard.Set(behavior.KeyMQTTClient, client)

	// Subscribe
	topic := "lab/commands/" + e.Config.AgentID
	log.Printf("Subscribing to %s", topic)
	client.Subscribe(topic, e.mqttHandler)
	client.Subscribe("lab/commands/all", e.mqttHandler)
}

func (e *AgentEngine) mqttHandler(_ mqttlib.Client, msg mqttlib.Message) {
	var cmd Command
	if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
		log.Printf("invalid command JSON: %v", err)
		return
	}
	// Non-blocking send
	select {
	case e.cmdChan <- cmd:
		log.Printf("Queued command: %s", cmd.Type)
	default:
		log.Printf("command queue full, dropping command: %s", cmd.Type)
	}
}

func (e *AgentEngine) buildTree() behavior.Node {
	return &behavior.Parallel{
		Children: []behavior.Node{
			&behavior.ActionNode{Action: e.checkNetwork},
			&behavior.ActionNode{Action: e.processCommands},
			&behavior.ActionNode{Action: e.sendHeartbeat},
		},
	}
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
		action := e.mapCommandToAction(cmd)
		if action != nil {
			jobID := fmt.Sprintf("%d", time.Now().UnixNano())
			e.JobManager.StartJob(jobID, cmd.Type, cmd.Data, action)
		}
	default:
		// No commands
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
		e.MQTTClient.Publish(topic, payload)
		e.lastHeartbeat = time.Now()
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
	}

	s := status{
		Status: "ok",
		TS:     time.Now().Format(time.RFC3339),
		IP:     e.lastIP,
		Type:   e.Config.Type,
		Name:   e.Config.AgentID,
	}

	// Add Job info
	if job := e.JobManager.GetCurrentJob(); job != nil {
		s.JobID = job.ID
		s.JobStatus = string(job.Status)
		s.JobError = job.Error
	}

	buf, _ := json.Marshal(s)
	return buf
}

func (e *AgentEngine) mapCommandToAction(cmd Command) func() error {
	cfg := e.Config

	switch cmd.Type {
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
		return func() error { return HandleTestDrive(cfg, payload) }
	case "stop":
		return func() error { return HandleStop(cfg) }
	case "capture_image":
		var payload CaptureImageData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return HandleCaptureImage(cfg, payload) }
	case "identify":
		var payload IdentifyData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return HandleIdentify(cfg, payload) }
	case "reboot":
		return func() error { return HandleReboot(cfg) }
	default:
		log.Printf("unknown command type: %s", cmd.Type)
		return nil
	}
}
