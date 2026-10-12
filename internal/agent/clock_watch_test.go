package agent

import (
	"testing"
	"time"
)

func TestClockWatcherRestartsOnceAfterStepsSettle(t *testing.T) {
	var w clockWatcher
	start := time.Unix(1_800_000_000, 0)
	wallOffset := time.Duration(0)
	restarts := 0
	tick := func(i int) time.Duration {
		mono := start.Add(time.Duration(i) * time.Second)
		jump, restart := w.observe(mono.Add(wallOffset), mono)
		if restart {
			restarts++
		}
		return jump
	}

	for i := 0; i < 3; i++ {
		tick(i)
	}
	wallOffset += 50 * time.Millisecond // slew-sized: ignored
	tick(3)
	wallOffset += 2 * time.Second // sync_time steps the clock
	if jump := tick(4); jump != 2*time.Second {
		t.Fatalf("jump = %s, want 2s", jump)
	}
	tick(5)
	wallOffset -= 400 * time.Millisecond // timesyncd steps it again
	tick(6)
	for i := 7; i < 11; i++ {
		tick(i)
	}
	if restarts != 0 {
		t.Fatalf("restarted %d times before the clock settled", restarts)
	}
	tick(11) // 5s after the last step
	for i := 12; i < 30; i++ {
		tick(i)
	}
	if restarts != 1 {
		t.Fatalf("restarted %d times, want 1", restarts)
	}
}
