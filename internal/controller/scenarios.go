package controller

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"example.com/turtlebot-fleet/internal/db"
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
	scenario := db.Scenario{Name: req.Name, Description: req.Description, ConfigYAML: req.ConfigYAML}
	id, err := c.DB.CreateScenario(r.Context(), scenario)
	if err != nil {
		log.Printf("create scenario: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to create scenario")
		return
	}
	scenario.ID = id
	respondJSON(w, http.StatusCreated, scenario)
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
	scenario := db.Scenario{ID: id, Name: req.Name, Description: req.Description, ConfigYAML: req.ConfigYAML}
	if scenario.Name == "" {
		respondError(w, http.StatusBadRequest, "scenario name required")
		return
	}
	if err := c.DB.UpdateScenario(r.Context(), scenario); err != nil {
		log.Printf("update scenario: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to update scenario")
		return
	}
	respondJSON(w, http.StatusOK, scenario)
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
