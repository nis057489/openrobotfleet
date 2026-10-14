package agent

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Drop-ins written by configure_time. Each overrides the distro's servers
// (ntp.ubuntu.com, unreachable on a network that blocks NTP) with the
// controller's choice.
const (
	timesyncdDropIn = "/etc/systemd/timesyncd.conf.d/openrobot.conf"
	chronySources   = "/etc/chrony/sources.d/openrobot.sources"
)

// HandleConfigureTime points the device's NTP client at data.NTPServer, or
// removes the override when it is empty. chrony is configured when it is the
// active client; otherwise systemd-timesyncd, Ubuntu's default.
func HandleConfigureTime(data ConfigureTimeData) error {
	server := strings.TrimSpace(data.NTPServer)
	if strings.ContainsAny(server, " \t\r\n") {
		return fmt.Errorf("invalid NTP server %q", server)
	}
	if chronyActive() {
		if _, err := writeOrRemove(chronySources, server, chronySourcesContent(server)); err != nil {
			return err
		}
		if out, err := runCmd(defaultCmdTimeout, "chronyc", "reload", "sources"); err != nil {
			return fmt.Errorf("chronyc reload sources: %v: %s", err, strings.TrimSpace(string(out)))
		}
	} else {
		if _, err := writeOrRemove(timesyncdDropIn, server, timesyncdContent(server)); err != nil {
			return err
		}
		if out, err := runCmd(defaultCmdTimeout, "timedatectl", "set-ntp", "true"); err != nil {
			return fmt.Errorf("timedatectl set-ntp: %v: %s", err, strings.TrimSpace(string(out)))
		}
		if out, err := runCmd(defaultCmdTimeout, "systemctl", "restart", "systemd-timesyncd"); err != nil {
			return fmt.Errorf("restart systemd-timesyncd: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}
	if server == "" {
		log.Printf("[agent] removed NTP server override")
	} else {
		log.Printf("[agent] NTP server set to %s", server)
	}
	return nil
}

// timesyncdContent is the drop-in for server. An empty FallbackNTP stops
// timesyncd drifting back to the unreachable distro servers when the lab
// server is briefly down. timesyncd backs off to checking every 34 minutes
// once the clock looks stable, so when the lab server corrects its own clock
// (it has no RTC, and reboots on a stale time) a device could keep the old
// time for half an hour, out of step with its group's other device; checking
// every minute keeps them within a minute of any correction.
func timesyncdContent(server string) string {
	return fmt.Sprintf("# Managed by openrobot-agent (configure_time)\n[Time]\nNTP=%s\nFallbackNTP=\nPollIntervalMaxSec=64\n", server)
}

// chronySourcesContent is the chrony equivalent; maxpoll 6 is 2^6 = 64s.
func chronySourcesContent(server string) string {
	return fmt.Sprintf("# Managed by openrobot-agent (configure_time)\nserver %s iburst maxpoll 6\n", server)
}

// RefreshTimeConfig rewrites an existing configure_time drop-in whose content
// predates this agent, e.g. to add the shorter poll interval, and restarts
// the NTP client only if it changed. The controller only compares servers,
// so it would not re-send configure_time for this. Runs at agent start.
func RefreshTimeConfig() error {
	if server := scanValue(chronySources, "server "); server != "" {
		changed, err := writeOrRemove(chronySources, strings.Fields(server)[0], chronySourcesContent(strings.Fields(server)[0]))
		if err != nil || !changed {
			return err
		}
		if out, err := runCmd(defaultCmdTimeout, "chronyc", "reload", "sources"); err != nil {
			return fmt.Errorf("chronyc reload sources: %v: %s", err, strings.TrimSpace(string(out)))
		}
		log.Printf("[agent] updated %s", chronySources)
		return nil
	}
	if server := scanValue(timesyncdDropIn, "NTP="); server != "" {
		changed, err := writeOrRemove(timesyncdDropIn, server, timesyncdContent(server))
		if err != nil || !changed {
			return err
		}
		if out, err := runCmd(defaultCmdTimeout, "systemctl", "restart", "systemd-timesyncd"); err != nil {
			return fmt.Errorf("restart systemd-timesyncd: %v: %s", err, strings.TrimSpace(string(out)))
		}
		log.Printf("[agent] updated %s", timesyncdDropIn)
	}
	return nil
}

// writeOrRemove writes content to path, or removes path when server is
// empty, and reports whether the file changed.
func writeOrRemove(path, server, content string) (bool, error) {
	existing, readErr := os.ReadFile(path)
	if server == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, err
		}
		return readErr == nil, nil
	}
	if readErr == nil && string(existing) == content {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(content), 0o644)
}

func chronyActive() bool {
	_, err := runCmd(defaultCmdTimeout, "systemctl", "is-active", "--quiet", "chrony")
	return err == nil
}

// ConfiguredNTPServer reports the server set by configure_time, or "" when
// no override is in place. It only reads files, so it is cheap enough for
// every heartbeat.
func ConfiguredNTPServer() string {
	if server := scanValue(chronySources, "server "); server != "" {
		return strings.Fields(server)[0]
	}
	return scanValue(timesyncdDropIn, "NTP=")
}

func scanValue(path, prefix string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}
