package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Read configuration for each subprocess: changing a group must affect the
// very next stop/drive/camera command without restarting the agent.
func rosEnvironment(path string, inherited []string) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return inherited, nil
	}
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !ok {
			return nil, fmt.Errorf("invalid ROS environment line")
		}
		switch key {
		case "ROS_DOMAIN_ID":
			domain, err := strconv.Atoi(value)
			if err != nil || domain < 0 || domain > 232 {
				return nil, fmt.Errorf("invalid ROS domain")
			}
		case "RMW_IMPLEMENTATION", "CYCLONEDDS_URI":
		default:
			return nil, fmt.Errorf("unexpected ROS environment key %q", key)
		}
		values[key] = value
	}
	env := make([]string, 0, len(inherited)+len(values))
	for _, entry := range inherited {
		key, _, _ := strings.Cut(entry, "=")
		if key != "ROS_DOMAIN_ID" && key != "RMW_IMPLEMENTATION" && key != "CYCLONEDDS_URI" {
			env = append(env, entry)
		}
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env, nil
}

func rosCommand(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	env, err := rosEnvironment(rosEnvPath, os.Environ())
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	// Killing ros2 on timeout does not necessarily close its output pipe: the
	// ROS 2 CLI spawns a background daemon that inherits it, and CombinedOutput
	// blocks until every writer is gone. Without a WaitDelay that wait is
	// unbounded, so a command that outlives ctx hangs the job forever -- and
	// since the agent runs one job at a time, it wedges the whole queue.
	cmd.WaitDelay = 5 * time.Second
	return cmd, nil
}
func runROSCmd(timeout time.Duration, name string, args ...string) ([]byte, error) {
	return runROSCmdContext(context.Background(), timeout, name, args...)
}
func runROSCmdContext(parent context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd, err := rosCommand(ctx, name, args...)
	if err != nil {
		return nil, err
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

func writeAtomicFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".env-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
