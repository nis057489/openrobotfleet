package controller

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"example.com/turtlebot-fleet/internal/agent"
	"example.com/turtlebot-fleet/internal/db"
	"example.com/turtlebot-fleet/internal/scenario"
)

type scenarioRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ConfigYAML  string `json:"config_yaml"`
}

func (c *Controller) ListScenarios(w http.ResponseWriter, r *http.Request) {
	scenarios, err := c.DB.ListScenarios(r.Context())
	if err != nil {
		log.Printf("list scenarios: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list scenarios")
		return
	}
	respondJSON(w, http.StatusOK, scenarios)
}

func (c *Controller) GetScenario(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDFromPath(r.URL.Path, "/api/scenarios/")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid scenario id")
		return
	}
	scenario, err := c.DB.GetScenarioByID(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			respondError(w, http.StatusNotFound, "scenario not found")
			return
		}
		log.Printf("get scenario: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to fetch scenario")
		return
	}
	respondJSON(w, http.StatusOK, scenario)
}

func (c *Controller) CreateScenario(w http.ResponseWriter, r *http.Request) {
	var req scenarioRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid scenario payload")
		return
	}
	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "scenario name required")
		return
	}
	if _, err := scenario.Parse(req.ConfigYAML); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid scenario config: %v", err))
		return
	}
	s := db.Scenario{Name: req.Name, Description: req.Description, ConfigYAML: req.ConfigYAML}
	id, err := c.DB.CreateScenario(r.Context(), s)
	if err != nil {
		log.Printf("create scenario: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to create scenario")
		return
	}
	s.ID = id
	respondJSON(w, http.StatusCreated, s)
}

func (c *Controller) UpdateScenario(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDFromPath(r.URL.Path, "/api/scenarios/")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid scenario id")
		return
	}
	var req scenarioRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid scenario payload")
		return
	}
	s := db.Scenario{ID: id, Name: req.Name, Description: req.Description, ConfigYAML: req.ConfigYAML}
	if s.Name == "" {
		respondError(w, http.StatusBadRequest, "scenario name required")
		return
	}
	if _, err := scenario.Parse(req.ConfigYAML); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid scenario config: %v", err))
		return
	}
	if err := c.DB.UpdateScenario(r.Context(), s); err != nil {
		log.Printf("update scenario: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to update scenario")
		return
	}
	respondJSON(w, http.StatusOK, s)
}

func (c *Controller) DeleteScenario(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDFromPath(r.URL.Path, "/api/scenarios/")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid scenario id")
		return
	}
	if err := c.DB.DeleteScenario(r.Context(), id); err != nil {
		log.Printf("delete scenario: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to delete scenario")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type applyScenarioRequest struct {
	RobotIDs []int64 `json:"robot_ids"`
}

type applyScenarioResponse struct {
	Jobs []db.Job `json:"jobs"`
}

func (c *Controller) ApplyScenario(w http.ResponseWriter, r *http.Request) {
	scenarioID, err := parseScenarioApplyID(r.URL.Path)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid scenario apply path")
		return
	}
	var req applyScenarioRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid apply payload")
		return
	}
	if len(req.RobotIDs) == 0 {
		respondError(w, http.StatusBadRequest, "robot_ids required")
		return
	}
	s, err := c.DB.GetScenarioByID(r.Context(), scenarioID)
	if err != nil {
		if err == sql.ErrNoRows {
			respondError(w, http.StatusNotFound, "scenario not found")
			return
		}
		log.Printf("apply scenario fetch: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load scenario")
		return
	}
	spec, err := scenario.Parse(s.ConfigYAML)
	if err != nil {
		respondError(w, http.StatusBadRequest, fmt.Sprintf("invalid scenario config: %v", err))
		return
	}
	repoPayload := spec.Repo.ToUpdateRepo()
	data, err := json.Marshal(repoPayload)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to encode scenario command")
		return
	}
	cmd := agent.Command{Type: "update_repo", Data: data}
	var jobs []db.Job
	for _, robotID := range req.RobotIDs {
		robot, err := c.DB.GetRobotByID(r.Context(), robotID)
		if err != nil {
			if err == sql.ErrNoRows {
				respondError(w, http.StatusNotFound, fmt.Sprintf("robot %d not found", robotID))
				return
			}
			log.Printf("apply scenario robot fetch: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to fetch robot")
			return
		}
		if robot.AgentID == "" {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("robot %s has no agent", robot.Name))
			return
		}
		job, err := c.queueRobotCommand(r.Context(), robot, cmd)
		if err != nil {
			log.Printf("apply scenario queue: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to queue command")
			return
		}
		if err := c.DB.UpdateRobotScenario(r.Context(), robotID, scenarioID); err != nil {
			log.Printf("apply scenario update robot: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to tag robot scenario")
			return
		}
		jobs = append(jobs, job)
	}
	respondJSON(w, http.StatusCreated, applyScenarioResponse{Jobs: jobs})
}

func parseScenarioApplyID(path string) (int64, error) {
	trimmed := strings.TrimSuffix(path, "/")
	if !strings.HasSuffix(trimmed, "/apply") {
		return 0, fmt.Errorf("missing apply suffix")
	}
	base := strings.TrimSuffix(trimmed, "/apply")
	return parseIDFromPath(base, "/api/scenarios/")
}
