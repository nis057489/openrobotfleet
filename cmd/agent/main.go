package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"example.com/openrobot-fleet/internal/agent"
)

func main() {
	cfgPath := os.Getenv("AGENT_CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "/etc/openrobotfleet-agent/config.yaml"
	}
	cfg, err := agent.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	pinned := cfg.AgentID != ""
	resolved, err := cfg.ResolveAgentID()
	if err != nil || resolved == "" {
		log.Fatalf("failed to determine agent_id: %v", err)
	}
	cfg.AgentID = resolved

	if !pinned {
		// No identity was pinned at provisioning time (e.g. zero-touch
		// golden-image boot) -- default the OS hostname to the derived ID
		// until an admin renames the robot in the fleet UI.
		if err := agent.SetHostname(resolved); err != nil {
			log.Printf("failed to set default hostname: %v", err)
		}
	}

	if err := agent.EnsureTimezone(); err != nil {
		log.Printf("failed to set timezone: %v", err)
	}

	if err := agent.EnsureROSServiceOverride(); err != nil {
		log.Printf("failed to install ROS service override: %v", err)
	}

	if err := agent.EnsureCameraSetup(cfg); err != nil {
		log.Printf("failed to set up camera: %v", err)
	}

	log.Printf("Starting Agent %s (Behavior Tree Mode)", cfg.AgentID)

	// Create Engine
	engine := agent.NewAgentEngine(cfg)

	// Context with cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Handle signals
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sig
		log.Println("Shutting down...")
		cancel()
	}()

	// Start Engine
	engine.Start(ctx)
}
