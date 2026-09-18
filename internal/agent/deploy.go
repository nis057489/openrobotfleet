package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// deploymentPath only permits strict descendants of the configured workspace.
func deploymentPath(workspace, provided, repo string) (string, string, error) {
	if workspace == "" {
		return "", "", errors.New("workspace is required")
	}
	base, err := filepath.Abs(workspace)
	if err != nil || base == "/" {
		return "", "", errors.New("invalid workspace")
	}
	target := destinationPath(base, provided, repo)
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", "", errors.New("deployment target must be inside the workspace")
	}
	return base, rel, nil
}

func HandleUpdateRepo(cfg Config, data UpdateRepoData) error {
	if strings.TrimSpace(data.Repo) == "" || strings.HasPrefix(data.Repo, "-") {
		return errors.New("invalid repository")
	}
	base, target, err := deploymentPath(cfg.WorkspacePath, data.Path, data.Repo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0755); err != nil {
		return err
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer root.Close()
	// Reject symlink targets and ancestors, even if they currently point inside
	// the workspace. Root also confines the later operations against path races.
	part := ""
	for _, component := range strings.Split(target, string(filepath.Separator)) {
		part = filepath.Join(part, component)
		info, err := root.Lstat(part)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in deployment path: %s", part)
		}
	}
	if err := root.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(base, ".fleet-deploy-")
	if err != nil {
		return err
	}
	stageName := filepath.Base(stage)
	cleanup := true
	defer func() {
		if cleanup {
			_ = root.RemoveAll(stageName)
		}
	}()
	branch := data.Branch
	if branch == "" {
		branch = "main"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "clone", "--branch", branch, "--single-branch", "--", data.Repo, filepath.Join(stage, "repo"))
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := ensureOwnership(filepath.Join(stage, "repo"), cfg); err != nil {
		return err
	}
	backup := filepath.Join(stageName, "previous")
	hadPrevious := false
	if _, err := root.Lstat(target); err == nil {
		if err := root.Rename(target, backup); err != nil {
			return err
		}
		hadPrevious = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := root.Rename(filepath.Join(stageName, "repo"), target); err != nil {
		if hadPrevious {
			if rollbackErr := root.Rename(backup, target); rollbackErr != nil {
				cleanup = false // Preserve the only copy of the old deployment.
				return fmt.Errorf("install failed: %v; rollback failed: %v; previous deployment saved at %s", err, rollbackErr, filepath.Join(base, backup))
			}
		}
		return err
	}
	return nil
}
