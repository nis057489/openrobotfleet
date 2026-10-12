//go:build !linux

package agent

// ClockSynchronized is only implemented on Linux, where the agent runs.
func ClockSynchronized() bool { return false }
