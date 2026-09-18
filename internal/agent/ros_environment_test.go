package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestROSCommandsUseCurrentGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ros_env.sh")
	inherited := []string{"PATH=/bin", "ROS_DOMAIN_ID=11", "RMW_IMPLEMENTATION=old", "CYCLONEDDS_URI=file://old"}
	for _, domain := range []string{"12", "15"} {
		os.WriteFile(path, []byte("export ROS_DOMAIN_ID="+domain+"\nexport RMW_IMPLEMENTATION=rmw_cyclonedds_cpp\n"), 0644)
		env, err := rosEnvironment(path, inherited)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(env, "ROS_DOMAIN_ID="+domain) || slices.Contains(env, "ROS_DOMAIN_ID=11") || slices.Contains(env, "CYCLONEDDS_URI=file://old") {
			t.Fatalf("stale group environment: %v", env)
		}
	}
	os.WriteFile(path, []byte("export UNEXPECTED=bad\n"), 0644)
	if _, err := rosEnvironment(path, inherited); err == nil {
		t.Fatal("unexpected configuration accepted")
	}
}

func TestCancelledDrivePublishesFinalStop(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "commands")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DRIVE_TEST_LOG\"\n"
	if err := os.WriteFile(filepath.Join(dir, "ros2"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("DRIVE_TEST_LOG", logPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- HandleTestDriveContext(ctx, Config{}, TestDriveData{DurationSec: 30}) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if data, _ := os.ReadFile(logPath); len(data) > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("drive did not cancel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("drive remained asleep")
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "x: 0.1") || !strings.Contains(lines[1], "x: 0.0") {
		t.Fatalf("missing final stop: %s", data)
	}
}
