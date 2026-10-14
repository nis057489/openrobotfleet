package agent

import (
	"bytes"
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

//go:embed indicator
var indicatorFiles embed.FS

// indicatorPackage provides the AppIndicator binding the indicator needs;
// Ubuntu's top bar shows AppIndicators out of the box, but Jammy doesn't
// ship this binding by default.
const indicatorPackage = "gir1.2-ayatanaappindicator3-0.1"

// indicatorInstall maps each embedded file to where it goes and its mode.
var indicatorInstall = map[string]struct {
	path string
	mode os.FileMode
}{
	"time_indicator.py":                     {"/usr/local/lib/openrobotfleet/time_indicator.py", 0o644},
	"openrobotfleet-time-indicator":         {"/usr/local/bin/openrobotfleet-time-indicator", 0o755},
	"openrobotfleet-time-indicator.desktop": {"/etc/xdg/autostart/openrobotfleet-time-indicator.desktop", 0o644},
	"time-green.svg":                        {"/usr/local/share/openrobotfleet/icons/time-green.svg", 0o644},
	"time-amber.svg":                        {"/usr/local/share/openrobotfleet/icons/time-amber.svg", 0o644},
	"time-red.svg":                          {"/usr/local/share/openrobotfleet/icons/time-red.svg", 0o644},
	"time-grey.svg":                         {"/usr/local/share/openrobotfleet/icons/time-grey.svg", 0o644},
}

// EnsureTimeIndicator installs the laptop top-bar indicator showing whether
// the laptop's clock agrees with its robot's (indicator/time_indicator.py),
// with buttons to resync. It starts at each user's next login. Laptops only:
// robots have no desktop. Runs at agent start; rewrites files only when they
// differ from this agent's copies.
func EnsureTimeIndicator(cfg Config) {
	if cfg.Type != "laptop" {
		return
	}
	if err := installIndicatorFiles("/"); err != nil {
		log.Printf("[agent] time indicator: %v", err)
		return
	}
	// In the background: apt can wait minutes for the dpkg lock at boot.
	go func() {
		out, _ := runCmd(defaultCmdTimeout, "dpkg-query", "-W", "-f", "${db:Status-Abbrev}", indicatorPackage)
		if strings.HasPrefix(string(out), "ii") {
			return
		}
		log.Printf("[agent] time indicator: installing %s", indicatorPackage)
		if _, err := aptGet("update"); err != nil {
			log.Printf("[agent] time indicator: %v", err)
			return
		}
		if _, err := aptGet("install", "-y", indicatorPackage); err != nil {
			log.Printf("[agent] time indicator: %v", err)
		}
	}()
}

// installIndicatorFiles writes the embedded files under root ("/" outside
// tests).
func installIndicatorFiles(root string) error {
	return fs.WalkDir(indicatorFiles, "indicator", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		target, ok := indicatorInstall[filepath.Base(name)]
		if !ok {
			return nil
		}
		data, err := indicatorFiles.ReadFile(name)
		if err != nil {
			return err
		}
		path := filepath.Join(root, target.path)
		if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
			if info, err := os.Stat(path); err == nil && info.Mode().Perm() == target.mode {
				return nil
			}
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, target.mode); err != nil {
			return err
		}
		if err := os.Chmod(path, target.mode); err != nil {
			return err
		}
		log.Printf("[agent] installed %s", target.path)
		return nil
	})
}
