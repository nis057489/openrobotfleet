package agent

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Laptops have no ros.service: students start ROS by hand (nav2, SLAM,
// Gazebo, RViz), and a launch that was closed with its terminal or hit with
// Ctrl-C more than once can leave nodes running. The next launch then runs a
// second copy of each, and two slam_toolbox or costmap nodes on the graph
// make navigation fail in ways a restart of the robot never fixes. So on a
// laptop, restart_ros stops every ROS process the workspace user has running
// instead, leaving a clean slate to launch from.

// laptopROSStopGrace is how long processes get to exit after SIGINT (what
// Ctrl-C sends, so launch shuts its nodes down properly) and then SIGTERM,
// before SIGKILL.
const laptopROSStopGrace = 5 * time.Second

// gazeboBinaries are Gazebo's own executables, which live outside /opt/ros.
var gazeboBinaries = map[string]bool{"gzserver": true, "gzclient": true, "gazebo": true, "gz": true, "ign": true}

// colconInstallLib matches a node built in a colcon workspace
// (<ws>/install/<pkg>/lib/<pkg>/<node>).
var colconInstallLib = regexp.MustCompile(`/install/[^/]+/lib/`)

// isROSProcess reports whether argv belongs to a ROS process: a ROS binary or
// script from /opt/ros or a colcon workspace (ros2 CLI and daemon, launch,
// nodes, RViz) or Gazebo. Only the executable, or the script a Python
// interpreter runs, is checked, so an editor or pager that merely has a ROS
// file open is left alone.
func isROSProcess(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	exe := argv[0]
	if gazeboBinaries[filepath.Base(exe)] {
		return true
	}
	if strings.HasPrefix(filepath.Base(exe), "python") {
		exe = ""
		for _, a := range argv[1:] {
			if !strings.HasPrefix(a, "-") {
				exe = a
				break
			}
		}
	}
	return strings.Contains(exe, "/opt/ros/") || colconInstallLib.MatchString(exe)
}

type rosProcess struct {
	pid, ppid int
	cmdline   string
}

// findROSProcesses lists uid's ROS processes from /proc.
func findROSProcesses(uid int) []rosProcess {
	entries, _ := os.ReadDir("/proc")
	self := os.Getpid()
	var procs []rosProcess
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
		if err != nil || procStatusField(status, "Uid") != strconv.Itoa(uid) {
			continue
		}
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}
		argv := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
		if !isROSProcess(argv) {
			continue
		}
		ppid, _ := strconv.Atoi(procStatusField(status, "PPid"))
		procs = append(procs, rosProcess{pid: pid, ppid: ppid, cmdline: strings.Join(argv, " ")})
	}
	return procs
}

// procStatusField returns the first value of a /proc/<pid>/status field
// (for Uid, the real uid).
func procStatusField(status []byte, name string) string {
	for _, line := range strings.Split(string(status), "\n") {
		if v, ok := strings.CutPrefix(line, name+":"); ok {
			if f := strings.Fields(v); len(f) > 0 {
				return f[0]
			}
		}
	}
	return ""
}

// processAlive reports whether pid is still running; a zombie has exited
// and only awaits its parent.
func processAlive(pid int) bool {
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	return err == nil && procStatusField(status, "State") != "Z"
}

// signalUntilGone sends sig to each live pid and waits up to grace for them
// to exit, returning the ones still running.
func signalUntilGone(pids []int, sig syscall.Signal, grace time.Duration) []int {
	for _, pid := range pids {
		syscall.Kill(pid, sig)
	}
	deadline := time.Now().Add(grace)
	for {
		var alive []int
		for _, pid := range pids {
			if processAlive(pid) {
				alive = append(alive, pid)
			}
		}
		if len(alive) == 0 || time.Now().After(deadline) {
			return alive
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// restartLaptopROS stops the workspace user's ROS processes and resyncs the
// clock: a drifted laptop clock makes SLAM drop the robot's scans, worth
// fixing while we're here but not worth failing the restart over.
func restartLaptopROS(cfg Config) error {
	if err := stopLaptopROS(cfg); err != nil {
		return err
	}
	if err := HandleResyncTime(); err != nil {
		log.Printf("[agent] laptop ROS restart: %v", err)
	}
	return nil
}

// stopLaptopROS stops the workspace user's ROS processes (SIGINT, then
// SIGTERM, then SIGKILL) and clears shared-memory files Fast DDS leaves
// behind. Each process found is logged first, with its parent: a ppid of 1
// or of the user's systemd marks a node orphaned by an earlier launch.
func stopLaptopROS(cfg Config) error {
	name := workspaceUsername(cfg)
	if name == "" {
		return errors.New("could not determine the workspace user whose ROS to restart")
	}
	u, err := user.Lookup(name)
	if err != nil {
		return fmt.Errorf("look up user %s: %w", name, err)
	}
	uid, _ := strconv.Atoi(u.Uid)

	procs := findROSProcesses(uid)
	log.Printf("[agent] laptop ROS restart: stopping %d ROS process(es) of %s", len(procs), name)
	pids := make([]int, len(procs))
	for i, p := range procs {
		pids[i] = p.pid
		cmd := p.cmdline
		if len(cmd) > 200 {
			cmd = cmd[:200] + "..."
		}
		log.Printf("[agent]   pid %d (ppid %d): %s", p.pid, p.ppid, cmd)
	}
	left := signalUntilGone(pids, syscall.SIGINT, laptopROSStopGrace)
	if len(left) > 0 {
		log.Printf("[agent] laptop ROS restart: %d process(es) ignored SIGINT, sending SIGTERM", len(left))
		left = signalUntilGone(left, syscall.SIGTERM, laptopROSStopGrace)
	}
	if len(left) > 0 {
		log.Printf("[agent] laptop ROS restart: %d process(es) ignored SIGTERM, sending SIGKILL", len(left))
		left = signalUntilGone(left, syscall.SIGKILL, 2*time.Second)
	}

	// Fast DDS (the default RMW, if a student switches back to it) keeps
	// shared-memory segments that outlive a killed process.
	shm, _ := filepath.Glob("/dev/shm/*fastrtps_*")
	for _, f := range shm {
		if info, err := os.Stat(f); err == nil {
			if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) == uid {
				os.Remove(f)
			}
		}
	}

	if len(left) > 0 {
		return fmt.Errorf("%d ROS process(es) could not be stopped: %v", len(left), left)
	}
	log.Printf("[agent] laptop ROS restart: all ROS processes stopped")
	return nil
}
