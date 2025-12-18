package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"example.com/openrobot-fleet/internal/agent/behavior"
	mqttc "example.com/openrobot-fleet/internal/mqtt"
	mqttlib "github.com/eclipse/paho.mqtt.golang"
)

type AgentEngine struct {
	Config     Config
	MQTTClient *mqttc.Client
	JobManager *JobManager
	Blackboard *behavior.Blackboard
	Tree       behavior.Node

	cmdChan            chan Command
	lastIP             string
	lastHeartbeat      time.Time
	lastConnectAttempt time.Time
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
	onConnect := func(c mqttlib.Client) {
		log.Printf("MQTT Connected")
		// Subscribe
		topic := "lab/commands/" + e.Config.AgentID
		log.Printf("Subscribing to %s", topic)
		if token := c.Subscribe(topic, 0, e.mqttHandler); token.Wait() && token.Error() != nil {
			log.Printf("subscribe error: %v", token.Error())
		}
		if token := c.Subscribe("lab/commands/all", 0, e.mqttHandler); token.Wait() && token.Error() != nil {
			log.Printf("subscribe all error: %v", token.Error())
		}
	}

	client := mqttc.NewClientWithHandler("agent-"+e.Config.AgentID, e.Config.MQTTBroker, onConnect)
	e.MQTTClient = client
	e.Blackboard.Set(behavior.KeyMQTTClient, client)
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
			&behavior.ActionNode{Action: e.maintainConnection},
			&behavior.ActionNode{Action: e.processCommands},
			&behavior.ActionNode{Action: e.sendHeartbeat},
		},
	}
}

func (e *AgentEngine) maintainConnection(ctx context.Context, bb *behavior.Blackboard) behavior.Status {
	if e.MQTTClient == nil || e.MQTTClient.Client == nil {
		return behavior.StatusFailure
	}
	if !e.MQTTClient.Client.IsConnected() {
		if time.Since(e.lastConnectAttempt) > 5*time.Second {
			log.Println("MQTT disconnected, attempting reconnect...")
			go func() {
				token := e.MQTTClient.Client.Connect()
				if token.Wait() && token.Error() != nil {
					log.Printf("reconnect failed: %v", token.Error())
				}
			}()
			e.lastConnectAttempt = time.Now()
		}
		return behavior.StatusFailure
	}
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
		e.MQTTClient.PublishRetained(topic, payload)
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
	case "configure_agent":
		var payload ConfigureAgentData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return HandleConfigureAgent(cfg, payload) }
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
	case "batch":
		var payload BatchData
		if err := json.Unmarshal(cmd.Data, &payload); err != nil {
			return func() error { return err }
		}
		return func() error { return e.HandleBatch(payload) }
	default:
		log.Printf("unknown command type: %s", cmd.Type)
		return nil
	}
}

func (e *AgentEngine) HandleBatch(data BatchData) error {
	for i, cmd := range data.Commands {
		log.Printf("batch: executing command %d/%d: %s", i+1, len(data.Commands), cmd.Type)
		action := e.mapCommandToAction(cmd)
		if action == nil {
			return fmt.Errorf("unknown command in batch: %s", cmd.Type)
		}
		if err := action(); err != nil {
			return fmt.Errorf("batch failed at %s: %w", cmd.Type, err)
		}
	}
	return nil
}
