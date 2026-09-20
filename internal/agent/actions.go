package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// defaultCmdTimeout bounds short administrative subprocesses, including ROS
// publishers that would otherwise wait indefinitely for a subscriber.
const defaultCmdTimeout = 15 * time.Second

// runCmd bounds short subprocesses so a hung command cannot stall the queue.
func runCmd(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("timed out after %s", timeout)
	}
	return out, err
}

// HandleSetHostname updates only the OS hostname -- it never touches the
// agent's permanent identity, so unlike the legacy configure_agent command
// it needs no config rewrite or service restart.
func HandleSetHostname(data SetHostnameData) error {
	if data.Hostname == "" {
		return errors.New("hostname required")
	}
	if err := SetHostname(data.Hostname); err != nil {
		return fmt.Errorf("set hostname: %w", err)
	}
	log.Printf("[agent] set hostname to %s", data.Hostname)
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

// HandleConfigureNetwork writes the DDS/ROS networking env file for this
// device's group assignment and, for Cyclone, a config pinning the network
// interface plus any static peers. It makes sure interactive shells load that
// env (so a student's ros2/rviz2 on a laptop matches the robot, not just the
// agent's own commands), installs the RMW package if it's missing, then
// restarts ROS where there's a ROS service to restart.
func HandleConfigureNetwork(cfg Config, data ConfigureNetworkData) error {
	if data.ROSDomainID < 0 || data.ROSDomainID > 232 {
		return errors.New("invalid ROS domain ID")
	}
	if data.RMWImplementation != "" && !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(data.RMWImplementation) {
		return errors.New("invalid RMW implementation")
	}
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

	if rmw == "rmw_cyclonedds_cpp" {
		if err := ensureCycloneInstalled(); err != nil {
			return err
		}
		if err := os.WriteFile(cycloneDDSPath, []byte(cycloneConfig(data.StaticPeers)), 0o644); err != nil {
			return fmt.Errorf("write cyclonedds config: %w", err)
		}
		fmt.Fprintf(&env, "export CYCLONEDDS_URI=file://%s\n", cycloneDDSPath)
	} else if err := os.Remove(cycloneDDSPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale cyclonedds config: %w", err)
	}

	if err := writeAtomicFile(rosEnvPath, []byte(env.String()), 0o644); err != nil {
		return fmt.Errorf("write ros env: %w", err)
	}
	if err := ensureShellsSourceROSEnv(); err != nil {
		log.Printf("[agent] warning: %v", err)
	}

	log.Printf("[agent] configured network: domain=%d rmw=%s static_peers=%d", data.ROSDomainID, rmw, len(data.StaticPeers))
	if os.Getenv("ROS_RESTART_CMD") == "" {
		if _, err := runCmd(defaultCmdTimeout, "systemctl", "cat", rosServiceName()+".service"); err != nil {
			log.Printf("[agent] no %s service on this device, skipping ROS restart", rosServiceName())
			return nil
		}
	}
	return HandleRestartROS(cfg)
}

// cycloneConfig pins Cyclone to the interface that routes to the group's
// peer (or the default route): Cyclone binds to a single interface of its own
// choosing, which on a laptop with Docker, VM or VPN bridges is often the
// wrong one, and then nothing is discovered. Static peers, when given, are
// listed for unicast discovery. Avoid the Peers AddLocalhost attribute:
// Cyclone 0.10.x (ROS 2 Humble) rejects it and every node fails to create a
// domain; an explicit localhost peer works on all versions. The auto
// participant index cap is raised so unicast discovery still reaches ports
// beyond the default ~9 nodes.
func cycloneConfig(staticPeers []string) string {
	routeTarget := "1.1.1.1"
	if len(staticPeers) > 0 {
		routeTarget = staticPeers[0]
	}

	var xml strings.Builder
	xml.WriteString("<CycloneDDS><Domain>\n")
	if dev := routeInterface(routeTarget); dev != "" {
		fmt.Fprintf(&xml, "<General><Interfaces><NetworkInterface name=\"%s\"/></Interfaces></General>\n", dev)
	}
	if len(staticPeers) > 0 {
		xml.WriteString("<Discovery>\n")
		xml.WriteString("<ParticipantIndex>auto</ParticipantIndex>\n")
		xml.WriteString("<MaxAutoParticipantIndex>100</MaxAutoParticipantIndex>\n")
		xml.WriteString("<Peers>\n")
		xml.WriteString("  <Peer Address=\"localhost\"/>\n")
		for _, peer := range staticPeers {
			fmt.Fprintf(&xml, "  <Peer Address=\"%s\"/>\n", peer)
		}
		xml.WriteString("</Peers></Discovery>\n")
	}
	xml.WriteString("</Domain></CycloneDDS>\n")
	return xml.String()
}

// routeInterface returns the network interface the kernel would use to reach
// target, or "" if it can't tell (Cyclone then picks one itself).
func routeInterface(target string) string {
	out, err := runCmd(defaultCmdTimeout, "ip", "route", "get", target)
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(out))
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "dev" {
			return fields[i+1]
		}
	}
	return ""
}

