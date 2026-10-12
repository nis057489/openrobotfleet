package controller

import (
	"log"
	"net/http"
	"regexp"
	"sort"
)

// buildStamp is the build-farm timestamp ending a ROS package version
// ("2.4.1-1jammy.20240607.103020"). Each architecture is built separately, so
// the same release carries a different stamp on an arm64 robot than on an
// amd64 laptop; versions are compared without it.
var buildStamp = regexp.MustCompile(`\.\d{8}\.\d{6}$`)

func releaseVersion(v string) string { return buildStamp.ReplaceAllString(v, "") }

// RecordPackages stores a device's installed ros-* packages; nil (the
// heartbeat did not carry the list) keeps the previous one.
func (c *Controller) RecordPackages(agentID string, versions map[string]string) {
	if versions == nil {
		return
	}
	c.packageReportsMu.Lock()
	defer c.packageReportsMu.Unlock()
	if c.packageReports == nil {
		c.packageReports = map[string]map[string]string{}
	}
	c.packageReports[agentID] = versions
}

type packageDiff struct {
	Package string `json:"package"`
	Robot   string `json:"robot"`
	Laptop  string `json:"laptop"`
}

type groupPackageSync struct {
	GroupID int64 `json:"group_id"`
	// in_sync, differ, unknown (a device has not reported its packages
	// yet) or incomplete (the group lacks a robot or a laptop).
	State     string        `json:"state"`
	Differing []packageDiff `json:"differing,omitempty"`
}

// diffPackages lists packages installed on both devices at different
// releases. Packages on only one side are expected: robots and laptops
// install different stacks.
func diffPackages(robot, laptop map[string]string) []packageDiff {
	var diffs []packageDiff
	for pkg, rv := range robot {
		if lv, ok := laptop[pkg]; ok && releaseVersion(rv) != releaseVersion(lv) {
			diffs = append(diffs, packageDiff{Package: pkg, Robot: rv, Laptop: lv})
		}
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Package < diffs[j].Package })
	return diffs
}

// GetPackageSync reports, for each group, whether its robot and laptop run
// the same releases of the ROS packages they share.
func (c *Controller) GetPackageSync(w http.ResponseWriter, r *http.Request) {
	groups, err := c.DB.ListGroups(r.Context())
	if err != nil {
		log.Printf("list groups: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list groups")
		return
	}
	robots, err := c.DB.ListRobots(r.Context())
	if err != nil {
		log.Printf("list robots: %v", err)
		respondError(w, http.StatusInternalServerError, "failed to list robots")
		return
	}
	agentIDs := map[int64]string{}
	for _, robot := range robots {
		agentIDs[robot.ID] = robot.AgentID
	}

	c.packageReportsMu.RLock()
	defer c.packageReportsMu.RUnlock()
	result := make([]groupPackageSync, 0, len(groups))
	for _, g := range groups {
		s := groupPackageSync{GroupID: g.ID}
		if g.RobotID == nil || g.LaptopID == nil {
			s.State = "incomplete"
			result = append(result, s)
			continue
		}
		robotPkgs, rok := c.packageReports[agentIDs[*g.RobotID]]
		laptopPkgs, lok := c.packageReports[agentIDs[*g.LaptopID]]
		switch {
		case !rok || !lok:
			s.State = "unknown"
		default:
			s.Differing = diffPackages(robotPkgs, laptopPkgs)
			s.State = "in_sync"
			if len(s.Differing) > 0 {
				s.State = "differ"
			}
		}
		result = append(result, s)
	}
	respondJSON(w, http.StatusOK, result)
}
