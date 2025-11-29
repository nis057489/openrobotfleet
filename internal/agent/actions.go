package agent

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// HandleUpdateRepo clones the requested git repository to the target directory.
func HandleUpdateRepo(cfg Config, data UpdateRepoData) error {
	if data.Repo == "" {
		return errors.New("repo is required")
	}
	branch := data.Branch
	if branch == "" {
		branch = "main"
	}
	target := destinationPath(cfg.WorkspacePath, data.Path, data.Repo)
	if target == "" || target == "/" {
		return errors.New("invalid target path")
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("clean target %s: %w", target, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("prepare parent %s: %w", filepath.Dir(target), err)
	}
	cmd := exec.Command("git", "clone", "--branch", branch, "--single-branch", data.Repo, target)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := ensureOwnership(target, cfg); err != nil {
		return err
	}
	log.Printf("[agent] cloned %s (branch %s) into %s", data.Repo, branch, target)
	return nil
}

// HandleResetLogs truncates or clears the provided log files.
func HandleResetLogs(cfg Config, data ResetLogsData) error {
	paths := data.Paths
	if len(paths) == 0 {
		if cfg.WorkspacePath == "" {
			return errors.New("no log paths provided")
		}
		paths = []string{filepath.Join(cfg.WorkspacePath, "logs")}
	}
	for _, raw := range paths {
		resolved := resolvePath(cfg.WorkspacePath, raw)
		if resolved == "" || resolved == "/" {
			return fmt.Errorf("refusing to modify path %q", resolved)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("stat %s: %w", resolved, err)
		}
		if info.IsDir() {
			entries, err := os.ReadDir(resolved)
			if err != nil {
				return fmt.Errorf("read dir %s: %w", resolved, err)
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				filePath := filepath.Join(resolved, entry.Name())
				if err := truncateFile(filePath, info.Mode()); err != nil {
					return err
				}
			}
			continue
		}
		if err := truncateFile(resolved, info.Mode()); err != nil {
			return err
		}
	}
	log.Printf("[agent] reset logs for %d path(s)", len(paths))
	return nil
}

// HandleRestartROS restarts the ROS service via systemd or a custom command.
func HandleRestartROS(cfg Config) error {
	cmdArgs := customRestartCommand()
	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restart ros failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	log.Printf("[agent] restarted ROS using %s", strings.Join(cmdArgs, " "))
	return nil
}

// HandleTestDrive executes a short movement pattern.
func HandleTestDrive(cfg Config, data TestDriveData) error {
	log.Printf("[agent] starting test drive")

	// Twist message for forward motion
	// linear.x = 0.1, angular.z = 0.0
	cmdForward := exec.Command("ros2", "topic", "pub", "--once", "/cmd_vel", "geometry_msgs/msg/Twist", "{linear: {x: 0.1, y: 0.0, z: 0.0}, angular: {x: 0.0, y: 0.0, z: 0.0}}")
	if out, err := cmdForward.CombinedOutput(); err != nil {
		return fmt.Errorf("forward failed: %v: %s", err, string(out))
	}

	time.Sleep(time.Duration(data.DurationSec) * time.Second)

	// Stop
	cmdStop := exec.Command("ros2", "topic", "pub", "--once", "/cmd_vel", "geometry_msgs/msg/Twist", "{linear: {x: 0.0, y: 0.0, z: 0.0}, angular: {x: 0.0, y: 0.0, z: 0.0}}")
	if out, err := cmdStop.CombinedOutput(); err != nil {
		return fmt.Errorf("stop failed: %v: %s", err, string(out))
	}

	log.Printf("[agent] test drive complete")
	return nil
}

// HandleStop publishes zero velocity.
func HandleStop(cfg Config) error {
	log.Printf("[agent] stopping robot")
	cmd := exec.Command("ros2", "topic", "pub", "--once", "/cmd_vel", "geometry_msgs/msg/Twist", "{linear: {x: 0.0, y: 0.0, z: 0.0}, angular: {x: 0.0, y: 0.0, z: 0.0}}")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("stop failed: %v: %s", err, string(out))
	}
	return nil
}

