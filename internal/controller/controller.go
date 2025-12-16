package controller

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"example.com/turtlebot-fleet/internal/db"
	mqttc "example.com/turtlebot-fleet/internal/mqtt"
)

type RobotJobState struct {
	JobID     string
	JobStatus string
	JobError  string
	UpdatedAt time.Time
}

// Controller holds shared dependencies for HTTP handlers.
type Controller struct {
	DB            *db.DB
	MQTT          *mqttc.Client
	OnBuildUpdate func(status string, progress int, step string, logs []string, errorMsg string, imageName string)

	jobStates   map[string]RobotJobState
	jobStatesMu sync.RWMutex
}

func New(dbConn *db.DB, mqttClient *mqttc.Client) *Controller {
	return &Controller{
		DB:        dbConn,
		MQTT:      mqttClient,
		jobStates: make(map[string]RobotJobState),
	}
}

func (c *Controller) UpdateRobotJobStatus(agentID, jobID, status, errStr string) {
	c.jobStatesMu.Lock()
	defer c.jobStatesMu.Unlock()
	c.jobStates[agentID] = RobotJobState{
		JobID:     jobID,
		JobStatus: status,
		JobError:  errStr,
		UpdatedAt: time.Now(),
	}
}

func (c *Controller) GetRobotJobStatus(agentID string) RobotJobState {
	c.jobStatesMu.RLock()
	defer c.jobStatesMu.RUnlock()
	return c.jobStates[agentID]
}

func (c *Controller) Health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func respondJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}

func parseIDFromPath(path, prefix string) (int64, error) {
	if !strings.HasPrefix(path, prefix) {
		return 0, fmt.Errorf("invalid path")
	}
	tail := strings.TrimPrefix(path, prefix)
	tail = strings.Trim(tail, "/")
	if tail == "" {
		return 0, fmt.Errorf("missing id")
	}
	id, err := strconv.ParseInt(tail, 10, 64)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func parseCommandRobotID(path string) (int64, error) {
	if !strings.HasPrefix(path, "/api/robots/") || !strings.HasSuffix(path, "/command") {
		return 0, fmt.Errorf("invalid command path")
	}
	trimmed := strings.TrimSuffix(path, "/command")
	trimmed = strings.TrimSuffix(trimmed, "/")
	trimmed = strings.TrimPrefix(trimmed, "/api/robots/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return 0, fmt.Errorf("missing robot id")
	}
	return strconv.ParseInt(trimmed, 10, 64)
}

func parseInstallConfigRobotID(path string) (int64, error) {
	if !strings.HasPrefix(path, "/api/robots/") || !strings.HasSuffix(path, "/install-config") {
		return 0, fmt.Errorf("invalid install config path")
	}
	trimmed := strings.TrimSuffix(path, "/install-config")
	trimmed = strings.TrimSuffix(trimmed, "/")
	trimmed = strings.TrimPrefix(trimmed, "/api/robots/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		return 0, fmt.Errorf("missing robot id")
	}
	return strconv.ParseInt(trimmed, 10, 64)
}

func (c *Controller) HandleRobotUpload(w http.ResponseWriter, r *http.Request) {
	// Parse ID from path /api/robots/:id/upload
	path := r.URL.Path
	if !strings.HasPrefix(path, "/api/robots/") || !strings.HasSuffix(path, "/upload") {
		respondError(w, http.StatusBadRequest, "invalid path")
		return
	}
	trimmed := strings.TrimSuffix(path, "/upload")
	trimmed = strings.TrimSuffix(trimmed, "/")
	trimmed = strings.TrimPrefix(trimmed, "/api/robots/")
	idStr := strings.Trim(trimmed, "/")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid robot id")
		return
	}

	file, _, err := r.FormFile("image")
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to get image")
		return
	}
	defer file.Close()

	// Save to web/dist/snapshots/<id>.jpg
	webRoot := os.Getenv("WEB_ROOT")
	if webRoot == "" {
		webRoot = "./web/dist"
	}
	snapDir := filepath.Join(webRoot, "snapshots")
	if err := os.MkdirAll(snapDir, 0755); err != nil {
		log.Printf("failed to create snapshot dir: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to save")
		return
	}

	dstPath := filepath.Join(snapDir, fmt.Sprintf("%d.jpg", id))
	out, err := os.Create(dstPath)
	if err != nil {
		log.Printf("failed to create snapshot file: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to save")
		return
	}
	defer out.Close()

	if _, err := io.Copy(out, file); err != nil {
		log.Printf("failed to write snapshot file: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to save")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "uploaded", "url": fmt.Sprintf("/snapshots/%d.jpg", id)})
}
