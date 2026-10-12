package agent

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

// rebootRequiredPath is created by Ubuntu packages (kernel, libc, firmware,
// drivers) whose upgrade only takes effect after a reboot.
const rebootRequiredPath = "/var/run/reboot-required"

// RebootRequired reports whether an upgrade is waiting on a reboot.
func RebootRequired() bool {
	_, err := os.Stat(rebootRequiredPath)
	return err == nil
}

// BootID identifies the current boot; it changes every time the device
// starts, which lets the controller tell that a reboot has completed.
var BootID = sync.OnceValue(func() string {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
})

// HandleDelayedReboot schedules a reboot a few seconds out and returns at
// once. Rebooting inside the job would leave it running when the agent dies,
// and the agent reports such a job as failed when it restarts; the delay lets
// this job's success reach the controller first.
func HandleDelayedReboot() error {
	if out, err := runCmd(defaultCmdTimeout, "systemd-run", "--on-active=15", "--timer-property=AccuracySec=1s", "systemctl", "reboot"); err != nil {
		return fmt.Errorf("schedule reboot: %v: %s", err, strings.TrimSpace(string(out)))
	}
	log.Printf("[agent] reboot scheduled in 15s")
	return nil
}
