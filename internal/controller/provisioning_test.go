package controller

import (
	"bytes"
	"example.com/openrobot-fleet/internal/agent"
	"example.com/openrobot-fleet/internal/db"
	"gopkg.in/yaml.v3"
	"testing"
	"text/template"
)

func TestGoldenImageIncludesRestrictedAgentCredentials(t *testing.T) {
	data := struct {
		*db.GoldenImageConfig
		SSHPublicKey, MQTTUsername, MQTTPassword string
	}{
		GoldenImageConfig: &db.GoldenImageConfig{MQTTBroker: "tcp://controller:1883"}, MQTTUsername: "agents", MQTTPassword: "secret with \"quotes\" and : punctuation",
	}
	tmpl, err := template.New("user-data").Parse(userDataTemplate)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, data); err != nil {
		t.Fatal(err)
	}
	var cloud struct {
		Files []struct {
			Path        string `yaml:"path"`
			Permissions string `yaml:"permissions"`
			Content     string `yaml:"content"`
		} `yaml:"write_files"`
	}
	if err := yaml.Unmarshal(output.Bytes(), &cloud); err != nil {
		t.Fatal(err)
	}
	for _, file := range cloud.Files {
		if file.Path == "/etc/openrobotfleet-agent/config.yaml" {
			if file.Permissions != "0600" {
				t.Fatal("credentials are not root-readable only")
			}
			var cfg agent.Config
			if err := yaml.Unmarshal([]byte(file.Content), &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.MQTTUsername != data.MQTTUsername || cfg.MQTTPassword != data.MQTTPassword {
				t.Fatal("agent credentials not preserved")
			}
			return
		}
	}
	t.Fatal("agent config not generated")
}