// HandleIdentify makes the robot beep and flash LEDs to identify itself.
func HandleIdentify() error {
	log.Println("[agent] identifying robot...")

	// 1. Beep
	// Create 3 uses /cmd_audio (irobot_create_msgs/msg/AudioNoteVector)
	// We'll try a simple beep sequence.
	// Note: This requires the irobot_create_msgs package to be installed/sourced.
	// If not available, this might fail, but we'll log it.
	// Sequence: 2 beeps
	beepCmd := exec.Command("ros2", "topic", "pub", "--once", "/cmd_audio", "irobot_create_msgs/msg/AudioNoteVector",
		`{append: false, notes: [{frequency: 880, max_runtime: {sec: 0, nanosec: 500000000}}, {frequency: 0, max_runtime: {sec: 0, nanosec: 100000000}}, {frequency: 880, max_runtime: {sec: 0, nanosec: 500000000}}]}`)
	if out, err := beepCmd.CombinedOutput(); err != nil {
		log.Printf("[agent] failed to beep: %v: %s", err, string(out))
	}

	// 2. Flash LEDs
	// Create 3 uses /cmd_lightring (irobot_create_msgs/msg/LightringLeds)
	// We'll flash red a few times.
	// We need to run this in a loop or send a sequence if possible.
	// Since 'ros2 topic pub' blocks if we don't use --once, we'll just send a "red" command, wait, then "off".

	// Red
	ledRed := exec.Command("ros2", "topic", "pub", "--once", "/cmd_lightring", "irobot_create_msgs/msg/LightringLeds",
		`{override_system: true, leds: [{red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}]}`)
	if out, err := ledRed.CombinedOutput(); err != nil {
		log.Printf("[agent] failed to set LEDs red: %v: %s", err, string(out))
	}

	time.Sleep(1 * time.Second)

	// Off (or return to system control)
	// To return to system control, we can set override_system to false.
	ledOff := exec.Command("ros2", "topic", "pub", "--once", "/cmd_lightring", "irobot_create_msgs/msg/LightringLeds",
		`{override_system: false, leds: []}`)
	if out, err := ledOff.CombinedOutput(); err != nil {
		log.Printf("[agent] failed to reset LEDs: %v: %s", err, string(out))
	}

	return nil
}

// HandleCaptureImage takes a photo and uploads it.
func HandleCaptureImage(cfg Config, data CaptureImageData) error {
	log.Printf("[agent] capturing image")
	tmpPath := "/tmp/snapshot.jpg"

	// Try fswebcam first
	cmd := exec.Command("fswebcam", "-r", "640x480", "--jpeg", "85", "-D", "1", tmpPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[agent] fswebcam failed: %v: %s", err, string(out))
		// Fallback: create a dummy image or fail?
		// Let's fail for now, or maybe try a different tool if needed.
		return fmt.Errorf("capture failed: %v", err)
	}
	defer os.Remove(tmpPath)

	// Upload
	file, err := os.Open(tmpPath)
	if err != nil {
		return err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("image", filepath.Base(tmpPath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	writer.Close()

	req, err := http.NewRequest("POST", data.UploadURL, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("upload failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upload returned status: %s", resp.Status)
	}

	log.Printf("[agent] image uploaded to %s", data.UploadURL)
	return nil
}

// HandleWifiProfile configures wifi (placeholder).
func HandleWifiProfile(data WifiProfileData) error {
	log.Printf("[agent] wifi profile received for %s (not implemented)", data.SSID)
	return nil
}

func destinationPath(workspace, provided, repo string) string {
	switch {
	case provided != "" && filepath.IsAbs(provided):
		return filepath.Clean(provided)
	case provided != "":
		if workspace != "" {
			return filepath.Join(workspace, provided)
		}
		return filepath.Clean(provided)
	case workspace != "":
		base := strings.TrimSuffix(filepath.Base(repo), ".git")
		return filepath.Join(workspace, base)
	default:
		return ""
	}
}

func resolvePath(workspace, p string) string {
	if p == "" {
		return filepath.Clean(workspace)
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if workspace == "" {
		return filepath.Clean(p)
	}
	return filepath.Join(workspace, p)
}

func truncateFile(path string, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, mode)
	if err != nil {
		return fmt.Errorf("truncate %s: %w", path, err)
	}
	return f.Close()
}

func customRestartCommand() []string {
	if cmd := os.Getenv("ROS_RESTART_CMD"); cmd != "" {
		parts := strings.Fields(cmd)
		if len(parts) >= 1 {
			return parts
		}
	}
	service := os.Getenv("ROS_SERVICE_NAME")
	if service == "" {
		service = "ros"
	}
	return []string{"systemctl", "restart", service}
}

func ensureOwnership(target string, cfg Config) error {
	if os.Geteuid() != 0 {
		return nil
	}
	owner := strings.TrimSpace(cfg.WorkspaceOwner)
	if owner == "" {
		owner = detectOwnerFromPath(cfg.WorkspacePath)
	}
	if owner == "" {
		owner = detectOwnerFromPath(filepath.Dir(target))
	}
	if owner == "" {
		return nil
	}
	cmd := exec.Command("chown", "-R", owner, target)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("chown %s: %w: %s", target, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func detectOwnerFromPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ""
		}
		log.Printf("owner detect stat %s: %v", path, err)
		return ""
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d:%d", stat.Uid, stat.Gid)
}
