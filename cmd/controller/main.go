package main

import (
	"log"
	"os"

	"example.com/turtlebot-fleet/internal/http"
)

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "controller.db"
	}

	server, err := httpserver.NewServer(dbPath)
	if err != nil {
		log.Fatalf("failed to init server: %v", err)
	}

	if err := server.Start(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
