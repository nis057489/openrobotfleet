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
		cfgPath = "/etc/turtlebot-agent/config.yaml"
	}
	cfg, err := agent.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}
	if cfg.AgentID == "" {
		log.Fatalf("config missing agent_id")
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