// ensureCycloneInstalled installs rmw_cyclonedds_cpp when a ROS install lacks
// it. Golden-imaged robots ship with it, but an SSH-enrolled laptop may not,
// and exporting RMW_IMPLEMENTATION for a missing RMW makes every ros2 command
// fail outright. A device with no ROS under /opt/ros is left alone.
func ensureCycloneInstalled() error {
	if libs, _ := filepath.Glob("/opt/ros/*/lib/librmw_cyclonedds_cpp.so"); len(libs) > 0 {
		return nil
	}
	setups, _ := filepath.Glob("/opt/ros/*/setup.bash")
	if len(setups) == 0 {
		return nil
	}
	distro := filepath.Base(filepath.Dir(setups[0]))
	pkg := fmt.Sprintf("ros-%s-rmw-cyclonedds-cpp", distro)
	log.Printf("[agent] installing %s", pkg)
	// Update first: package lists are often stale or (on golden images) wiped.
	if out, err := runCmd(10*time.Minute, "bash", "-c", "DEBIAN_FRONTEND=noninteractive apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y "+pkg); err != nil {
		return fmt.Errorf("install %s: %w: %s", pkg, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// bashRCPath is read by every interactive bash on Debian/Ubuntu, login or
// not, so a new terminal picks up a group's domain change immediately --
// unlike /etc/profile.d, which only applies at the next desktop login.
const bashRCPath = "/etc/bash.bashrc"

// ensureShellsSourceROSEnv hooks ros_env.sh into bashRCPath once, so the
// laptop's users run ros2/rviz2 on the same domain and RMW as the robot.
func ensureShellsSourceROSEnv() error {
	existing, err := os.ReadFile(bashRCPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", bashRCPath, err)
	}
	if strings.Contains(string(existing), rosEnvPath) {
		return nil
	}
	f, err := os.OpenFile(bashRCPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", bashRCPath, err)
	}
	defer f.Close()
	hook := fmt.Sprintf("\n# Added by openrobotfleet-agent: ROS domain/RMW for this device's group.\n[ -f %[1]s ] && . %[1]s\n", rosEnvPath)
	if _, err := f.WriteString(hook); err != nil {
		return fmt.Errorf("append to %s: %w", bashRCPath, err)
	}
	log.Printf("[agent] %s now sources %s", bashRCPath, rosEnvPath)
	return nil
}

// turtlebot3Models are the values TURTLEBOT3_MODEL accepts; anything else
// makes turtlebot3_bringup fail to find its URDF/params.
var turtlebot3Models = map[string]bool{"burger": true, "waffle": true, "waffle_pi": true}

const defaultTurtleBot3Model = "waffle_pi"

// HandleResetBashrc restores the workspace user's ~/.bashrc from
// /etc/skel/.bashrc (what `cp /etc/skel/.bashrc ~/` does by hand), then
// appends the ROS setup source line and TURTLEBOT3_MODEL export students
// need. The previous file is kept as ~/.bashrc.bak.<unix time>. The group's
// ROS domain comes from /etc/bash.bashrc (ensureShellsSourceROSEnv), so it
// survives the reset untouched.
func HandleResetBashrc(cfg Config, data ResetBashrcData) error {
	model := strings.TrimSpace(data.TurtleBot3Model)
	if model == "" {
		model = defaultTurtleBot3Model
	}
	if !turtlebot3Models[model] {
		return fmt.Errorf("unknown TurtleBot3 model %q (want burger, waffle or waffle_pi)", model)
	}

	name := workspaceUsername(cfg)
	if name == "" {
		return errors.New("could not determine the workspace user whose .bashrc to reset")
	}
	u, err := user.Lookup(name)
	if err != nil {
		return fmt.Errorf("look up user %s: %w", name, err)
	}

	skel, err := os.ReadFile("/etc/skel/.bashrc")
	if err != nil {
		return fmt.Errorf("read default bashrc: %w", err)
	}
	var b strings.Builder
	b.Write(skel)
	b.WriteString("\n# Added by OpenRobotFleet\n")
	if matches, _ := filepath.Glob("/opt/ros/*/setup.bash"); len(matches) > 0 {
		fmt.Fprintf(&b, "source %s\n", matches[0])
	} else {
		log.Printf("[agent] no ROS install under /opt/ros; .bashrc won't source a ROS setup script")
	}
	fmt.Fprintf(&b, "export TURTLEBOT3_MODEL=%s\n", model)

	path := filepath.Join(u.HomeDir, ".bashrc")
	if _, err := os.Stat(path); err == nil {
		backup := fmt.Sprintf("%s.bak.%d", path, time.Now().Unix())
		if err := os.Rename(path, backup); err != nil {
			return fmt.Errorf("back up %s: %w", path, err)
		}
		log.Printf("[agent] backed up %s to %s", path, backup)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown %s: %w", path, err)
	}
	log.Printf("[agent] reset %s to default (TURTLEBOT3_MODEL=%s)", path, model)
	return nil
}

// aptPackagePattern matches a Debian package name, optionally with an
// :arch qualifier and/or =version pin (e.g. ros-humble-image-transport,
// libfoo:arm64, vim=2:8.2.3995-1ubuntu2). It never matches a leading '-', so
// a "package" can't smuggle an option into apt-get.
var aptPackagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+(:[a-z0-9-]+)?(=[A-Za-z0-9.+~:-]+)?$`)

// ValidatePackageNames rejects anything that isn't a plain apt package name.
func ValidatePackageNames(pkgs []string) error {
	for _, p := range pkgs {
		if !aptPackagePattern.MatchString(p) {
			return fmt.Errorf("invalid package name %q", p)
		}
	}
	return nil
}

// systemUpdateTimeout bounds each apt-get step. A full upgrade on a Pi over
// lab wifi can legitimately take a long time, but apt must never be allowed
// to hang forever and jam the agent's single job slot (see defaultCmdTimeout).
const systemUpdateTimeout = 60 * time.Minute

// aptGet runs apt-get non-interactively. It waits up to 5 minutes for the
// dpkg lock (unattended-upgrades often holds it just after boot) and keeps
// existing config files rather than prompting when a package ships a new one.
func aptGet(args ...string) error {
	full := append([]string{
		"-o", "DPkg::Lock::Timeout=300",
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
	}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), systemUpdateTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "apt-get", full...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("timed out after %s", systemUpdateTimeout)
	}
	if err != nil {
		// apt's output can run to thousands of lines; the error is at the end.
		msg := strings.TrimSpace(string(out))
		if len(msg) > 2000 {
			msg = "..." + msg[len(msg)-2000:]
		}
		return fmt.Errorf("apt-get %s: %w: %s", args[0], err, msg)
	}
	return nil
}

// HandleSystemUpdate is `apt update`, followed by `apt upgrade -y` and/or
// `apt install -y <packages>` as requested.
func HandleSystemUpdate(data SystemUpdateData) error {
	if err := ValidatePackageNames(data.Packages); err != nil {
		return err
	}
	log.Printf("[agent] system update: refreshing package lists")
	if err := aptGet("update"); err != nil {
		return err
	}
	if data.Upgrade {
		log.Printf("[agent] system update: upgrading installed packages")
		if err := aptGet("upgrade", "-y"); err != nil {
			return err
		}
	}
	if len(data.Packages) > 0 {
		log.Printf("[agent] system update: installing %s", strings.Join(data.Packages, " "))
		if err := aptGet(append([]string{"install", "-y"}, data.Packages...)...); err != nil {
			return err
		}
	}
	log.Printf("[agent] system update complete")
	return nil
}

const (
	networkWaitPath   = "/usr/local/bin/openrobotfleet-wait-network"
	networkWaitScript = `#!/bin/bash
# Installed by openrobotfleet-agent. Blocks ROS startup until the network
# interface Cyclone DDS is pinned to has an IPv4 address: at boot, Wi-Fi comes
# up after network.target, and Cyclone can't create a domain on an interface
# with no address -- every node dies with "rmw handle is invalid".
cfg=/etc/openrobotfleet-agent/cyclonedds.xml
dev=$(sed -n 's/.*NetworkInterface name="\([^"]*\)".*/\1/p' "$cfg" 2>/dev/null | head -n1)
[ -n "$dev" ] || exit 0
for _ in $(seq 60); do
  ip -4 -o addr show dev "$dev" 2>/dev/null | grep -q inet && exit 0
  sleep 1
done
echo "openrobotfleet: $dev still has no IPv4 address after 60s, starting anyway" >&2
exit 0
`
	rosOverrideDir = "/etc/systemd/system/"
	rosOverride    = `# Installed by openrobotfleet-agent.
[Unit]
Wants=network-online.target
After=network-online.target

[Service]
ExecStartPre=` + networkWaitPath + `
# ros2 launch exits 0 even when every node crashed, so on-failure never
# retries; restart whenever it exits (a manual systemctl stop still sticks).
Restart=always
RestartSec=10
`
)

// EnsureROSServiceOverride installs a systemd drop-in for the ROS bringup
// service so it waits for the pinned network interface and restarts whenever
// it exits. A drop-in rather than editing the unit, so it applies to robots
// imaged before this existed. No-op where there's no ROS service (laptops).
func EnsureROSServiceOverride() error {
	unit := rosServiceName() + ".service"
	if _, err := runCmd(defaultCmdTimeout, "systemctl", "cat", unit); err != nil {
		return nil
	}
	if err := os.WriteFile(networkWaitPath, []byte(networkWaitScript), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", networkWaitPath, err)
	}

	dropInDir := filepath.Join(rosOverrideDir, unit+".d")
	dropInPath := filepath.Join(dropInDir, "openrobotfleet.conf")
	if existing, _ := os.ReadFile(dropInPath); string(existing) == rosOverride {
		return nil
	}
	if err := os.MkdirAll(dropInDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dropInDir, err)
	}
	if err := os.WriteFile(dropInPath, []byte(rosOverride), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dropInPath, err)
	}
	if out, err := runCmd(defaultCmdTimeout, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	log.Printf("[agent] installed %s", dropInPath)

	// A bringup that already died before this override existed won't be
	// retried by it, so start an enabled-but-dead service once now.
	if _, err := runCmd(defaultCmdTimeout, "systemctl", "is-enabled", "--quiet", unit); err == nil {
		if _, err := runCmd(defaultCmdTimeout, "systemctl", "is-active", "--quiet", unit); err != nil {
			if out, err := runCmd(2*time.Minute, "systemctl", "start", unit); err != nil {
				return fmt.Errorf("start %s: %w: %s", unit, err, strings.TrimSpace(string(out)))
			}
			log.Printf("[agent] started %s", unit)
		}
	}
	return nil
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
	return HandleTestDriveContext(context.Background(), cfg, data)
}

func HandleTestDriveContext(ctx context.Context, cfg Config, data TestDriveData) (err error) {
	if data.DurationSec == 0 {
		data.DurationSec = 2
	}
	if data.DurationSec < 0 || data.DurationSec > 30 {
		return errors.New("test drive duration must be between 1 and 30 seconds")
	}
	// Always publish a final zero, including after cancellation or a partial
	// failure of the initial publisher. Use an independent bounded context.
	defer func() { err = errors.Join(err, HandleStop(cfg)) }()
	if out, err := runROSCmdContext(ctx, defaultCmdTimeout, "ros2", "topic", "pub", "--once", "-w", "0", "/cmd_vel", "geometry_msgs/msg/Twist", "{linear: {x: 0.1, y: 0.0, z: 0.0}, angular: {x: 0.0, y: 0.0, z: 0.0}}"); err != nil {
		return fmt.Errorf("forward failed: %w: %s", err, out)
	}
	timer := time.NewTimer(time.Duration(data.DurationSec) * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// HandleStop publishes zero velocity.
func HandleStop(cfg Config) error {
	log.Printf("[agent] stopping robot")
	if out, err := runROSCmd(defaultCmdTimeout, "ros2", "topic", "pub", "--once", "-w", "0", "/cmd_vel", "geometry_msgs/msg/Twist", "{linear: {x: 0.0, y: 0.0, z: 0.0}, angular: {x: 0.0, y: 0.0, z: 0.0}}"); err != nil {
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
	if _, err := runROSCmd(defaultCmdTimeout, "ros2", "service", "call", "/sound", "turtlebot3_msgs/srv/Sound", "value: 1"); err == nil {
		return nil
	}

	// Fall back to the iRobot Create 3 (TurtleBot4) audio/lightring topics.
	if out, err := runROSCmd(defaultCmdTimeout, "ros2", "topic", "pub", "--once", "-w", "0", "/cmd_audio", "irobot_create_msgs/msg/AudioNoteVector",
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
	if out, err := runROSCmd(defaultCmdTimeout, "ros2", "topic", "pub", "--once", "-w", "0", "/cmd_lightring", "irobot_create_msgs/msg/LightringLeds",
		`{override_system: true, leds: [{red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}, {red: 255, green: 0, blue: 0}]}`); err != nil {
		log.Printf("[agent] failed to set LEDs red: %v: %s", err, string(out))
	}

	time.Sleep(1 * time.Second)

	// Off (or return to system control)
	// To return to system control, we can set override_system to false.
	if out, err := runROSCmd(defaultCmdTimeout, "ros2", "topic", "pub", "--once", "-w", "0", "/cmd_lightring", "irobot_create_msgs/msg/LightringLeds",
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

// cameraConfigPaths are checked in order for the Pi firmware config file --
// Bookworm-based images (and Ubuntu's Pi images) use the first path; older
// Raspberry Pi OS releases use the second.
var cameraConfigPaths = []string{"/boot/firmware/config.txt", "/boot/config.txt"}

// ensureCameraAutoDetect makes sure libcamera's device auto-detection is
// enabled in the Pi's firmware config. Some base images ship with this
// explicitly disabled in favor of the legacy bcm2835-v4l2 stack, which
// silently breaks camera_ros: libcamera enumerates zero cameras and
// camera_node aborts with "no cameras available", no matter how cleanly
// camera_ros itself was built -- confirmed on hardware where the build had
// already completed successfully but every capture still failed. Checked on
// every install_camera_support run, not just the first, so a robot whose
// software stack built fine but had this firmware setting wrong still gets
// fixed. Returns whether it changed anything, since that only takes effect
// after a reboot.
func ensureCameraAutoDetect() (bool, error) {
	var path string
	for _, p := range cameraConfigPaths {
		if _, err := os.Stat(p); err == nil {
			path = p
			break
		}
	}
	if path == "" {
		return false, fmt.Errorf("no firmware config found at %s", strings.Join(cameraConfigPaths, " or "))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	lines := strings.Split(string(data), "\n")
	found := false
	changed := false
	for i, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "camera_auto_detect=") {
			found = true
			if strings.TrimSpace(line) != "camera_auto_detect=1" {
				lines[i] = "camera_auto_detect=1"
				changed = true
			}
		}
	}
	if !found {
		lines = append(lines, "camera_auto_detect=1")
		changed = true
	}
	if !changed {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	log.Printf("[agent] enabled camera_auto_detect in %s (was disabled or missing)", path)
	return true, nil
}

// cameraUdevRulesPath and cameraUdevRules hand the camera device nodes to the
// video group. The agent runs as root so its own captures never notice, but on
// Ubuntu's Pi images /dev/media* (and /dev/dma_heap/*, which libcamera also
// opens) come up root:root, so a user running camera_ros or v4l2_camera by
// hand gets "permission denied" even when they're in the video group.
const (
	cameraUdevRulesPath = "/etc/udev/rules.d/99-openrobotfleet-camera.rules"
	cameraUdevRules     = `SUBSYSTEM=="media", GROUP="video", MODE="0660"
SUBSYSTEM=="video4linux", GROUP="video", MODE="0660"
SUBSYSTEM=="dma_heap", GROUP="video", MODE="0660"
`
)

// ensureCameraPermissions installs cameraUdevRules (re-triggering udev so the
// existing nodes pick them up without a reboot) and adds the workspace owner
// to the video group. Both steps are idempotent. A group change only reaches
// new login sessions, so an already-open SSH shell still needs to reconnect.
func ensureCameraPermissions(cfg Config) error {
	existing, _ := os.ReadFile(cameraUdevRulesPath)
	if string(existing) != cameraUdevRules {
		if err := os.WriteFile(cameraUdevRulesPath, []byte(cameraUdevRules), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", cameraUdevRulesPath, err)
		}
		if out, err := runCmd(defaultCmdTimeout, "udevadm", "control", "--reload-rules"); err != nil {
			return fmt.Errorf("reload udev rules: %w: %s", err, strings.TrimSpace(string(out)))
		}
		for _, subsystem := range []string{"media", "video4linux", "dma_heap"} {
			if out, err := runCmd(defaultCmdTimeout, "udevadm", "trigger", "--action=change", "--subsystem-match="+subsystem); err != nil {
				return fmt.Errorf("trigger udev for %s: %w: %s", subsystem, err, strings.TrimSpace(string(out)))
			}
		}
		log.Printf("[agent] installed camera udev rules at %s", cameraUdevRulesPath)
	}

	username := workspaceUsername(cfg)
	if username == "" {
		return errors.New("could not determine the workspace user to add to the video group")
	}
	u, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("look up user %s: %w", username, err)
	}
	video, err := user.LookupGroup("video")
	if err != nil {
		return fmt.Errorf("look up video group: %w", err)
	}
	if gids, err := u.GroupIds(); err == nil && slices.Contains(gids, video.Gid) {
		return nil
	}
	if out, err := runCmd(defaultCmdTimeout, "usermod", "-aG", "video", username); err != nil {
		return fmt.Errorf("add %s to video group: %w: %s", username, err, strings.TrimSpace(string(out)))
	}
	log.Printf("[agent] added %s to the video group", username)
	return nil
}

// cameraFrameID is the TurtleBot3 Waffle Pi URDF's optical frame for the Pi
// camera. camera_ros defaults to frame_id "camera", which isn't in the TF
// tree, so RViz's Camera display can't place the image without this.
const cameraFrameID = "camera_rgb_optical_frame"

// sensorHFOVDegrees is the full-sensor horizontal field of view of each Pi CSI
// camera we know how to approximate intrinsics for. It doubles as the filter
// for which i2c devices detectCSICameras treats as cameras.
var sensorHFOVDegrees = map[string]float64{
	"imx219": 62.2, // Pi Camera v2
	"ov5647": 53.5, // Pi Camera v1
}

// cameraCalibrationResolutions are the 4:3 modes an approximate calibration
// file is written for. camera_ros looks up a separate file per resolution.
var cameraCalibrationResolutions = [][2]int{
	{320, 240}, {640, 480}, {800, 600}, {1024, 768}, {1280, 960}, {1640, 1232},
}

type csiCamera struct {
	model string
	// name is the calibration name camera_ros derives from libcamera's
	// camera: model + "_" + the device tree path with every non-alphanumeric
	// character replaced by "_", e.g. imx219__base_soc_i2c0mux_i2c_1_imx219_10.
	name string
}

// detectCSICameras finds CSI sensors by their device tree nodes, the same
// path libcamera uses as the camera id. Reading sysfs rather than starting
// camera_ros to see what it logs means this never holds the camera open.
func detectCSICameras() []csiCamera {
	nodes, _ := filepath.Glob("/sys/bus/i2c/devices/*/of_node")
	var cams []csiCamera
	for _, node := range nodes {
		target, err := filepath.EvalSymlinks(node)
		if err != nil {
			continue
		}
		id, ok := strings.CutPrefix(target, "/sys/firmware/devicetree")
		if !ok {
			continue
		}
		model, _, _ := strings.Cut(filepath.Base(id), "@")
		if _, known := sensorHFOVDegrees[model]; !known {
			continue
		}
		sanitized := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				return r
			}
			return '_'
		}, id)
		cams = append(cams, csiCamera{model: model, name: model + "_" + sanitized})
	}
	return cams
}

// approximateCalibrationYAML returns a pinhole calibration with no distortion
// derived from the sensor's field of view: accurate enough for RViz's Camera
// display to overlay the scene, not for anything metric.
func approximateCalibrationYAML(name string, hfovDeg float64, width, height int) string {
	f := float64(width) / 2 / math.Tan(hfovDeg/2*math.Pi/180)
	cx, cy := float64(width)/2, float64(height)/2
	return fmt.Sprintf(`# Approximate calibration written by openrobotfleet-agent from the sensor's
# field of view. Replace it by running camera_calibration's cameracalibrator.
image_width: %[2]d
image_height: %[3]d
camera_name: %[1]s
camera_matrix:
  rows: 3
  cols: 3
  data: [%.2[4]f, 0.0, %.1[5]f, 0.0, %.2[4]f, %.1[6]f, 0.0, 0.0, 1.0]
distortion_model: plumb_bob
distortion_coefficients:
  rows: 1
  cols: 5
  data: [0.0, 0.0, 0.0, 0.0, 0.0]
rectification_matrix:
  rows: 3
  cols: 3
  data: [1.0, 0.0, 0.0, 0.0, 1.0, 0.0, 0.0, 0.0, 1.0]
projection_matrix:
  rows: 3
  cols: 4
  data: [%.2[4]f, 0.0, %.1[5]f, 0.0, 0.0, %.2[4]f, %.1[6]f, 0.0, 0.0, 0.0, 1.0, 0.0]
`, name, width, height, f, cx, cy)
}

// cameraParamsFile is cameraROSParams' name in the workspace user's home.
const cameraParamsFile = "camera_ros.yaml"

// cameraROSParams is written to the workspace user's home so a manual run is
// just `ros2 run camera_ros camera_node --ros-args --params-file
// ~/camera_ros.yaml`. RGB888 because camera_ros's default NV21 isn't
// displayable in RViz and yields an empty compressed image.
const cameraROSParams = `/**:
  ros__parameters:
    format: RGB888
    width: 320
    height: 240
    frame_id: ` + cameraFrameID + `
`

// EnsureCameraSetup writes approximate calibration files for each detected
// CSI camera into the workspace user's ~/.ros/camera_info (the default
// location camera_ros reads) and ~/camera_ros.yaml, then installs the
// ros-camera service that streams with those settings. Existing files
// are never overwritten, so a real cameracalibrator result survives. It's a
// no-op until camera support is installed, and cheap enough to run at every
// agent start -- which also covers the reboot HandleInstallCameraSupport
// triggers after enabling camera_auto_detect, before which no sensor shows up.
func EnsureCameraSetup(cfg Config) error {
	if _, err := os.Stat(cameraInstalledMarker); err != nil {
		return nil
	}
	cams := detectCSICameras()
	if len(cams) == 0 {
		return nil
	}

	username := workspaceUsername(cfg)
	if username == "" {
		return errors.New("could not determine the workspace user for camera calibration files")
	}
	u, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("look up user %s: %w", username, err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	writeOwned := func(path, content string) error {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		log.Printf("[agent] wrote %s", path)
		return os.Chown(path, uid, gid)
	}

	rosDir := filepath.Join(u.HomeDir, ".ros")
	infoDir := filepath.Join(rosDir, "camera_info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", infoDir, err)
	}
	for _, dir := range []string{rosDir, infoDir} {
		if err := os.Chown(dir, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", dir, err)
		}
	}

	for _, cam := range cams {
		for _, res := range cameraCalibrationResolutions {
			path := filepath.Join(infoDir, fmt.Sprintf("%s_%dx%d.yaml", cam.name, res[0], res[1]))
			content := approximateCalibrationYAML(filepath.Base(strings.TrimSuffix(path, ".yaml")), sensorHFOVDegrees[cam.model], res[0], res[1])
			if err := writeOwned(path, content); err != nil {
				return err
			}
		}
	}
	paramsPath := filepath.Join(u.HomeDir, cameraParamsFile)
	if err := writeOwned(paramsPath, cameraROSParams); err != nil {
		return err
	}
	return ensureCameraService(u, paramsPath)
}

const (
	cameraServiceName = "ros-camera"
	cameraServicePath = "/etc/systemd/system/" + cameraServiceName + ".service"
	cameraStartPath   = "/usr/local/bin/openrobotfleet-camera-start"
	cameraStartScript = `#!/bin/bash
for setup in /opt/ros/*/setup.bash; do
  [ -f "$setup" ] && source "$setup" && break
done
[ -f /etc/openrobotfleet-agent/ros_env.sh ] && source /etc/openrobotfleet-agent/ros_env.sh
exec ros2 run camera_ros camera_node --ros-args --params-file "$1"
`
)

// cameraServiceUnit ties the camera to the bringup service: PartOf means
// stopping or restarting ROS (including the dashboard's Restart ROS, which
// also picks up a new ROS_DOMAIN_ID) takes the camera with it, and
// WantedBy means starting ROS starts the camera.
func cameraServiceUnit(u *user.User, paramsPath string) string {
	ros := rosServiceName()
	return fmt.Sprintf(`[Unit]
Description=OpenRobot camera (camera_ros)
PartOf=%[1]s.service
After=%[1]s.service

[Service]
Type=simple
User=%[2]s
Environment=HOME=%[3]s
ExecStart=%[4]s %[5]s
Restart=on-failure
RestartSec=10

[Install]
WantedBy=%[1]s.service
`, ros, u.Username, u.HomeDir, cameraStartPath, paramsPath)
}

// ensureCameraService installs the ros-camera systemd unit so the camera
// streams whenever bringup runs. It's only enabled when the agent first
// creates the unit: someone who later runs `systemctl disable ros-camera` to
// launch the camera from their own scenario instead stays disabled across
// agent restarts.
func ensureCameraService(u *user.User, paramsPath string) error {
	_, statErr := os.Stat(cameraServicePath)
	firstInstall := errors.Is(statErr, os.ErrNotExist)

	if err := os.WriteFile(cameraStartPath, []byte(cameraStartScript), 0o755); err != nil {
		return fmt.Errorf("write %s: %w", cameraStartPath, err)
	}
	unit := cameraServiceUnit(u, paramsPath)
	existing, _ := os.ReadFile(cameraServicePath)
	if string(existing) == unit {
		return nil
	}
	if err := os.WriteFile(cameraServicePath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", cameraServicePath, err)
	}
	if out, err := runCmd(defaultCmdTimeout, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if !firstInstall {
		log.Printf("[agent] updated %s", cameraServicePath)
		return nil
	}
	if out, err := runCmd(defaultCmdTimeout, "systemctl", "enable", "--now", cameraServiceName); err != nil {
		return fmt.Errorf("enable %s: %w: %s", cameraServiceName, err, strings.TrimSpace(string(out)))
	}
	log.Printf("[agent] installed and started %s", cameraServiceName)
	return nil
}

// HandleCameraService starts or stops the ros-camera service from the
// dashboard. Stopping doesn't disable it, so the camera comes back the next
// time ROS starts.
func HandleCameraService(start bool) error {
	if _, err := os.Stat(cameraServicePath); err != nil {
		return errors.New("camera service isn't installed on this robot -- run \"Install Camera Support\" from the Semester Wizard first")
	}
	verb := "stop"
	if start {
		verb = "start"
	}
	if out, err := runCmd(defaultCmdTimeout, "systemctl", verb, cameraServiceName); err != nil {
		return fmt.Errorf("%s %s: %w: %s", verb, cameraServiceName, err, strings.TrimSpace(string(out)))
	}
	log.Printf("[agent] camera service %s", verb)
	return nil
}

// cameraParamsPath returns ~/camera_ros.yaml for the workspace user.
func cameraParamsPath(cfg Config) (string, error) {
	username := workspaceUsername(cfg)
	if username == "" {
		return "", errors.New("could not determine the workspace user")
	}
	u, err := user.Lookup(username)
	if err != nil {
		return "", fmt.Errorf("look up user %s: %w", username, err)
	}
	return filepath.Join(u.HomeDir, cameraParamsFile), nil
}

// cameraParamLine matches a top-level-of-ros__parameters "width:" or
// "height:" entry, capturing its indentation so a rewrite keeps the YAML
// structure (and any other edits the user made to the file) intact.
var cameraParamLine = regexp.MustCompile(`^(\s*)(width|height):\s*(\d+)\s*$`)

// cameraResolution reads the stream resolution from ~/camera_ros.yaml.
func cameraResolution(cfg Config) (int, int, error) {
	path, err := cameraParamsPath(cfg)
	if err != nil {
		return 0, 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	var w, h int
	for _, line := range strings.Split(string(data), "\n") {
		if m := cameraParamLine.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[3])
			if m[2] == "width" {
				w = n
			} else {
				h = n
			}
		}
	}
	if w == 0 || h == 0 {
		return 0, 0, fmt.Errorf("no width/height in %s", path)
	}
	return w, h, nil
}

// setCameraResolutionYAML replaces the width/height in a camera_ros params
// file, adding either under ros__parameters if it's missing.
func setCameraResolutionYAML(content string, width, height int) (string, error) {
	lines := strings.Split(content, "\n")
	seen := map[string]bool{}
	paramsIdx, paramsIndent := -1, ""
	for i, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed == "ros__parameters:" {
			paramsIdx = i
			paramsIndent = line[:len(line)-len(strings.TrimLeft(line, " "))] + "  "
			continue
		}
		m := cameraParamLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		v := width
		if m[2] == "height" {
			v = height
		}
		lines[i] = fmt.Sprintf("%s%s: %d", m[1], m[2], v)
		seen[m[2]] = true
	}
	var missing []string
	for _, key := range []string{"width", "height"} {
		if seen[key] {
			continue
		}
		if paramsIdx < 0 {
			return "", fmt.Errorf("no ros__parameters section to add %s to", key)
		}
		v := width
		if key == "height" {
			v = height
		}
		missing = append(missing, fmt.Sprintf("%s%s: %d", paramsIndent, key, v))
	}
	if len(missing) > 0 {
		lines = slices.Insert(lines, paramsIdx+1, missing...)
	}
	return strings.Join(lines, "\n"), nil
}

// HandleCameraResolution rewrites the width/height in ~/camera_ros.yaml and
// restarts ros-camera if it's streaming so the change takes effect. Only the
// modes in cameraCalibrationResolutions are accepted, since those are the
// ones EnsureCameraSetup wrote camera_info files for -- any other size would
// stream without intrinsics and break RViz's Camera display.
func HandleCameraResolution(cfg Config, data CameraResolutionData) error {
	if !slices.Contains(cameraCalibrationResolutions, [2]int{data.Width, data.Height}) {
		return fmt.Errorf("unsupported camera resolution %dx%d", data.Width, data.Height)
	}
	path, err := cameraParamsPath(cfg)
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("camera isn't set up on this robot -- run \"Install Camera Support\" from the Semester Wizard first (%w)", err)
	}

	updated, err := setCameraResolutionYAML(string(existing), data.Width, data.Height)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	// WriteFile on an existing file keeps its owner, so the workspace user
	// can still edit it by hand.
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	log.Printf("[agent] camera resolution set to %dx%d in %s", data.Width, data.Height, path)

	if cameraServiceActive() {
		if out, err := runCmd(defaultCmdTimeout, "systemctl", "restart", cameraServiceName); err != nil {
			return fmt.Errorf("restart %s: %w: %s", cameraServiceName, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// CameraServiceState is reported in the heartbeat so the dashboard can show
// the right start/stop button: "active" or "inactive", or "" when the
// service isn't installed (a laptop, or a robot without camera support).
func CameraServiceState() string {
	if _, err := os.Stat(cameraServicePath); err != nil {
		return ""
	}
	if cameraServiceActive() {
		return "active"
	}
	return "inactive"
}

// cameraServiceActive reports whether ros-camera is currently streaming.
func cameraServiceActive() bool {
	_, err := runCmd(defaultCmdTimeout, "systemctl", "is-active", "--quiet", cameraServiceName)
	return err == nil
}

// workspaceUsername resolves the login name of the robot's workspace user.
// cfg.WorkspaceOwner may be a name, "name:group", or "uid:gid" (the form
// detectOwnerFromPath produces), so numeric ids are looked up.
func workspaceUsername(cfg Config) string {
	owner := strings.TrimSpace(cfg.WorkspaceOwner)
	if owner == "" {
		owner = detectOwnerFromPath(cfg.WorkspacePath)
	}
	owner, _, _ = strings.Cut(owner, ":")
	if owner == "" {
		return ""
	}
	if u, err := user.LookupId(owner); err == nil {
		return u.Username
	}
	return owner
}

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
    libglib2.0-dev libgstreamer-plugins-base1.0-dev \
    ros-%[1]s-camera-ros ros-%[1]s-compressed-image-transport

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
	if err := ensureCameraPermissions(cfg); err != nil {
		log.Printf("[agent] warning: could not ensure camera device permissions: %v", err)
	}

	configChanged, err := ensureCameraAutoDetect()
	if err != nil {
		log.Printf("[agent] warning: could not ensure camera_auto_detect: %v", err)
	}

	if _, err := os.Stat(cameraInstalledMarker); err == nil {
		log.Printf("[agent] camera support already installed")
		if err := EnsureCameraSetup(cfg); err != nil {
			log.Printf("[agent] warning: could not set up camera: %v", err)
		}
		if configChanged {
			log.Printf("[agent] camera_auto_detect was just enabled -- rebooting to apply it")
			return HandleReboot(cfg)
		}
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
	if err := EnsureCameraSetup(cfg); err != nil {
		log.Printf("[agent] warning: could not set up camera: %v", err)
	}
	if configChanged {
		log.Printf("[agent] camera_auto_detect was just enabled -- rebooting to apply it")
		return HandleReboot(cfg)
	}
	return nil
}

// HandleCaptureImage takes a photo via ROS and uploads it. When the camera
// service (see ensureCameraService) is already streaming, it just grabs a
// frame from that topic -- starting a second camera_ros node would fail on
// the busy device. Otherwise it starts a short-lived camera_ros node just for
// the duration of the capture and kills it afterward. camera_ros (libcamera-backed),
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
	if !cameraServiceActive() {
		// Use the configured stream resolution so the snapshot matches what
		// ros-camera would publish.
		w, h, err := cameraResolution(cfg)
		if err != nil {
			w, h = 640, 480
		}
		camCmd, err := rosCommand(context.Background(), "timeout", "--kill-after=5s", "30s",
			"ros2", "run", "camera_ros", "camera_node", "--ros-args",
			"-p", "format:=RGB888", "-p", fmt.Sprintf("width:=%d", w), "-p", fmt.Sprintf("height:=%d", h))
		if err != nil {
			return err
		}
		if err := camCmd.Start(); err != nil {
			return fmt.Errorf("failed to start camera node: %w", err)
		}
		defer func() {
			if camCmd.Process != nil {
				_ = camCmd.Process.Kill()
				_ = camCmd.Wait()
			}
		}()
	}

	if out, err := runROSCmd(25*time.Second, "python3", "-c", snapshotGrabScript, tmpPath); err != nil {
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
	return []string{"systemctl", "restart", rosServiceName()}
}

func rosServiceName() string {
	if service := os.Getenv("ROS_SERVICE_NAME"); service != "" {
		return service
	}
	return "ros"
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
