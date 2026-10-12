package controller

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"example.com/openrobot-fleet/internal/agent"
	"example.com/openrobot-fleet/internal/db"
)

// timeRetryInterval spaces out re-sending configure_time to a device that
// still reports another server, so one that keeps failing it is not sent a
// new job on every heartbeat.
const timeRetryInterval = 5 * time.Minute

// timeReport is what a device last said about its clock.
type timeReport struct {
	NTPServer   string
	ClockSynced bool
}

type timeReports struct {
	mu sync.RWMutex
	m  map[string]timeReport // by agent ID
}

var reportedTime = timeReports{m: map[string]timeReport{}}

type timeConformance struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	NTPServer   string `json:"ntp_server"`
	ClockSynced bool   `json:"clock_synced"`
	// conformant, pending (wrong server, configure_time queued or in
	// flight), drifting (right server but clock not yet synchronised),
	// unsupported (agent too old to report), or unknown (not heard from
	// since the controller started).
	State string `json:"state"`
}

// GetTimeConfig returns the fleet NTP setting and each device's conformance.
func (c *Controller) GetTimeConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := c.DB.GetTimeConfig(r.Context())
	if err != nil {
		log.Printf("get time config: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load time config")
		return
	}
	robots, err := c.DB.ListRobots(r.Context())
	if err != nil {
		log.Printf("list robots: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list robots")
		return
	}
	devices := make([]timeConformance, 0, len(robots))
	reportedTime.mu.RLock()
	for _, robot := range robots {
		if robot.AgentID == "" {
			continue
		}
		d := timeConformance{ID: robot.ID, Name: robot.Name, Type: robot.Type, Status: robot.Status}
		rep, ok := reportedTime.m[robot.AgentID]
		switch {
		case !ok:
			d.State = "unknown"
			if c.unsupportedTime(robot.AgentID) {
				d.State = "unsupported"
			}
		case cfg.NTPServer != "" && rep.NTPServer != cfg.NTPServer:
			d.State = "pending"
		case !rep.ClockSynced:
			d.State = "drifting"
		default:
			d.State = "conformant"
		}
		d.NTPServer, d.ClockSynced = rep.NTPServer, rep.ClockSynced
		devices = append(devices, d)
	}
	reportedTime.mu.RUnlock()
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"ntp_server": cfg.NTPServer,
		"devices":    devices,
	})
}

// UpdateTimeConfig saves the fleet NTP server and sends it to every device
// straight away; devices that are offline get it from the outbox when they
// reconnect, and EnsureTimeConformance catches any that drift later.
func (c *Controller) UpdateTimeConfig(w http.ResponseWriter, r *http.Request) {
	var cfg db.TimeConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		respondError(w, http.StatusBadRequest, "invalid time config")
		return
	}
	cfg.NTPServer = strings.TrimSpace(cfg.NTPServer)
	if strings.ContainsAny(cfg.NTPServer, " \t\r\n/") {
		respondError(w, http.StatusBadRequest, "ntp_server must be a hostname or IP address")
		return
	}
	if err := c.DB.SaveTimeConfig(r.Context(), cfg); err != nil {
		log.Printf("save time config: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to save time config")
		return
	}
	robots, err := c.DB.ListRobots(r.Context())
	if err != nil {
		log.Printf("list robots: %v", err)
		respondError(w, http.StatusInternalServerError, "saved, but failed to list robots")
		return
	}
	queued := 0
	for _, robot := range robots {
		if robot.AgentID == "" {
			continue
		}
		if err := c.queueConfigureTime(r.Context(), robot, cfg.NTPServer); err != nil {
			log.Printf("queue configure_time for %s: %v", robot.Name, err)
			continue
		}
		queued++
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{"ntp_server": cfg.NTPServer, "queued": queued})
}

// EnsureTimeConformance runs on every heartbeat. ntpServer is nil when the
// agent predates configure_time, which it cannot apply, so none is sent.
func (c *Controller) EnsureTimeConformance(agentID string, ntpServer *string, clockSynced bool) {
	if ntpServer == nil {
		return
	}
	reportedTime.mu.Lock()
	reportedTime.m[agentID] = timeReport{NTPServer: *ntpServer, ClockSynced: clockSynced}
	reportedTime.mu.Unlock()

	ctx := context.Background()
	cfg, err := c.DB.GetTimeConfig(ctx)
	if err != nil || cfg.NTPServer == "" || *ntpServer == cfg.NTPServer {
		return
	}
	// One in flight, or one tried recently: let it finish or fail first.
	if id, err := c.DB.LatestJobID(ctx, agentID, "configure_time"); err == nil && id != 0 {
		if job, err := c.DB.GetJob(ctx, id); err == nil {
			if job.Status == "queued" || job.Status == "pending" || job.Status == "running" ||
				time.Since(job.UpdatedAt) < timeRetryInterval {
				return
			}
		}
	}
	robot, err := c.DB.GetRobotByAgentID(ctx, agentID)
	if err != nil {
		return
	}
	log.Printf("time: %s reports NTP server %q, want %q; sending configure_time", robot.Name, *ntpServer, cfg.NTPServer)
	if err := c.queueConfigureTime(ctx, robot, cfg.NTPServer); err != nil {
		log.Printf("queue configure_time for %s: %v", robot.Name, err)
	}
}

// unsupportedTime reports whether the device's last configure_time failed
// because its agent does not know the command.
func (c *Controller) unsupportedTime(agentID string) bool {
	ctx := context.Background()
	id, err := c.DB.LatestJobID(ctx, agentID, "configure_time")
	if err != nil || id == 0 {
		return false
	}
	job, err := c.DB.GetJob(ctx, id)
	return err == nil && strings.Contains(job.Error, "unknown command type")
}

func (c *Controller) queueConfigureTime(ctx context.Context, robot db.Robot, server string) error {
	data, err := json.Marshal(agent.ConfigureTimeData{NTPServer: server})
	if err != nil {
		return err
	}
	_, err = c.queueRobotCommand(ctx, robot, agent.Command{Type: "configure_time", Data: data})
	return err
}
