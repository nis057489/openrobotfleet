package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

func (d *DB) GetJob(ctx context.Context, id int64) (Job, error) {
	var j Job
	err := d.SQL.QueryRowContext(ctx, `SELECT id,type,target_robot,payload_json,status,created_at,updated_at,error FROM jobs WHERE id=?`, id).
		Scan(&j.ID, &j.Type, &j.TargetRobot, &j.PayloadJSON, &j.Status, &j.CreatedAt, &j.UpdatedAt, &j.Error)
	return j, err
}

// RecordJobResult checks ownership and preserves terminal states against late
// running/pending heartbeats. Scenario labels change in the same transaction.
func (d *DB) RecordJobResult(ctx context.Context, agentID string, id int64, status, message string) error {
	switch status {
	case "pending", "running", "success", "failed":
	default:
		return errors.New("invalid job status")
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old, payload string
	err = tx.QueryRowContext(ctx, `SELECT status,payload_json FROM jobs WHERE id=? AND target_robot=?`, id, agentID).Scan(&old, &payload)
	if err != nil {
		return err
	}
	if old == "success" || old == "failed" {
		return tx.Commit()
	}
	if old == "running" && status == "pending" {
		return tx.Commit()
	}
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET status=?,error=?,updated_at=? WHERE id=?`, status, message, time.Now().UTC(), id); err != nil {
		return err
	}
	if status == "success" {
		var meta struct {
			ScenarioID int64 `json:"scenario_id"`
		}
		if json.Unmarshal([]byte(payload), &meta) == nil && meta.ScenarioID != 0 {
			if _, err = tx.ExecContext(ctx, `UPDATE robots SET last_scenario_id=? WHERE agent_id=?`, meta.ScenarioID, agentID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (d *DB) QueuedJobs(ctx context.Context, agentID string) ([]Job, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT id,type,payload_json,created_at FROM jobs WHERE target_robot=? AND status='queued' ORDER BY id`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		var j Job
		j.TargetRobot = agentID
		if err := rows.Scan(&j.ID, &j.Type, &j.PayloadJSON, &j.CreatedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// LatestJobID includes acknowledged and running jobs: an older queued desired
// state must not overwrite a newer command merely because it already finished.
func (d *DB) LatestJobID(ctx context.Context, agentID, jobType string) (int64, error) {
	var id int64
	err := d.SQL.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM jobs WHERE target_robot=? AND type=?`, agentID, jobType).Scan(&id)
	return id, err
}
