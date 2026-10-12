package agent

import (
	"context"
	"log"
	"os"
	"strings"
	"time"
)

const (
	// clockJumpThreshold is how far the wall clock must move against the
	// monotonic clock between checks to count as a step. NTP slewing
	// (timesyncd corrects offsets under 0.4s gradually) stays far below it.
	clockJumpThreshold = 200 * time.Millisecond
	clockCheckInterval = time.Second
	// A clock often steps more than once while settling (sync_time, then
	// timesyncd's first sync), so ROS is restarted once it has been quiet
	// this long rather than after every step.
	clockSettleDelay = 5 * time.Second
)

// clockWatcher detects steps in the wall clock by comparing it with the
// monotonic clock, which a step does not move.
type clockWatcher struct {
	lastWall, lastMono time.Time
	steppedAt          time.Time // monotonic; zero when no restart is pending
}

// observe takes the wall and monotonic readings separately so tests can
// supply both. It returns how far the wall clock jumped since the previous
// reading, and whether a restart is now due.
func (w *clockWatcher) observe(wall, mono time.Time) (jump time.Duration, restart bool) {
	if !w.lastMono.IsZero() {
		jump = wall.Sub(w.lastWall) - mono.Sub(w.lastMono)
		if jump.Abs() > clockJumpThreshold {
			w.steppedAt = mono
		}
	}
	w.lastWall, w.lastMono = wall, mono
	if !w.steppedAt.IsZero() && mono.Sub(w.steppedAt) >= clockSettleDelay {
		w.steppedAt = time.Time{}
		restart = true
	}
	return jump, restart
}

// watchClock restarts ROS after the system clock is stepped. Nodes running
// across a step keep TF data stamped in the old time, so lookups fail with
// "transform data too old" until they restart.
func watchClock(ctx context.Context) {
	var w clockWatcher
	ticker := time.NewTicker(clockCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			jump, restart := w.observe(now.Round(0), now)
			if jump.Abs() > clockJumpThreshold {
				log.Printf("[agent] system clock stepped by %s", jump)
			}
			if restart {
				restartROSAfterClockStep()
			}
		}
	}
}

func restartROSAfterClockStep() {
	unit := rosServiceName() + ".service"
	if os.Getenv("ROS_RESTART_CMD") != "" {
		if err := HandleRestartROS(Config{}); err != nil {
			log.Printf("[agent] restart ROS after clock step: %v", err)
		}
		return
	}
	if _, err := runCmd(defaultCmdTimeout, "systemctl", "cat", unit); err != nil {
		return // no ROS service here (laptops)
	}
	// try-restart leaves a deliberately stopped service stopped.
	if out, err := runCmd(defaultCmdTimeout, "systemctl", "try-restart", unit); err != nil {
		log.Printf("[agent] restart %s after clock step: %v: %s", unit, err, strings.TrimSpace(string(out)))
		return
	}
	log.Printf("[agent] restarted %s after clock step", unit)
}
