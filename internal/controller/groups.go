package controller

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"text/template"

	"example.com/openrobot-fleet/internal/agent"
	"example.com/openrobot-fleet/internal/db"
)

// defaultDomainBase mirrors the source lab-network plan's example allocation
// (group 1 -> domain 11, group 2 -> domain 12, ...).
const defaultDomainBase = 11

type groupRequest struct {
	Name        string `json:"name"`
	ROSDomainID int    `json:"ros_domain_id"`
	RobotID     *int64 `json:"robot_id"`
	LaptopID    *int64 `json:"laptop_id"`
	StaticPeers bool   `json:"static_peers"`
	Notes       string `json:"notes"`
}

func (req groupRequest) toGroup(id int64) db.Group {
	return db.Group{
		ID:          id,
		Name:        strings.TrimSpace(req.Name),
		ROSDomainID: req.ROSDomainID,
		RobotID:     req.RobotID,
		LaptopID:    req.LaptopID,
		StaticPeers: req.StaticPeers,
		Notes:       req.Notes,
	}
}

func (c *Controller) ListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := c.DB.ListGroups(r.Context())
	if err != nil {
		log.Printf("list groups: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list groups")
		return
	}
	respondJSON(w, http.StatusOK, groups)
}

func (c *Controller) GetGroup(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDFromPath(r.URL.Path, "/api/groups/")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid group id")
		return
	}
	group, err := c.DB.GetGroupByID(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			respondError(w, http.StatusNotFound, "group not found")
			return
		}
		log.Printf("get group: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to fetch group")
		return
	}
	respondJSON(w, http.StatusOK, group)
}

func (c *Controller) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid group payload")
		return
	}
	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "group name required")
		return
	}
	existing, err := c.DB.ListGroups(r.Context())
	if err != nil {
		log.Printf("create group: list existing: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to validate group")
		return
	}
	if req.ROSDomainID == 0 {
		req.ROSDomainID = nextFreeDomainID(existing)
	}
	group := req.toGroup(0)
	if err := validateGroup(existing, group, 0); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := c.DB.CreateGroup(r.Context(), group)
	if err != nil {
		log.Printf("create group: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to create group")
		return
	}
	group.ID = id

	applied, skipped := c.applyGroupNetwork(r.Context(), group)
	respondJSON(w, http.StatusCreated, map[string]interface{}{
		"group": group, "applied": applied, "skipped": skipped,
	})
}

func (c *Controller) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDFromPath(r.URL.Path, "/api/groups/")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid group id")
		return
	}
	var req groupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid group payload")
		return
	}
	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "group name required")
		return
	}
	existing, err := c.DB.ListGroups(r.Context())
	if err != nil {
		log.Printf("update group: list existing: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to validate group")
		return
	}
	group := req.toGroup(id)
	if group.ROSDomainID == 0 {
		group.ROSDomainID = nextFreeDomainID(existing)
	}
	if err := validateGroup(existing, group, id); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := c.DB.UpdateGroup(r.Context(), group); err != nil {
		log.Printf("update group: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to update group")
		return
	}

	applied, skipped := c.applyGroupNetwork(r.Context(), group)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"group": group, "applied": applied, "skipped": skipped,
	})
}

func (c *Controller) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := parseIDFromPath(r.URL.Path, "/api/groups/")
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid group id")
		return
	}
	if err := c.DB.DeleteGroup(r.Context(), id); err != nil {
		log.Printf("delete group: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to delete group")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ApplyGroup re-pushes a group's current DB configuration to its robot and
// laptop over MQTT without changing anything in the DB. Useful after a robot
// that was offline during CreateGroup/UpdateGroup reconnects.
func (c *Controller) ApplyGroup(w http.ResponseWriter, r *http.Request) {
	id, err := parseGroupApplyID(r.URL.Path)
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid group apply path")
		return
	}
	group, err := c.DB.GetGroupByID(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			respondError(w, http.StatusNotFound, "group not found")
			return
		}
		log.Printf("apply group fetch: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load group")
		return
	}
	applied, skipped := c.applyGroupNetwork(r.Context(), group)
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"applied": applied, "skipped": skipped,
	})
}

func parseGroupApplyID(path string) (int64, error) {
	trimmed := strings.TrimSuffix(path, "/")
	if !strings.HasSuffix(trimmed, "/apply") {
		return 0, fmt.Errorf("missing apply suffix")
	}
	base := strings.TrimSuffix(trimmed, "/apply")
	return parseIDFromPath(base, "/api/groups/")
}

// nextFreeDomainID suggests the next unused domain ID starting at
// defaultDomainBase, matching the source plan's group-1 -> 11, group-2 -> 12
// example allocation.
func nextFreeDomainID(existing []db.Group) int {
	used := make(map[int]bool, len(existing))
	for _, g := range existing {
		used[g.ROSDomainID] = true
	}
	for candidate := defaultDomainBase; ; candidate++ {
		if !used[candidate] {
			return candidate
		}
	}
}

