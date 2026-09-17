package agent

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// DeriveAgentIDFromMAC picks the first non-loopback interface with a real
// hardware address (sorted by name for determinism across boots) and formats
// it as a stable agent identity. Used when no agent_id was pinned in the
// local config file, so reinstalling or re-flashing a device always yields
// the same identity as long as its network hardware is unchanged.
func DeriveAgentIDFromMAC() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].Name < ifaces[j].Name })

	// Prefer a wired interface if one is present, since some Wi-Fi chipsets
	// can rotate their MAC for privacy purposes.
	var wired, any string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		mac := iface.HardwareAddr.String()
		if mac == "" || mac == "00:00:00:00:00:00" {
			continue
		}
		if any == "" {
			any = mac
		}
		if wired == "" && strings.HasPrefix(iface.Name, "eth") {
			wired = mac
		}
	}
	mac := wired
	if mac == "" {
		mac = any
	}
	if mac == "" {
		return "", errors.New("no usable network interface with a hardware address")
	}
	return FormatAgentIDFromMAC(mac), nil
}

// FormatAgentIDFromMAC is the pure formatting half of identity derivation,
// shared with internal/ssh which computes the same value over SSH at
// install time so both paths produce byte-identical IDs.
func FormatAgentIDFromMAC(mac string) string {
	return "mac-" + strings.ToLower(strings.ReplaceAll(mac, ":", ""))
}

// SetHostname sets the OS hostname via hostnamectl and keeps /etc/hosts
// consistent by replacing any occurrence of the previous hostname with the
// new one, so local hostname resolution (used by sudo and some ROS tooling)
// doesn't keep pointing at a stale name.
func SetHostname(name string) error {
	old, _ := os.Hostname()
	if err := exec.Command("hostnamectl", "set-hostname", name).Run(); err != nil {
		return err
	}
	if old == "" || old == name {
		return nil
	}
	data, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return nil // best-effort; hostnamectl already succeeded
	}
	updated := strings.ReplaceAll(string(data), old, name)
	if updated != string(data) {
		_ = os.WriteFile("/etc/hosts", []byte(updated), 0o644)
	}
	return nil
}

// SanitizeHostname coerces an arbitrary display name into a valid Linux
// hostname label: lowercase, [a-z0-9-] only, no leading/trailing hyphen,
// capped at 63 chars. Cosmetic validation only -- callers pass this to
// exec.Command/SSH exec with argument arrays, never shell interpolation,
// so this is not a security boundary.
func SanitizeHostname(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	if len(s) > 63 {
		s = strings.Trim(s[:63], "-")
	}
	return s
}

// fleetTimezone is applied to every device so timestamps in logs and on the
// dashboard read the same across the fleet. Australia/Brisbane is AEST
// (UTC+10) with no daylight saving. ROS/DDS time is UTC epoch, so this has no
// effect on TF or message stamps -- clock sync does.
const fleetTimezone = "Australia/Brisbane"

// EnsureTimezone sets the OS timezone to fleetTimezone via timedatectl if it
// isn't already.
func EnsureTimezone() error {
	out, err := runCmd(defaultCmdTimeout, "timedatectl", "show", "--property=Timezone", "--value")
	if err == nil && strings.TrimSpace(string(out)) == fleetTimezone {
		return nil
	}
	if out, err := runCmd(defaultCmdTimeout, "timedatectl", "set-timezone", fleetTimezone); err != nil {
		return fmt.Errorf("set timezone %s: %w: %s", fleetTimezone, err, strings.TrimSpace(string(out)))
	}
	log.Printf("[agent] set timezone to %s", fleetTimezone)
	return nil
}
