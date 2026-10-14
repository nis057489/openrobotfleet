package agent

import (
	"bytes"
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

//go:embed indicator
var indicatorFiles embed.FS

// indicatorPackage provides the AppIndicator binding the indicator needs;
// Ubuntu's top bar shows AppIndicators out of the box, but Jammy doesn't
// ship this binding by default.
const indicatorPackage = "gir1.2-ayatanaappindicator3-0.1"

// indicatorInstall maps each embedded file to where it goes and its mode.
var indicatorInstall = map[string]struct {
	path string
	mode os.FileMode
}{
	"time_indicator.py":                     {"/usr/local/lib/openrobotfleet/time_indicator.py", 0o644},
	"openrobotfleet-time-indicator":         {"/usr/local/bin/openrobotfleet-time-indicator", 0o755},
	"openrobotfleet-time-indicator.desktop": {"/etc/xdg/autostart/openrobotfleet-time-indicator.desktop", 0o644},
	"time-green.svg":                        {"/usr/local/share/openrobotfleet/icons/time-green.svg", 0o644},
	"time-amber.svg":                        {"/usr/local/share/openrobotfleet/icons/time-amber.svg", 0o644},
	"time-red.svg":                          {"/usr/local/share/openrobotfleet/icons/time-red.svg", 0o644},
	"time-grey.svg":                         {"/usr/local/share/openrobotfleet/icons/time-grey.svg", 0o644},
	"time-busy.svg":                         {"/usr/local/share/openrobotfleet/icons/time-busy.svg", 0o644},
}

// EnsureTimeIndicator installs the laptop top-bar indicator showing whether
// the laptop's clock agrees with its robot's (indicator/time_indicator.py),
// with buttons to resync. Autostart runs it at each login, and it is also
// started (or, when its files changed, restarted) in desktop sessions that
// are already open, so an agent update shows without logging out. Laptops
// only: robots have no desktop. Runs at agent start; rewrites files only
// when they differ from this agent's copies.
func EnsureTimeIndicator(cfg Config) {
	if cfg.Type != "laptop" {
		return
	}
	changed, err := installIndicatorFiles("/")
	if err != nil {
		log.Printf("[agent] time indicator: %v", err)
		return
	}
	// In the background: apt can wait minutes for the dpkg lock at boot.
	go func() {
		out, _ := runCmd(defaultCmdTimeout, "dpkg-query", "-W", "-f", "${db:Status-Abbrev}", indicatorPackage)
		if !strings.HasPrefix(string(out), "ii") {
			log.Printf("[agent] time indicator: installing %s", indicatorPackage)
			if _, err := aptGet("update"); err != nil {
				log.Printf("[agent] time indicator: %v", err)
				return
			}
			if _, err := aptGet("install", "-y", indicatorPackage); err != nil {
				log.Printf("[agent] time indicator: %v", err)
				return
			}
			changed = true
		}
		startIndicatorInSessions(changed)
	}()
}

// startIndicatorInSessions runs the indicator in every open graphical
// session that lacks one, restarting existing ones when restart is set. It
// runs under the user's systemd manager, which holds the session's display
// environment (WAYLAND_DISPLAY, DBUS_SESSION_BUS_ADDRESS).
func startIndicatorInSessions(restart bool) {
	for _, user := range graphicalSessionUsers() {
		running := false
		if _, err := runCmd(defaultCmdTimeout, "pgrep", "-u", user, "-f", "time_indicator.py"); err == nil {
			running = true
		}
		if running && !restart {
			continue
		}
		if running {
			runCmd(defaultCmdTimeout, "pkill", "-u", user, "-f", "time_indicator.py")
			// A transient unit's name stays taken until it has stopped.
			runCmd(defaultCmdTimeout, "systemctl", "--user", "--machine="+user+"@", "stop", "openrobotfleet-time-indicator")
		}
		out, err := runCmd(defaultCmdTimeout, "systemd-run", "--user", "--machine="+user+"@",
			"--unit=openrobotfleet-time-indicator", "--collect", "/usr/local/bin/openrobotfleet-time-indicator")
		if err != nil {
			log.Printf("[agent] time indicator: start for %s: %v: %s", user, err, strings.TrimSpace(string(out)))
			continue
		}
		log.Printf("[agent] time indicator: started in %s's session", user)
	}
}

// graphicalSessionUsers lists users with an open Wayland or X11 session.
func graphicalSessionUsers() []string {
	list, err := runCmd(defaultCmdTimeout, "loginctl", "list-sessions", "--no-legend")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var users []string
	for _, line := range strings.Split(string(list), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		out, err := runCmd(defaultCmdTimeout, "loginctl", "show-session", fields[0], "-p", "Name", "-p", "Type", "-p", "Remote")
		if err != nil {
			continue
		}
		// key=value lines; loginctl orders them itself, not as requested.
		props := parseSessionProps(string(out))
		name := props["Name"]
		if (props["Type"] != "wayland" && props["Type"] != "x11") || props["Remote"] != "no" || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		users = append(users, name)
	}
	return users
}

func parseSessionProps(out string) map[string]string {
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[k] = v
		}
	}
	return props
}

// installIndicatorFiles writes the embedded files under root ("/" outside
// tests) and reports whether any changed.
func installIndicatorFiles(root string) (bool, error) {
	changed := false
	err := fs.WalkDir(indicatorFiles, "indicator", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		target, ok := indicatorInstall[filepath.Base(name)]
		if !ok {
			return nil
		}
		data, err := indicatorFiles.ReadFile(name)
		if err != nil {
			return err
		}
		path := filepath.Join(root, target.path)
		if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
			if info, err := os.Stat(path); err == nil && info.Mode().Perm() == target.mode {
				return nil
			}
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, target.mode); err != nil {
			return err
		}
		if err := os.Chmod(path, target.mode); err != nil {
			return err
		}
		changed = true
		log.Printf("[agent] installed %s", target.path)
		return nil
	})
	return changed, err
}
