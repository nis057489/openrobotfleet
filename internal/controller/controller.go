package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"example.com/turtlebot-fleet/internal/db"
	mqttc "example.com/turtlebot-fleet/internal/mqtt"
)

// Controller holds shared dependencies for HTTP handlers.
type Controller struct {
	DB   *db.DB
	MQTT *mqttc.Client
}

func New(dbConn *db.DB, mqttClient *mqttc.Client) *Controller {
	return &Controller{DB: dbConn, MQTT: mqttClient}
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
