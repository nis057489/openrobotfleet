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
		if err := writeOrRemove(chronySources, server, fmt.Sprintf("# Managed by openrobot-agent (configure_time)\nserver %s iburst\n", server)); err != nil {
			return err
		}
		if out, err := runCmd(defaultCmdTimeout, "chronyc", "reload", "sources"); err != nil {
			return fmt.Errorf("chronyc reload sources: %v: %s", err, strings.TrimSpace(string(out)))
		}
	} else {
		// An empty FallbackNTP stops timesyncd drifting back to the
		// unreachable distro servers when the lab server is briefly down.
		if err := writeOrRemove(timesyncdDropIn, server, fmt.Sprintf("# Managed by openrobot-agent (configure_time)\n[Time]\nNTP=%s\nFallbackNTP=\n", server)); err != nil {
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

func writeOrRemove(path, server, content string) error {
	if server == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
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