// validateGroup enforces the app-level invariants the source plan depends on:
// each robot/laptop belongs to at most one group, and each group has a
// unique ROS_DOMAIN_ID so groups never overlap on the DDS graph.
func validateGroup(existing []db.Group, g db.Group, selfID int64) error {
	if g.RobotID != nil && g.LaptopID != nil && *g.RobotID == *g.LaptopID {
		return errors.New("robot and laptop must be different devices")
	}
	for _, other := range existing {
		if other.ID == selfID {
			continue
		}
		if other.ROSDomainID == g.ROSDomainID {
			return fmt.Errorf("ROS_DOMAIN_ID %d is already used by group %q", g.ROSDomainID, other.Name)
		}
		if g.RobotID != nil && other.RobotID != nil && *other.RobotID == *g.RobotID {
			return fmt.Errorf("robot is already assigned to group %q", other.Name)
		}
		if g.LaptopID != nil && other.LaptopID != nil && *other.LaptopID == *g.LaptopID {
			return fmt.Errorf("laptop is already assigned to group %q", other.Name)
		}
	}
	return nil
}

// applyGroupNetwork pushes a configure_network MQTT command to the group's
// robot and laptop. It's best-effort per device: a device with no agent
// attached yet (or, when static peers are enabled, no known IP yet) is
// skipped rather than failing the whole call, since either side may not have
// checked in yet.
func (c *Controller) applyGroupNetwork(ctx context.Context, group db.Group) (applied []string, skipped []string) {
	var robot, laptop *db.Robot
	if group.RobotID != nil {
		if r, err := c.DB.GetRobotByID(ctx, *group.RobotID); err == nil {
			robot = &r
		}
	}
	if group.LaptopID != nil {
		if l, err := c.DB.GetRobotByID(ctx, *group.LaptopID); err == nil {
			laptop = &l
		}
	}

	send := func(role string, target *db.Robot, peerIP string) {
		if target == nil {
			return
		}
		if target.AgentID == "" {
			skipped = append(skipped, fmt.Sprintf("%s: not enrolled yet", role))
			return
		}
		var peers []string
		if group.StaticPeers {
			if peerIP == "" {
				skipped = append(skipped, fmt.Sprintf("%s: partner has no known IP yet, static peers not applied", role))
			} else {
				peers = []string{peerIP}
			}
		}
		data := agent.ConfigureNetworkData{
			ROSDomainID:       group.ROSDomainID,
			RMWImplementation: "rmw_cyclonedds_cpp",
			StaticPeers:       peers,
		}
		payload, err := json.Marshal(data)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: encode failed: %v", role, err))
			return
		}
		if _, err := c.queueRobotCommand(ctx, *target, agent.Command{Type: "configure_network", Data: payload}); err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", role, err))
			return
		}
		applied = append(applied, role)
	}

	robotIP, laptopIP := "", ""
	if robot != nil {
		robotIP = robot.IP
	}
	if laptop != nil {
		laptopIP = laptop.IP
	}

	send("robot", robot, laptopIP)
	send("laptop", laptop, robotIP)
	return applied, skipped
}

// DownloadRvizLauncher generates a small shell script the lab manager can use
// to open RViz against any group's ROS_DOMAIN_ID, e.g. `rviz-domain group-3`
// or several at once with `rviz-domain group-1 & rviz-domain group-2 &`.
func (c *Controller) DownloadRvizLauncher(w http.ResponseWriter, r *http.Request) {
	groups, err := c.DB.ListGroups(r.Context())
	if err != nil {
		log.Printf("rviz launcher: list groups: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to load groups")
		return
	}

	tmpl, err := template.New("rviz-domain").Parse(rvizLauncherTemplate)
	if err != nil {
		log.Printf("rviz launcher: parse template: %v", err)
		respondError(w, http.StatusInternalServerError, "template error")
		return
	}

	w.Header().Set("Content-Type", "text/x-shellscript")
	w.Header().Set("Content-Disposition", "attachment; filename=rviz-domain")
	if err := tmpl.Execute(w, map[string]interface{}{"Groups": groups}); err != nil {
		log.Printf("rviz launcher: execute template: %v", err)
	}
}

const rvizLauncherTemplate = `#!/bin/bash
# Auto-generated by OpenRobotFleet. Launches RViz on a group's ROS_DOMAIN_ID
# so the lab manager can inspect any group without disturbing the others.
#
# Usage:
#   rviz-domain list              # show known groups and their domain IDs
#   rviz-domain <group-name>      # launch RViz on that group's domain
#
# Multiple groups can be inspected at once:
#   rviz-domain group-1 & rviz-domain group-2 &

set -e

case "$1" in
{{- range .Groups}}
  {{.Name}}) exec env ROS_DOMAIN_ID={{.ROSDomainID}} rviz2 ;;
{{- end}}
  list)
{{- range .Groups}}
    echo "{{.Name}} (domain {{.ROSDomainID}})"
{{- end}}
    ;;
  *)
    echo "Usage: rviz-domain <group-name|list>"
    exit 1
    ;;
esac
`
