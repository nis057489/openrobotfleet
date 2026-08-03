package agent

import (
	"bytes"
	"context"
	"crypto/tls"
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

	"gopkg.in/yaml.v3"
)

// defaultCmdTimeout bounds any command the JobManager waits on synchronously.
// The agent only runs one job at a time (job_manager.go) and silently drops
// new commands while one is "running", so a subprocess that never exits (e.g.
// `ros2 topic pub` waiting forever for a subscriber that will never appear)
// permanently jams the robot's entire command queue, not just that one job.
const defaultCmdTimeout = 15 * time.Second

// runCmd runs name/args with a timeout so a hung subprocess can't block the
// agent's command queue forever; see defaultCmdTimeout.
func runCmd(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("timed out after %s", timeout)
	}
	return out, err
}

// HandleConfigureAgent updates the agent configuration and restarts the service.
func HandleConfigureAgent(cfg Config, data ConfigureAgentData) error {
	if data.AgentID == "" {
		return errors.New("agent_id required")
	}

	// Update config struct
	cfg.AgentID = data.AgentID

	// Write back to file
	cfgPath := os.Getenv("AGENT_CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "/etc/openrobotfleet-agent/config.yaml"
	}

	// Read existing to preserve other fields if needed, but we have full config in memory usually.
	// Actually cfg passed here is a copy.
	// Let's just marshal the updated cfg.
	// Wait, cfg passed to this function might be incomplete if we don't pass the full config around.
	// But LoadConfig returns full config.
	// Let's re-read to be safe or just use what we have.
	// The cfg passed to HandleConfigureAgent comes from e.Config in engine.go, which is loaded at startup.

	bytes, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(cfgPath, bytes, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	log.Printf("[agent] updated config with new agent_id: %s", data.AgentID)

	// Restart service
	// We assume systemd
	go func() {
		time.Sleep(1 * time.Second)
		cmd := exec.Command("systemctl", "restart", "openrobotfleet-agent")
		if err := cmd.Run(); err != nil {
			log.Printf("failed to restart agent: %v", err)
			// Fallback: exit and let systemd restart us
			os.Exit(0)
		}
	}()

	return nil
}

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

const (
	rosEnvPath     = "/etc/openrobotfleet-agent/ros_env.sh"
	cycloneDDSPath = "/etc/openrobotfleet-agent/cyclonedds.xml"
)

// HandleConfigureNetwork writes the DDS/ROS networking env file (and, if
// static peers are supplied, a Cyclone DDS peer-discovery config) for this
// robot's group assignment, then restarts ROS so the new environment takes
// effect. With no static peers, any previous Cyclone peer config is removed
// so the robot falls back to normal multicast discovery.
func HandleConfigureNetwork(cfg Config, data ConfigureNetworkData) error {
	if err := os.MkdirAll(filepath.Dir(rosEnvPath), 0o755); err != nil {
		return fmt.Errorf("prepare config dir: %w", err)
	}

	rmw := data.RMWImplementation
	if rmw == "" {
		rmw = "rmw_cyclonedds_cpp"
	}

	var env strings.Builder
	fmt.Fprintf(&env, "export RMW_IMPLEMENTATION=%s\n", rmw)
	fmt.Fprintf(&env, "export ROS_DOMAIN_ID=%d\n", data.ROSDomainID)

	if len(data.StaticPeers) > 0 {
		var xml strings.Builder
		xml.WriteString("<CycloneDDS><Domain><Discovery><Peers AddLocalhost=\"true\">\n")
		for _, peer := range data.StaticPeers {
			fmt.Fprintf(&xml, "  <Peer Address=\"%s\"/>\n", peer)
		}
		xml.WriteString("</Peers></Discovery></Domain></CycloneDDS>\n")
		if err := os.WriteFile(cycloneDDSPath, []byte(xml.String()), 0o644); err != nil {
			return fmt.Errorf("write cyclonedds config: %w", err)
		}
		fmt.Fprintf(&env, "export CYCLONEDDS_URI=file://%s\n", cycloneDDSPath)
	} else if err := os.Remove(cycloneDDSPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale cyclonedds config: %w", err)
	}

	if err := os.WriteFile(rosEnvPath, []byte(env.String()), 0o644); err != nil {
		return fmt.Errorf("write ros env: %w", err)
	}

	log.Printf("[agent] configured network: domain=%d rmw=%s static_peers=%d", data.ROSDomainID, rmw, len(data.StaticPeers))
	return HandleRestartROS(cfg)
}

// HandleRestartROS restarts the ROS service via systemd or a custom command.
func HandleRestartROS(cfg Config) error {
	cmdArgs := customRestartCommand()
	output, err := runCmd(defaultCmdTimeout, cmdArgs[0], cmdArgs[1:]...)
	if err != nil {
		return fmt.Errorf("restart ros failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	log.Printf("[agent] restarted ROS using %s", strings.Join(cmdArgs, " "))
	return nil
}

// HandleTestDrive executes a short movement pattern.
func HandleTestDrive(cfg Config, data TestDriveData) error {
	log.Printf("[agent] starting test drive")

	// Twist message for forward motion. -w 0 publishes immediately instead of
	// waiting (by default, forever) for a matching subscriber, since cmd_vel
	// is fire-and-forget and there may be no bringup node running to receive
	// it yet.
	// linear.x = 0.1, angular.z = 0.0
	if out, err := runCmd(defaultCmdTimeout, "ros2", "topic", "pub", "--once", "-w", "0", "/cmd_vel", "geometry_msgs/msg/Twist", "{linear: {x: 0.1, y: 0.0, z: 0.0}, angular: {x: 0.0, y: 0.0, z: 0.0}}"); err != nil {
		return fmt.Errorf("forward failed: %v: %s", err, string(out))
	}

	time.Sleep(time.Duration(data.DurationSec) * time.Second)

	// Stop
	if out, err := runCmd(defaultCmdTimeout, "ros2", "topic", "pub", "--once", "-w", "0", "/cmd_vel", "geometry_msgs/msg/Twist", "{linear: {x: 0.0, y: 0.0, z: 0.0}, angular: {x: 0.0, y: 0.0, z: 0.0}}"); err != nil {
		return fmt.Errorf("stop failed: %v: %s", err, string(out))
	}

	log.Printf("[agent] test drive complete")
	return nil
}

// HandleStop publishes zero velocity.
func HandleStop(cfg Config) error {
	log.Printf("[agent] stopping robot")
	if out, err := runCmd(defaultCmdTimeout, "ros2", "topic", "pub", "--once", "-w", "0", "/cmd_vel", "geometry_msgs/msg/Twist", "{linear: {x: 0.0, y: 0.0, z: 0.0}, angular: {x: 0.0, y: 0.0, z: 0.0}}"); err != nil {
		return fmt.Errorf("stop failed: %v: %s", err, string(out))
	}
	return nil
}

// HandleIdentify makes the robot beep and flash LEDs to identify itself.
func HandleIdentify(cfg Config, data IdentifyData) error {
	log.Println("[agent] identifying robot...")

	// Blink Pi LED (fire and forget)
	blinkPiLED(data.Pattern, data.Duration)

	if cfg.Type == "laptop" {
		return identifyLaptop(data)
	}

	// TurtleBot3's turtlebot3_node exposes a /sound service
	// (turtlebot3_msgs/srv/Sound) once bringup (ros.service) is running.
	// That's what our golden image actually launches, so try it first.
	if _, err := runCmd(defaultCmdTimeout, "ros2", "service", "call", "/sound", "turtlebot3_msgs/srv/Sound", "value: 1"); err == nil {
		return nil
	}

	// Fall back to the iRobot Create 3 (TurtleBot4) audio/lightring topics.
	if out, err := runCmd(defaultCmdTimeout, "ros2", "topic", "pub", "--once", "-w", "0", "/cmd_audio", "irobot_create_msgs/msg/AudioNoteVector",
		`{append: false, notes: [{frequency: 880, max_runtime: {sec: 0, nanosec: 500000000}}, {frequency: 0, max_runtime: {sec: 0, nanosec: 100000000}}, {frequency: 880, max_runtime: {sec: 0, nanosec: 500000000}}]}`); err != nil {
		log.Printf("[agent] failed to beep via ROS: %v: %s", err, string(out))
		// Fallback to laptop identification (system beep) if ROS fails
		if err := identifyLaptop(data); err != nil {
			log.Printf("[agent] fallback identify failed: %v", err)
		}
		return nil
	}

	// Flash LEDs (Create 3 lightring; TurtleBot3 has no equivalent hardware,
	// so this only does anything on a TB4).
	// Red
	if out, err := runCmd(defaultCmdTimeout, "ros2", "topic", "pub", "--once", "-w", "0", "/cmd_lightring", "irobot_create_msgs/msg/LightringLeds",
		`{override_system: true, leds: [{red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}]}`); err != nil {
		log.Printf("[agent] failed to set LEDs red: %v: %s", err, string(out))
	}

	time.Sleep(1 * time.Second)

	// Off (or return to system control)
	// To return to system control, we can set override_system to false.
	if out, err := runCmd(defaultCmdTimeout, "ros2", "topic", "pub", "--once", "-w", "0", "/cmd_lightring", "irobot_create_msgs/msg/LightringLeds",
		`{override_system: false, leds: []}`); err != nil {
		log.Printf("[agent] failed to reset LEDs: %v: %s", err, string(out))
	}

	return nil
}

func identifyLaptop(data IdentifyData) error {
	// Sound (fire and forget)
	go func() {
		// Try speaker-test (ALSA)
		cmd := exec.Command("speaker-test", "-t", "sine", "-f", "1000", "-l", "1")
		if err := cmd.Run(); err != nil {
			// Try beep command
			exec.Command("beep").Run()
		}
	}()

	// Visual: TTY takeover (works even if not logged in)
	if data.ID != "" {
		go func() {
			// Get current VT
			out, _ := exec.Command("fgconsole").Output()
			currentVT := strings.TrimSpace(string(out))
			if currentVT == "" {
				currentVT = "1"
			}

			// Switch to VT 6
			exec.Command("chvt", "6").Run()

			// Write to tty6
			f, err := os.OpenFile("/dev/tty6", os.O_WRONLY, 0)
			if err == nil {
				// Clear screen
				f.WriteString("\033[2J\033[H")
				f.WriteString("\n\n")

				if _, err := exec.LookPath("figlet"); err == nil {
					cmd := exec.Command("figlet", "-w", "100", data.ID)
					cmd.Stdout = f
					cmd.Run()
					fmt.Fprintf(f, "\n")
					cmd = exec.Command("figlet", "-w", "100", data.Name)
					cmd.Stdout = f
					cmd.Run()
					fmt.Fprintf(f, "\n")
					cmd = exec.Command("figlet", "-w", "100", data.IP)
					cmd.Stdout = f
					cmd.Run()
				} else {
					fmt.Fprintf(f, "************************************\n")
					fmt.Fprintf(f, "ID:   %s\n", data.ID)
					fmt.Fprintf(f, "Name: %s\n", data.Name)
					fmt.Fprintf(f, "IP:   %s\n", data.IP)
					fmt.Fprintf(f, "************************************\n")
				}
				f.Close()
			} else {
				log.Printf("[agent] failed to open tty6: %v", err)
			}

			duration := data.Duration
			if duration <= 0 {
				duration = 10
			}
			time.Sleep(time.Duration(duration) * time.Second)

			// Switch back
			exec.Command("chvt", currentVT).Run()
		}()
	}

	// Visual: Browser (works if logged in)
	if data.URL != "" {
		go func() {
			// If running as root, this is hard.
			// If running as user, this works.
			if os.Geteuid() != 0 {
				exec.Command("xdg-open", data.URL).Start()
				return
			}

			// Attempt to find a user session
			// This is a best-effort heuristic for Ubuntu/Gnome
			users, _ := exec.Command("users").Output()
			userList := strings.Fields(string(users))
			if len(userList) > 0 {
				user := userList[0] // Pick first user
				// Try to open browser as user
				// We need to guess DISPLAY. Usually :0 or :1
				cmd := exec.Command("su", "-", user, "-c", fmt.Sprintf("export DISPLAY=:0; xdg-open '%s'", data.URL))
				if err := cmd.Run(); err != nil {
					// Try :1
					exec.Command("su", "-", user, "-c", fmt.Sprintf("export DISPLAY=:1; xdg-open '%s'", data.URL)).Run()
				}
			}
		}()
	}

	return nil
}

func blinkPiLED(pattern string, duration int) {
	led0Path := "/sys/class/leds/led0/brightness" // Green
	led1Path := "/sys/class/leds/led1/brightness" // Red
	led0Trigger := "/sys/class/leds/led0/trigger"
	led1Trigger := "/sys/class/leds/led1/trigger"

	// Check if at least one LED exists
	_, err0 := os.Stat(led0Path)
	_, err1 := os.Stat(led1Path)
	if os.IsNotExist(err0) && os.IsNotExist(err1) {
		return
	}

	// Helper to get current trigger
	getTrigger := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			return "none"
		}
		// Format is "none [mmc0] heartbeat"
		s := string(data)
		start := strings.Index(s, "[")
		end := strings.Index(s, "]")
		if start != -1 && end != -1 && end > start {
			return s[start+1 : end]
		}
		return "none"
	}

	// Save current state
	origTrig0 := getTrigger(led0Trigger)
	origTrig1 := getTrigger(led1Trigger)

	go func() {
		log.Printf("[agent] blinking Pi LEDs with pattern %s for %ds (orig: %s, %s)", pattern, duration, origTrig0, origTrig1)

		// Default pattern if empty
		if pattern == "" {
			pattern = "g0g0g0g0g0"
		}
		if duration <= 0 {
			duration = 5
		}

		endTime := time.Now().Add(time.Duration(duration) * time.Second)

		for time.Now().Before(endTime) {
			for _, char := range pattern {
				if time.Now().After(endTime) {
					break
				}

				var gVal, rVal []byte
				switch char {
				case 'g':
					gVal, rVal = []byte("1"), []byte("0")
				case 'r':
					gVal, rVal = []byte("0"), []byte("1")
				case 'b':
					gVal, rVal = []byte("1"), []byte("1")
				default: // '0' or unknown
					gVal, rVal = []byte("0"), []byte("0")
				}

				_ = os.WriteFile(led0Path, gVal, 0644)
				_ = os.WriteFile(led1Path, rVal, 0644)

				time.Sleep(200 * time.Millisecond)
			}
		}

		// Restore triggers
		_ = os.WriteFile(led0Trigger, []byte(origTrig0), 0644)
		_ = os.WriteFile(led1Trigger, []byte(origTrig1), 0644)

		// If trigger was "none" or "input" (often default for red), ensure it's ON if it was likely ON (red usually is)
		// Actually, restoring trigger usually restores state.
		// But if it was "default-on", we should be good.
		// If it was "input", it might be off.
		// Let's just ensure Red is ON if it's the power LED (led1 usually) and trigger is input/none
		if origTrig1 == "input" || origTrig1 == "none" {
			_ = os.WriteFile(led1Path, []byte("1"), 0644)
		}
	}()
}

// snapshotGrabScript starts a rclpy node, waits for a single frame on
// /camera/image_raw/compressed, and writes it straight to disk. A
// CompressedImage message in jpeg format already *is* a JPEG byte stream, so
// no decoding/re-encoding (and no OpenCV/cv_bridge dependency) is needed.
const snapshotGrabScript = `
import sys, rclpy
from rclpy.node import Node
from sensor_msgs.msg import CompressedImage

class Grab(Node):
    def __init__(self, path):
        super().__init__('openrobotfleet_snapshot')
        self.path = path
        self.got = False
        self.create_subscription(CompressedImage, '/camera/image_raw/compressed', self.cb, 1)
    def cb(self, msg):
        with open(self.path, 'wb') as f:
            f.write(bytes(msg.data))
        self.got = True

rclpy.init()
node = Grab(sys.argv[1])
deadline = node.get_clock().now().nanoseconds + 20_000_000_000
while rclpy.ok() and not node.got and node.get_clock().now().nanoseconds < deadline:
    rclpy.spin_once(node, timeout_sec=1.0)
rclpy.shutdown()
sys.exit(0 if node.got else 1)
`

// cameraInstalledMarker records that HandleInstallCameraSupport has already
// built and installed libcamera/camera_ros on this robot.
const cameraInstalledMarker = "/var/lib/openrobotfleet/camera-installed"

// libcameraBuildScript builds and installs the Raspberry Pi libcamera fork
// from source, natively on the robot's own ARM64 CPU. This used to run
// inside the golden image's chroot build under qemu-aarch64 emulation, where
// ldconfig was observed intermittently segfaulting ("qemu: uncaught target
// signal 11") -- a qemu-user flakiness, not anything wrong with the build
// itself. Running it here, on real hardware with no emulation layer in the
// loop, sidesteps that failure mode entirely; the cost is that the first
// camera capture on a given robot takes several extra minutes while this
// runs once.
const libcameraBuildScript = `
set -e
export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y python3-pip python3-jinja2 python3-yaml python3-ply \
    libboost-dev libgnutls28-dev openssl libtiff-dev pybind11-dev \
    qtbase5-dev libqt5core5a libqt5widgets5 meson cmake \
    libglib2.0-dev libgstreamer-plugins-base1.0-dev ros-%s-camera-ros

# Jammy's apt meson (0.61) is too old for this libcamera (needs >= 0.63);
# pip's meson is newer and installs to /usr/local/bin, which takes PATH
# precedence over apt's /usr/bin/meson.
pip3 install --upgrade 'meson>=0.63'

rm -rf /tmp/libcamera
git clone -b v0.5.2 --depth 1 https://github.com/raspberrypi/libcamera.git /tmp/libcamera
cd /tmp/libcamera
meson setup build --buildtype=release -Dpipelines=rpi/vc4,rpi/pisp -Dipas=rpi/vc4,rpi/pisp -Dv4l2=true -Dgstreamer=enabled -Dtest=false -Dlc-compliance=disabled -Dcam=disabled -Dqcam=disabled -Ddocumentation=disabled -Dpycamera=enabled
ninja -C build
ninja -C build install
cd /
rm -rf /tmp/libcamera

echo "/usr/local/lib/$(dpkg-architecture -qDEB_HOST_MULTIARCH)" > /etc/ld.so.conf.d/openrobotfleet-libcamera.conf
ldconfig

mkdir -p "$(dirname %q)"
touch %q
`

// HandleInstallCameraSupport builds and installs libcamera + camera_ros from
// source, natively on the robot (see libcameraBuildScript), then leaves
// cameraInstalledMarker behind so HandleCaptureImage can tell it's present
// without re-running anything. This is a distinct, explicitly user-triggered
// action (surfaced from the Semester Wizard) rather than something
// HandleCaptureImage does automatically on demand: an unsuspecting user
// tapping "capture image" should never be the one who accidentally kicks off
// a from-source build that can take the better part of an hour. Runs with no
// artificial timeout for the same reason -- the JobManager already tracks
// this as a single long-running job and reports success/failure back to the
// controller the same way any other command does.
func HandleInstallCameraSupport(cfg Config) error {
	if _, err := os.Stat(cameraInstalledMarker); err == nil {
		log.Printf("[agent] camera support already installed")
		return nil
	}

	matches, err := filepath.Glob("/opt/ros/*/setup.bash")
	if err != nil || len(matches) == 0 {
		return fmt.Errorf("could not find a ROS install under /opt/ros to build camera_ros against")
	}
	rosDistro := filepath.Base(filepath.Dir(matches[0]))

	log.Printf("[agent] installing camera support: building camera_ros/libcamera from source (this can take several minutes)...")
	script := fmt.Sprintf(libcameraBuildScript, rosDistro, cameraInstalledMarker, cameraInstalledMarker)
	cmd := exec.Command("bash", "-c", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("camera stack build failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	log.Printf("[agent] camera_ros/libcamera build complete")
	return nil
}

// HandleCaptureImage takes a photo via ROS and uploads it. Nothing keeps a
// camera node running persistently (see the golden image's
// camera_params.example.yaml), so this starts a short-lived camera_ros node
// just for the duration of the capture and kills it afterward -- it can't
// collide with a scenario's own camera usage. camera_ros (libcamera-backed),
// not v4l2_camera, because the TB3 Pi camera is a raw Bayer CSI sensor:
// v4l2_camera only talks to the plain V4L2 video node and never configures
// the sensor's media-controller pad or routes frames through the ISP for
// demosaicing, so it can't stream at all, let alone produce a real color
// image. libcamera (via camera_ros) is what actually drives that pipeline.
func HandleCaptureImage(cfg Config, data CaptureImageData) error {
	if _, err := os.Stat(cameraInstalledMarker); err != nil {
		return errors.New("camera support isn't installed on this robot yet -- run \"Install Camera Support\" from the Semester Wizard first")
	}

	log.Printf("[agent] capturing image")
	tmpPath := "/tmp/snapshot.jpg"

	// format:=RGB888 is required: camera_ros's auto-selected default (NV21)
	// produces an empty compressed_image_transport payload (confirmed by
	// testing -- the topic publishes, but msg.data is zero-length), since
	// the JPEG encoder expects a standard RGB/BGR/mono encoding.
	//
	// Wrapped in `timeout` (not just our own deferred Kill/Wait below) so the
	// camera node is guaranteed to release the device even if the agent
	// process itself dies or gets restarted mid-capture -- observed in
	// practice during development: an agent restart orphaned a camera child
	// process, which then held /dev/video0 open indefinitely and blocked
	// every capture after it until something manually killed it.
	camCmd := exec.Command("timeout", "--kill-after=5s", "30s",
		"ros2", "run", "camera_ros", "camera_node", "--ros-args",
		"-p", "format:=RGB888", "-p", "width:=640", "-p", "height:=480")
	if err := camCmd.Start(); err != nil {
		return fmt.Errorf("failed to start camera node: %w", err)
	}
	defer func() {
		if camCmd.Process != nil {
			_ = camCmd.Process.Kill()
			_ = camCmd.Wait()
		}
	}()

	if out, err := runCmd(25*time.Second, "python3", "-c", snapshotGrabScript, tmpPath); err != nil {
		log.Printf("[agent] camera capture failed: %v: %s", err, string(out))
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

	// The controller is frequently reached through a self-signed fallback
	// cert (e.g. Traefik's "localhost" router); the browser's own TLS
	// exception doesn't extend to the agent's separate HTTP client, so
	// verification has to be skipped here or every upload fails with a
	// certificate error.
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[agent] image upload request failed: %v", err)
		return fmt.Errorf("upload failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[agent] image upload rejected: %s", resp.Status)
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

// HandleReboot reboots the system.
func HandleReboot(cfg Config) error {
	log.Printf("[agent] rebooting system...")
	// Sync filesystem before reboot
	exec.Command("sync").Run()

	// Try systemctl first (most modern linuxes)
	if err := exec.Command("sudo", "systemctl", "reboot").Run(); err == nil {
		return nil
	}

	// Fallback to reboot command
	if err := exec.Command("sudo", "reboot").Run(); err == nil {
		return nil
	}

	// Fallback to direct reboot (if running as root)
	cmd := exec.Command("reboot")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("reboot failed: %v: %s", err, string(out))
	}
	return nil
}

// factoryResetFlagPath is where the boot-time overlay wipe-check script
// (00-factory-reset-check, only present on golden images built with the
// overlay toggle enabled) looks for a pending reset request. Writing this
// file is harmless on any other image: nothing reads it, so it's just a
// stray file followed by an ordinary reboot.
const factoryResetFlagPath = "/boot/firmware/factory-reset-requested"

// HandleFactoryReset arms the wipe-on-next-boot flag and reboots. The actual
// wipe happens in initramfs before the overlay is mounted; this only ever
// requests it.
func HandleFactoryReset(cfg Config) error {
	log.Printf("[agent] factory reset requested, arming wipe-on-next-boot flag")
	if err := os.WriteFile(factoryResetFlagPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0644); err != nil {
		return fmt.Errorf("failed to arm factory reset flag: %w", err)
	}
	exec.Command("sync").Run()
	log.Printf("[agent] factory reset flag armed, rebooting to apply")
	return HandleReboot(cfg)
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
