package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTimeConfigContentPollsEveryMinute(t *testing.T) {
	if c := timesyncdContent("192.168.100.1"); !strings.Contains(c, "\nNTP=192.168.100.1\n") || !strings.Contains(c, "\nPollIntervalMaxSec=64\n") || !strings.Contains(c, "\nFallbackNTP=\n") {
		t.Errorf("timesyncd drop-in:\n%s", c)
	}
	if c := chronySourcesContent("192.168.100.1"); !strings.Contains(c, "server 192.168.100.1 iburst maxpoll 6\n") {
		t.Errorf("chrony sources:\n%s", c)
	}
}

func TestWriteOrRemoveReportsChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d", "openrobot.conf")
	old := "# Managed by openrobot-agent (configure_time)\n[Time]\nNTP=192.168.100.1\nFallbackNTP=\n"
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(old), 0o644)
	if server := scanValue(path, "NTP="); server != "192.168.100.1" {
		t.Fatalf("scanValue = %q", server)
	}
	for i, want := range []bool{true, false} { // an older drop-in is rewritten once
		changed, err := writeOrRemove(path, "192.168.100.1", timesyncdContent("192.168.100.1"))
		if err != nil || changed != want {
			t.Fatalf("write %d: changed=%v err=%v, want %v", i, changed, err, want)
		}
	}
	if changed, _ := writeOrRemove(path, "", ""); !changed {
		t.Fatal("removal not reported")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file not removed")
	}
}
