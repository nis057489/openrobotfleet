package controller

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"example.com/turtlebot-fleet/internal/agent"
	"example.com/turtlebot-fleet/internal/db"
)

type commandRequest struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (c *Controller) ListRobots(w http.ResponseWriter, r *http.Request) {
	robots, err := c.DB.ListRobots(r.Context())
	if err != nil {
		log.Printf("list robots: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list robots")
		return
	}
	respondJSON(w, http.StatusOK, robots)
}

func (c *Controller) GetRobot(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDFromPath(r.URL.Path, "/api/robots/")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid robot id")
		return
	}
	robot, err := c.DB.GetRobotByID(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			respondError(w, http.StatusNotFound, "robot not found")
			return
		}
		log.Printf("get robot: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to fetch robot")
		return
	}
	respondJSON(w, http.StatusOK, robot)
}

func (c *Controller) RobotCommand(w http.ResponseWriter, r *http.Request) {
	robotID, err := parseCommandRobotID(r.URL.Path)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	robot, err := c.DB.GetRobotByID(r.Context(), robotID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondError(w, http.StatusNotFound, "robot not found")
			return
		}
		log.Printf("fetch robot for command: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to fetch robot")
		return
	}
	if robot.AgentID == "" {
		respondError(w, http.StatusBadRequest, "robot has no agent attached")
		return
	}
	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid command payload")
		return
	}
	if req.Type == "" {
		respondError(w, http.StatusBadRequest, "command type required")
		return
	}
	cmd := agent.Command{Type: req.Type, Data: req.Data}
	job, err := c.queueRobotCommand(r.Context(), robot, cmd)
	if err != nil {
		log.Printf("queue command: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to queue command")
		return
	}
	respondJSON(w, http.StatusCreated, job)
}

func (c *Controller) BroadcastCommand(w http.ResponseWriter, r *http.Request) {
	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid command payload")
		return
	}
	if req.Type == "" {
		respondError(w, http.StatusBadRequest, "command type required")
		return
	}
	cmd := agent.Command{Type: req.Type, Data: req.Data}
	payload, err := json.Marshal(cmd)
	if err != nil {
		log.Printf("marshal broadcast: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to encode command")
		return
	}
	now := time.Now().UTC()
	job := db.Job{
		Type:        req.Type,
		TargetRobot: "all",
		PayloadJSON: string(payload),
		Status:      "queued",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	jobID, err := c.DB.CreateJob(r.Context(), job)
	if err != nil {
		log.Printf("create broadcast job: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to create job")
		return
	}
	job.ID = jobID
	log.Printf("broadcast command %s queued to lab/commands/all", req.Type)
	c.MQTT.Publish("lab/commands/all", payload)
	respondJSON(w, http.StatusCreated, job)
}

func (c *Controller) UpdateInstallConfig(w http.ResponseWriter, r *http.Request) {
	robotID, err := parseInstallConfigRobotID(r.URL.Path)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	var req installConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid install config")
		return
	}
	if err := req.validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := req.toInstallConfig()
	if err := c.DB.UpdateRobotInstallConfigByID(r.Context(), robotID, cfg); err != nil {
		log.Printf("update install config: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to save install config")
		return
	}
	robot, err := c.DB.GetRobotByID(r.Context(), robotID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondError(w, http.StatusNotFound, "robot not found")
			return
		}
		log.Printf("fetch robot after install config update: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to fetch robot")
		return
	}
	respondJSON(w, http.StatusOK, robot)
}

func (c *Controller) UpdateRobotTags(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDFromPath(r.URL.Path, "/api/robots/")
	if err != nil {
		// Try parsing with /tags suffix removed if needed, but parseIDFromPath expects prefix
		// The path is likely /api/robots/123/tags
		// Let's manually parse since the helper is strict
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 4 {
			respondError(w, http.StatusBadRequest, "invalid path")
			return
		}
		// parts: ["", "api", "robots", "123", "tags"]
		idStr := parts[3]
		var parseErr error
		id, parseErr = strconv.ParseInt(idStr, 10, 64)
		if parseErr != nil {
			respondError(w, http.StatusBadRequest, "invalid robot id")
			return
		}
	}

	var req struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	if err := c.DB.UpdateRobotTags(r.Context(), id, req.Tags); err != nil {
		log.Printf("update tags: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to update tags")
		return
	}
	
	// Return updated robot
	robot, err := c.DB.GetRobotByID(r.Context(), id)
	if err != nil {
		respondJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		return
	}
	respondJSON(w, http.StatusOK, robot)
}

func (c *Controller) queueRobotCommand(ctx context.Context, robot db.Robot, cmd agent.Command) (db.Job, error) {
	payload, err := json.Marshal(cmd)
	if err != nil {
		return db.Job{}, fmt.Errorf("marshal command: %w", err)
	}
	now := time.Now().UTC()
	job := db.Job{
		Type:        cmd.Type,
		TargetRobot: robot.AgentID,
		PayloadJSON: string(payload),
		Status:      "queued",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	jobID, err := c.DB.CreateJob(ctx, job)
	if err != nil {
		return db.Job{}, fmt.Errorf("create job: %w", err)
	}
	job.ID = jobID
	topic := fmt.Sprintf("lab/commands/%s", robot.AgentID)
	log.Printf("command %s queued for robot %s (agent %s) topic %s", cmd.Type, robot.Name, robot.AgentID, topic)
	c.MQTT.Publish(topic, payload)
	return job, nil
}
