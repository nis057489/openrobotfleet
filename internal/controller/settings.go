package controller

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"example.com/openrobot-fleet/internal/db"
)

func (c *Controller) GetInstallDefaults(w http.ResponseWriter, r *http.Request) {
	cfg, err := c.DB.GetDefaultInstallConfig(r.Context())
	if err != nil {
		log.Printf("get install defaults: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load defaults")
		return
	}
	laptopCfg, err := c.DB.GetDefaultInstallConfigFor(r.Context(), "laptop")
	if err != nil {
		log.Printf("get laptop install defaults: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load defaults")
		return
	}

	type response struct {
		*db.InstallConfig
		SSHPublicKey string `json:"ssh_public_key"`
	}
	build := func(cfg *db.InstallConfig) *response {
		pubKey := ""
		if cfg != nil && cfg.SSHKey != "" {
			pubKey, _ = prepareSSHKeys(cfg.SSHKey)
		}
		return &response{InstallConfig: cfg, SSHPublicKey: pubKey}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"install_config":        build(cfg),
		"laptop_install_config": build(laptopCfg),
		"demo_mode":             os.Getenv("DEMO_MODE") == "true",
	})
}

func (c *Controller) UpdateInstallDefaults(w http.ResponseWriter, r *http.Request) {
	var req installDefaultsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid install defaults")
		return
	}
	if err := req.validate(); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := req.toInstallConfig()
	if err := c.DB.SaveDefaultInstallConfig(r.Context(), cfg); err != nil {
		log.Printf("update install defaults: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to save defaults")
		return
	}

	// Laptop defaults are optional: sending none leaves laptops falling back
	// to the robot defaults, which is the previous behaviour.
	resp := map[string]*db.InstallConfig{"install_config": &cfg}
	if req.Laptop != nil {
		if err := req.Laptop.validate(); err != nil {
			respondError(w, http.StatusBadRequest, "laptop defaults: "+err.Error())
			return
		}
		laptopCfg := req.Laptop.toInstallConfig()
		if err := c.DB.SaveDefaultInstallConfigFor(r.Context(), "laptop", laptopCfg); err != nil {
			log.Printf("update laptop install defaults: %v", err)
			respondError(w, http.StatusInternalServerError, "failed to save laptop defaults")
			return
		}
		resp["laptop_install_config"] = &laptopCfg
	}
	respondJSON(w, http.StatusOK, resp)
}
