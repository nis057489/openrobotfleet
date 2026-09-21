package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSnapshotIncludesUncheckpointedWrites(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := Open(filepath.Join(dir, "live.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.SQL.Close()
	if err := d.UpsertRobotStatus(ctx, "robot-a", "robot-a", "10.0.0.1", "ok", "robot"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "backup.db")
	if err := d.Snapshot(ctx, path); err != nil {
		t.Fatal(err)
	}
	backup, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.SQL.Close()
	if _, err := backup.GetRobotByAgentID(ctx, "robot-a"); err != nil {
		t.Fatalf("snapshot missing recent write: %v", err)
	}
}
