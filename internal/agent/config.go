package agent

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the agent's runtime configuration.
type Config struct {
	AgentID        string `yaml:"agent_id"`
	Type           string `yaml:"type"` // "robot" or "laptop"
	MQTTBroker     string `yaml:"mqtt_broker"`
	MQTTUsername   string `yaml:"mqtt_username,omitempty"`
	MQTTPassword   string `yaml:"mqtt_password,omitempty"`
	JobStatePath   string `yaml:"job_state_path,omitempty"`
	WorkspacePath  string `yaml:"workspace_path"`
	WorkspaceOwner string `yaml:"workspace_owner"`
}

// ResolveAgentID returns cfg.AgentID unchanged if it was pinned in the
// config file (legacy or explicitly-provisioned installs), otherwise
// derives a stable identity from the device's MAC address.
func (cfg Config) ResolveAgentID() (string, error) {
	if cfg.AgentID != "" {
		return cfg.AgentID, nil
	}
	return DeriveAgentIDFromMAC()
}

// LoadConfig reads and parses a YAML config file.
func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Errorf("config file %s not found", path)
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}
	if cfg.MQTTUsername == "" {
		cfg.MQTTUsername = os.Getenv("MQTT_USERNAME")
	}
	if cfg.MQTTPassword == "" {
		cfg.MQTTPassword = os.Getenv("MQTT_PASSWORD")
	}
	return cfg, nil
}
