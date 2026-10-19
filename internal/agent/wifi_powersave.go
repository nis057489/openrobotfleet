package agent

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Wi-Fi power saving lets the adapter doze between beacons: fine for a
// browser, but it delays and drops DDS traffic, so /scan and /tf arrive late
// and nav2's transforms time out. The Pi's brcmfmac driver turns it on by
// default and back on whenever the link comes up again, and NetworkManager
// on the laptops enables it per connection, so the agent keeps it off: once
// through NetworkManager's config, and by checking every interface
// periodically, which also catches a driver re-enabling it.

const (
	wifiPowerSaveInterval = 30 * time.Second
	nmPowerSaveConfPath   = "/etc/NetworkManager/conf.d/zz-openrobotfleet-wifi-powersave.conf"
	// wifi.powersave 2 is "disable"; zz- sorts after Ubuntu's
	// default-wifi-powersave-on.conf, so it wins.
	nmPowerSaveConf = "# Managed by openrobotfleet-agent: Wi-Fi power saving delays ROS traffic.\n" +
		"[connection]\nwifi.powersave = 2\n"
)

// watchWifiPowerSave keeps Wi-Fi power saving off on every wireless
// interface for as long as ctx lives.
func watchWifiPowerSave(ctx context.Context) {
	ensureNMPowerSaveOff()
	if !ensureIW() {
		return
	}
	ticker := time.NewTicker(wifiPowerSaveInterval)
	defer ticker.Stop()
	for {
		disableWifiPowerSave()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ensureNMPowerSaveOff makes NetworkManager connect with power saving off,
// where NetworkManager is installed. It takes effect on the next connection;
// disableWifiPowerSave covers the current one.
func ensureNMPowerSaveOff() {
	if _, err := os.Stat("/etc/NetworkManager"); err != nil {
		return
	}
	if existing, _ := os.ReadFile(nmPowerSaveConfPath); string(existing) == nmPowerSaveConf {
		return
	}
	if err := os.MkdirAll(filepath.Dir(nmPowerSaveConfPath), 0o755); err != nil {
		log.Printf("[agent] wifi power save: %v", err)
		return
	}
	if err := os.WriteFile(nmPowerSaveConfPath, []byte(nmPowerSaveConf), 0o644); err != nil {
		log.Printf("[agent] wifi power save: %v", err)
		return
	}
	runCmd(defaultCmdTimeout, "systemctl", "reload", "NetworkManager")
	log.Printf("[agent] installed %s", nmPowerSaveConfPath)
}

// ensureIW installs iw if it's missing, reporting whether it's available.
func ensureIW() bool {
	if _, err := exec.LookPath("iw"); err == nil {
		return true
	}
	if len(wirelessInterfaces()) == 0 {
		return false
	}
	log.Printf("[agent] installing iw to manage Wi-Fi power saving")
	if out, err := runCmd(10*time.Minute, "bash", "-c", "DEBIAN_FRONTEND=noninteractive apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y iw"); err != nil {
		log.Printf("[agent] install iw: %v: %s; Wi-Fi power saving stays as the driver set it", err, strings.TrimSpace(string(out)))
		return false
	}
	return true
}

// wirelessInterfaces lists the device's Wi-Fi interfaces.
func wirelessInterfaces() []string {
	dirs, _ := filepath.Glob("/sys/class/net/*/wireless")
	ifaces := make([]string, len(dirs))
	for i, d := range dirs {
		ifaces[i] = filepath.Base(filepath.Dir(d))
	}
	return ifaces
}

// disableWifiPowerSave turns power saving off on each wireless interface
// that has it on, logging each time: a driver that keeps re-enabling it
// shows up in the agent's log.
func disableWifiPowerSave() {
	for _, iface := range wirelessInterfaces() {
		out, err := runCmd(defaultCmdTimeout, "iw", "dev", iface, "get", "power_save")
		if err != nil || !strings.Contains(string(out), "Power save: on") {
			continue
		}
		if out, err := runCmd(defaultCmdTimeout, "iw", "dev", iface, "set", "power_save", "off"); err != nil {
			log.Printf("[agent] turn off Wi-Fi power saving on %s: %v: %s", iface, err, strings.TrimSpace(string(out)))
			continue
		}
		log.Printf("[agent] turned off Wi-Fi power saving on %s", iface)
	}
}
