package scenario

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"example.com/openrobot-fleet/internal/agent"
	"gopkg.in/yaml.v3"
)

// Spec describes declarative scenario instructions stored as YAML.
type Spec struct {
	Repo RepoSpec `yaml:"repo"`
}

// RepoSpec declares which git repo/branch/path a scenario expects on a robot.
type RepoSpec struct {
	URL    string `yaml:"url"`
	Branch string `yaml:"branch"`
	Path   string `yaml:"path"`
}

// Parse converts the scenario config YAML into a Spec.
func Parse(raw string) (Spec, error) {
	var spec Spec
	if strings.TrimSpace(raw) == "" {
		return spec, errors.New("scenario config is empty")
	}
	if err := yaml.Unmarshal([]byte(raw), &spec); err != nil {
		return spec, fmt.Errorf("parse scenario config: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// Validate ensures required fields are populated.
func (s Spec) Validate() error {
	if strings.TrimSpace(s.Repo.URL) == "" {
		return errors.New("scenario repo url is required")
	}
	return nil
}

// ToUpdateRepo builds the payload sent to agents.
func (r RepoSpec) ToUpdateRepo() agent.UpdateRepoData {
	branch := strings.TrimSpace(r.Branch)
	if branch == "" {
		branch = "main"
	}
	path := strings.TrimSpace(r.Path)
	if path == "" {
		repoName := strings.TrimSuffix(filepath.Base(r.URL), ".git")
		path = repoName
	}
	return agent.UpdateRepoData{
		Repo:   r.URL,
		Branch: branch,
		Path:   path,
	}
}
