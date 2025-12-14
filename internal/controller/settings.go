package controller

import (
	"encoding/json"
	"log"
	"net/http"

	"example.com/turtlebot-fleet/internal/db"
)

func (c *Controller) GetInstallDefaults(w http.ResponseWriter, r *http.Request) {
	cfg, err := c.DB.GetDefaultInstallConfig(r.Context())
	if err != nil {
		log.Printf("get install defaults: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load defaults")
		return
	}

	// Compute public key
	pubKey := ""
	if cfg != nil && cfg.SSHKey != "" {
		pubKey, _ = prepareSSHKeys(cfg.SSHKey)
	}

	type response struct {
		*db.InstallConfig
		SSHPublicKey string `json:"ssh_public_key"`
	}

	resp := &response{
		InstallConfig: cfg,
		SSHPublicKey:  pubKey,
	}

	respondJSON(w, http.StatusOK, map[string]*response{"install_config": resp})
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
	respondJSON(w, http.StatusOK, map[string]*db.InstallConfig{"install_config": &cfg})
}
