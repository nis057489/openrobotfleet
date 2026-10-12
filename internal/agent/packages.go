package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	packageRefreshInterval = 5 * time.Minute
	// packageResendInterval re-sends an unchanged list now and then, so a
	// restarted controller learns it again.
	packageResendInterval = 15 * time.Minute
)

// rosPackages tracks the installed ros-* packages, which the controller
// compares between a group's robot and laptop. The list is large, so it goes
// out in a heartbeat only when it changes or is due a resend.
type rosPackages struct {
	mu       sync.Mutex
	versions map[string]string
	hash     string
	sentHash string
	sentAt   time.Time
	refresh  chan struct{}
}

func newROSPackages() *rosPackages {
	return &rosPackages{refresh: make(chan struct{}, 1)}
}

// requestRefresh re-reads the package list soon, e.g. after an upgrade.
func (p *rosPackages) requestRefresh() {
	select {
	case p.refresh <- struct{}{}:
	default:
	}
}

func (p *rosPackages) watch(ctx context.Context) {
	ticker := time.NewTicker(packageRefreshInterval)
	defer ticker.Stop()
	for {
		p.load()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-p.refresh:
		}
	}
}

func (p *rosPackages) load() {
	out, err := runCmd(defaultCmdTimeout, "dpkg-query", "-W", "-f", "${db:Status-Abbrev}\t${Package}\t${Version}\n", "ros-*")
	if err != nil && len(out) == 0 {
		return // dpkg-query exits 1 when nothing matches, still printing matches if any
	}
	versions := parseInstalledPackages(string(out))
	keys := make([]string, 0, len(versions))
	for k := range versions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k + "=" + versions[k] + "\n"))
	}
	p.mu.Lock()
	p.versions, p.hash = versions, hex.EncodeToString(h.Sum(nil))
	p.mu.Unlock()
}

// parseInstalledPackages reads dpkg-query output, keeping only packages that
// are fully installed ("ii").
func parseInstalledPackages(out string) map[string]string {
	versions := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) == 3 && strings.TrimSpace(fields[0]) == "ii" {
			versions[fields[1]] = fields[2]
		}
	}
	return versions
}

// toSend returns the list when it changed since it was last sent or is due
// a resend, marking it sent; otherwise nil.
func (p *rosPackages) toSend() map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.versions == nil || (p.hash == p.sentHash && time.Since(p.sentAt) < packageResendInterval) {
		return nil
	}
	p.sentHash, p.sentAt = p.hash, time.Now()
	return p.versions
}
