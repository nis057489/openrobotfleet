package main

import (
	"encoding/json"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"example.com/turtlebot-fleet/internal/agent"
	mqttc "example.com/turtlebot-fleet/internal/mqtt"
	mqttlib "github.com/eclipse/paho.mqtt.golang"
)

func main() {
	cfgPath := os.Getenv("AGENT_CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "/etc/turtlebot-agent/config.yaml"
	}
	cfg, err := agent.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	if cfg.AgentID == "" {
		log.Fatalf("config missing agent_id")
	}
	agentID := cfg.AgentID
	workspacePath := cfg.WorkspacePath
	if workspacePath != "" {
		log.Printf("workspace path: %s", workspacePath)
	}

	client := mqttc.NewClientWithBroker("agent-"+agentID, cfg.MQTTBroker)
	commandTopics := []string{
		"lab/commands/" + agentID,
		"lab/commands/all",
	}

	handler := func(_ mqttlib.Client, msg mqttlib.Message) {
		log.Printf("received command on %s: %s", msg.Topic(), string(msg.Payload()))
		var cmd agent.Command
		if err := json.Unmarshal(msg.Payload(), &cmd); err != nil {
			log.Printf("invalid command JSON: %v", err)
			return
		}
		switch cmd.Type {
		case "update_repo":
			var payload agent.UpdateRepoData
			if err := json.Unmarshal(cmd.Data, &payload); err != nil {
				log.Printf("update_repo payload error: %v", err)
				return
			}
			if err := agent.HandleUpdateRepo(cfg, payload); err != nil {
				log.Printf("update_repo failed: %v", err)
				return
			}
			log.Printf("update_repo succeeded for repo %s", payload.Repo)
		case "reset_logs":
			var payload agent.ResetLogsData
			if err := json.Unmarshal(cmd.Data, &payload); err != nil {
				log.Printf("reset_logs payload error: %v", err)
				return
			}
			if err := agent.HandleResetLogs(cfg, payload); err != nil {
				log.Printf("reset_logs failed: %v", err)
				return
			}
			log.Printf("reset_logs succeeded")
		case "restart_ros":
			if err := agent.HandleRestartROS(cfg); err != nil {
				log.Printf("restart_ros failed: %v", err)
				return
			}
			log.Printf("restart_ros succeeded")
		case "wifi_profile":
			var payload agent.WifiProfileData
			if err := json.Unmarshal(cmd.Data, &payload); err != nil {
				log.Printf("wifi_profile payload error: %v", err)
				return
			}
			if err := agent.HandleWifiProfile(payload); err != nil {
				log.Printf("wifi_profile failed: %v", err)
				return
			}
			log.Printf("wifi_profile succeeded for ssid %s", payload.SSID)
		case "test_drive":
			var payload agent.TestDriveData
			if err := json.Unmarshal(cmd.Data, &payload); err != nil {
				log.Printf("test_drive payload error: %v", err)
				return
			}
			if err := agent.HandleTestDrive(cfg, payload); err != nil {
				log.Printf("test_drive failed: %v", err)
				return
			}
			log.Printf("test_drive succeeded")
		case "stop":
			if err := agent.HandleStop(cfg); err != nil {
				log.Printf("stop failed: %v", err)
				return
			}
			log.Printf("stop succeeded")
		case "capture_image":
			var payload agent.CaptureImageData
			if err := json.Unmarshal(cmd.Data, &payload); err != nil {
				log.Printf("capture_image payload error: %v", err)
				return
			}
			if err := agent.HandleCaptureImage(cfg, payload); err != nil {
				log.Printf("capture_image failed: %v", err)
				return
			}
			log.Printf("capture_image succeeded")
		case "identify":
			if err := agent.HandleIdentify(); err != nil {
				log.Printf("identify failed: %v", err)
				return
			}
			log.Printf("identify succeeded")
		default:
			log.Printf("unknown command type: %s", cmd.Type)
		}
	}

	for _, topic := range commandTopics {
		log.Printf("agent %s subscribing to %s", agentID, topic)
		client.Subscribe(topic, handler)
	}

	statusTopic := "lab/status/" + agentID

	// Periodic heartbeat
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	log.Println("agent started, sending heartbeats...")

	for {
		select {
		case <-sig:
			log.Println("shutting down agent")
			return
		case <-time.After(10 * time.Second):
			payload := buildStatusPayload()
			client.Publish(statusTopic, payload)
		}
	}
}

func buildStatusPayload() []byte {
	type status struct {
		Status string `json:"status"`
		TS     string `json:"ts"`
		IP     string `json:"ip"`
	}
	data := status{
		Status: "ok",
		TS:     time.Now().Format(time.RFC3339),
		IP:     detectIPv4(),
	}
	buf, err := json.Marshal(data)
	if err != nil {
		log.Printf("status marshal error: %v", err)
		return []byte(`{"status":"ok"}`)
	}
	return buf
}

func detectIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Printf("ip detect: %v", err)
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue
			}
			return ip.String()
		}
	}
	return ""
}
