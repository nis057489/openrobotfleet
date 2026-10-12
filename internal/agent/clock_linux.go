package agent

import "syscall"

// staUnsync is the kernel's STA_UNSYNC flag: set until an NTP client
// disciplines the clock, which is what timedatectl's "System clock
// synchronized" reports.
const staUnsync = 0x0040

// ClockSynchronized reports whether an NTP client is currently keeping the
// system clock in sync.
func ClockSynchronized() bool {
	var tx syscall.Timex
	if _, err := syscall.Adjtimex(&tx); err != nil {
		return false
	}
	return tx.Status&staUnsync == 0
}
