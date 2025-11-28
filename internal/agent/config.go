package agent

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the agent's runtime configuration.
type Config struct {
	AgentID       string `yaml:"agent_id"`
	MQTTBroker    string `yaml:"mqtt_broker"`
	WorkspacePath string `yaml:"workspace_path"`
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
	return cfg, nil
}
