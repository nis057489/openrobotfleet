package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	SQL  *sql.DB
	Path string
}

type Robot struct {
	ID            int64          `json:"id"`
	Name          string         `json:"name"`
	Type          string         `json:"type"`
	AgentID       string         `json:"agent_id"`
	IP            string         `json:"ip"`
	Status        string         `json:"status"`
	Notes         string         `json:"notes"`
	LastSeen      time.Time      `json:"last_seen"`
	LastScenario  *ScenarioRef   `json:"last_scenario,omitempty"`
	InstallConfig *InstallConfig `json:"install_config,omitempty"`
	Tags          []string       `json:"tags"`
}

type InstallConfig struct {
	Address string `json:"address"`
	User    string `json:"user"`
	SSHKey  string `json:"ssh_key"`
}

type ScenarioRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Scenario struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ConfigYAML  string `json:"config_yaml"`
}

type Job struct {
	ID          int64     `json:"id"`
	Type        string    `json:"type"`
	TargetRobot string    `json:"target_robot"`
	PayloadJSON string    `json:"payload_json"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type GoldenImageConfig struct {
	WifiSSID      string `json:"wifi_ssid"`
	WifiPassword  string `json:"wifi_password"`
	ControllerURL string `json:"controller_url"`
	MQTTBroker    string `json:"mqtt_broker"`
	LDSModel      string `json:"lds_model"`
	ROSDomainID   int    `json:"ros_domain_id"`
	RobotModel    string `json:"robot_model"` // "TB3" or "TB4"
	ROSVersion    string `json:"ros_version"` // "Humble" or "Jazzy"
}

type LoginEvent struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
}

const (
	defaultInstallConfigKey = "default_install_config"
	goldenImageConfigKey    = "golden_image_config"
)

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		return nil, err
	}
	// modernc SQLite creates new connections per goroutine unless capped; keep it at 1
	// to avoid unexpected SQLITE_BUSY errors since we don't need parallel writers yet.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	if err := migrate(db); err != nil {
		return nil, err
	}
	return &DB{SQL: db, Path: path}, nil
}

func migrate(db *sql.DB) error {
	ctx := context.Background()
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS robots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			agent_id TEXT,
			ip TEXT,
			last_seen TIMESTAMP,
			status TEXT,
			notes TEXT,
			last_scenario_id INTEGER,
			ssh_address TEXT,
			ssh_user TEXT,
			ssh_key TEXT,
			type TEXT DEFAULT 'robot'
		);`,
		`CREATE TABLE IF NOT EXISTS scenarios (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			description TEXT,
			config_yaml TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			target_robot TEXT,
			payload_json TEXT,
			status TEXT,
			created_at TIMESTAMP,
			updated_at TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS login_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp TIMESTAMP,
			ip TEXT,
			user_agent TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS interest_signups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL,
			timestamp TIMESTAMP,
			ip TEXT
		);`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			log.Printf("migration failed: %v", err)
			return err
		}
	}
	if err := ensureRobotSchema(db); err != nil {
		return err
	}
	return nil
}

func ensureRobotSchema(db *sql.DB) error {
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `ALTER TABLE robots ADD COLUMN last_scenario_id INTEGER`); err != nil {
		if !isDuplicateColumnError(err) {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE robots ADD COLUMN ssh_address TEXT`); err != nil {
		if !isDuplicateColumnError(err) {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE robots ADD COLUMN ssh_user TEXT`); err != nil {
		if !isDuplicateColumnError(err) {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE robots ADD COLUMN ssh_key TEXT`); err != nil {
		if !isDuplicateColumnError(err) {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE robots ADD COLUMN tags TEXT`); err != nil {
		if !isDuplicateColumnError(err) {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE robots ADD COLUMN type TEXT DEFAULT 'robot'`); err != nil {
		if !isDuplicateColumnError(err) {
			return err
		}
	}
	return nil
}

func isDuplicateColumnError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate column name")
}

func buildInstallConfig(addr, user, key sql.NullString) *InstallConfig {
	if !addr.Valid && !user.Valid && !key.Valid {
		return nil
	}
	cfg := InstallConfig{}
	if addr.Valid {
		cfg.Address = addr.String
	}
	if user.Valid {
		cfg.User = user.String
	}
	if key.Valid {
		cfg.SSHKey = key.String
	}
	if cfg.Address == "" && cfg.User == "" && cfg.SSHKey == "" {
		return nil
	}
	return &cfg
}

func (d *DB) ListRobots(ctx context.Context) ([]Robot, error) {
	stmt, err := d.SQL.PrepareContext(ctx, `SELECT r.id, r.name, r.agent_id, r.ip, r.last_seen, r.status, r.notes, s.id, s.name, r.ssh_address, r.ssh_user, r.ssh_key, r.tags, r.type
FROM robots r
LEFT JOIN scenarios s ON s.id = r.last_scenario_id
ORDER BY r.name`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var robots []Robot
	for rows.Next() {
		var r Robot
		var lastSeen sql.NullTime
		var notes sql.NullString
		var scenarioID sql.NullInt64
		var scenarioName sql.NullString
		var sshAddr, sshUser, sshKey sql.NullString
		var tags sql.NullString
		var rType sql.NullString
		if err := rows.Scan(&r.ID, &r.Name, &r.AgentID, &r.IP, &lastSeen, &r.Status, &notes, &scenarioID, &scenarioName, &sshAddr, &sshUser, &sshKey, &tags, &rType); err != nil {
			return nil, err
		}
		if lastSeen.Valid {
			r.LastSeen = lastSeen.Time
		}
		if notes.Valid {
			r.Notes = notes.String
		}
		if scenarioID.Valid {
			r.LastScenario = &ScenarioRef{ID: scenarioID.Int64, Name: scenarioName.String}
		}
		if tags.Valid && tags.String != "" {
			r.Tags = strings.Split(tags.String, ",")
		} else {
			r.Tags = []string{}
		}
		if rType.Valid {
			r.Type = rType.String
		} else {
			r.Type = "robot"
		}
		r.InstallConfig = buildInstallConfig(sshAddr, sshUser, sshKey)

		// Check for offline status
		if !r.LastSeen.IsZero() && time.Since(r.LastSeen) > 1*time.Minute {
			r.Status = "offline"
		} else if r.LastSeen.IsZero() {
			r.Status = "unknown"
		}

		robots = append(robots, r)
	}
	if robots == nil {
		robots = []Robot{}
	}
	return robots, rows.Err()
}

func (d *DB) UpsertRobotStatus(ctx context.Context, agentID, name, ip, status, rType string) error {
	if name == "" {
		return errors.New("robot name required")
	}
	stmt, err := d.SQL.PrepareContext(ctx, `INSERT INTO robots (name, agent_id, ip, last_seen, status, type) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
	agent_id=excluded.agent_id,
	ip=excluded.ip,
	status=excluded.status,
	last_seen=excluded.last_seen,
	type=CASE WHEN excluded.type != '' THEN excluded.type ELSE robots.type END`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.ExecContext(ctx, name, agentID, ip, time.Now().UTC(), status, rType)
	return err
}

func (d *DB) UpsertRobotWithType(ctx context.Context, agentID, name, ip, status, rType string) error {
	if name == "" {
		return errors.New("robot name required")
	}
	stmt, err := d.SQL.PrepareContext(ctx, `INSERT INTO robots (name, agent_id, ip, last_seen, status, type) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
	agent_id=excluded.agent_id,
	ip=excluded.ip,
	status=excluded.status,
	last_seen=excluded.last_seen,
	type=excluded.type`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.ExecContext(ctx, name, agentID, ip, time.Now().UTC(), status, rType)
	return err
}

func (d *DB) GetRobotByID(ctx context.Context, id int64) (Robot, error) {
	stmt, err := d.SQL.PrepareContext(ctx, `SELECT r.id, r.name, r.agent_id, r.ip, r.last_seen, r.status, r.notes, s.id, s.name, r.ssh_address, r.ssh_user, r.ssh_key, r.tags, r.type
FROM robots r
LEFT JOIN scenarios s ON s.id = r.last_scenario_id
WHERE r.id = ?`)
	if err != nil {
		return Robot{}, err
	}
	defer stmt.Close()
	var r Robot
	var lastSeen sql.NullTime
	var notes sql.NullString
	var scenarioID sql.NullInt64
	var scenarioName sql.NullString
	var sshAddr, sshUser, sshKey sql.NullString
	var tags sql.NullString
	var rType sql.NullString
	if err := stmt.QueryRowContext(ctx, id).Scan(&r.ID, &r.Name, &r.AgentID, &r.IP, &lastSeen, &r.Status, &notes, &scenarioID, &scenarioName, &sshAddr, &sshUser, &sshKey, &tags, &rType); err != nil {
		return Robot{}, err
	}
	if lastSeen.Valid {
		r.LastSeen = lastSeen.Time
	}
	if notes.Valid {
		r.Notes = notes.String
	}
	if scenarioID.Valid {
		r.LastScenario = &ScenarioRef{ID: scenarioID.Int64, Name: scenarioName.String}
	}
	if tags.Valid && tags.String != "" {
		r.Tags = strings.Split(tags.String, ",")
	} else {
		r.Tags = []string{}
	}
	if rType.Valid {
		r.Type = rType.String
	} else {
		r.Type = "robot"
	}
	r.InstallConfig = buildInstallConfig(sshAddr, sshUser, sshKey)

	// Check for offline status
	if !r.LastSeen.IsZero() && time.Since(r.LastSeen) > 1*time.Minute {
		r.Status = "offline"
	} else if r.LastSeen.IsZero() {
		r.Status = "unknown"
	}

	return r, nil
}

func (d *DB) GetRobotByName(ctx context.Context, name string) (Robot, error) {
	stmt, err := d.SQL.PrepareContext(ctx, `SELECT r.id, r.name, r.agent_id, r.ip, r.last_seen, r.status, r.notes, s.id, s.name, r.ssh_address, r.ssh_user, r.ssh_key, r.tags, r.type
FROM robots r
LEFT JOIN scenarios s ON s.id = r.last_scenario_id
WHERE r.name = ?`)
	if err != nil {
		return Robot{}, err
	}
	defer stmt.Close()
	var r Robot
	var lastSeen sql.NullTime
	var notes sql.NullString
	var scenarioID sql.NullInt64
	var scenarioName sql.NullString
	var sshAddr, sshUser, sshKey sql.NullString
	var tags sql.NullString
	var rType sql.NullString
	if err := stmt.QueryRowContext(ctx, name).Scan(&r.ID, &r.Name, &r.AgentID, &r.IP, &lastSeen, &r.Status, &notes, &scenarioID, &scenarioName, &sshAddr, &sshUser, &sshKey, &tags, &rType); err != nil {
		return Robot{}, err
	}
	if lastSeen.Valid {
		r.LastSeen = lastSeen.Time
	}
	if notes.Valid {
		r.Notes = notes.String
	}
	if scenarioID.Valid {
		r.LastScenario = &ScenarioRef{ID: scenarioID.Int64, Name: scenarioName.String}
	}
	if tags.Valid && tags.String != "" {
		r.Tags = strings.Split(tags.String, ",")
	} else {
		r.Tags = []string{}
	}
	if rType.Valid {
		r.Type = rType.String
	} else {
		r.Type = "robot"
	}
	r.InstallConfig = buildInstallConfig(sshAddr, sshUser, sshKey)
	return r, nil
}

func (d *DB) GetRobotByAgentID(ctx context.Context, agentID string) (Robot, error) {
	stmt, err := d.SQL.PrepareContext(ctx, `SELECT r.id, r.name, r.agent_id, r.ip, r.last_seen, r.status, r.notes, s.id, s.name, r.ssh_address, r.ssh_user, r.ssh_key, r.tags, r.type
FROM robots r
LEFT JOIN scenarios s ON s.id = r.last_scenario_id
WHERE r.agent_id = ?`)
	if err != nil {
		return Robot{}, err
	}
	defer stmt.Close()
	var r Robot
	var lastSeen sql.NullTime
	var notes sql.NullString
	var scenarioID sql.NullInt64
	var scenarioName sql.NullString
	var sshAddr, sshUser, sshKey sql.NullString
	var tags sql.NullString
	var rType sql.NullString
	if err := stmt.QueryRowContext(ctx, agentID).Scan(&r.ID, &r.Name, &r.AgentID, &r.IP, &lastSeen, &r.Status, &notes, &scenarioID, &scenarioName, &sshAddr, &sshUser, &sshKey, &tags, &rType); err != nil {
		return Robot{}, err
	}
	if lastSeen.Valid {
		r.LastSeen = lastSeen.Time
	}
	if notes.Valid {
		r.Notes = notes.String
	}
	if scenarioID.Valid {
		r.LastScenario = &ScenarioRef{ID: scenarioID.Int64, Name: scenarioName.String}
	}
	if tags.Valid && tags.String != "" {
		r.Tags = strings.Split(tags.String, ",")
	} else {
		r.Tags = []string{}
	}
	if rType.Valid {
		r.Type = rType.String
	} else {
		r.Type = "robot"
	}
	r.InstallConfig = buildInstallConfig(sshAddr, sshUser, sshKey)
	return r, nil
}

func (d *DB) UpdateRobotName(ctx context.Context, id int64, name string) error {
	stmt, err := d.SQL.PrepareContext(ctx, `UPDATE robots SET name = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.ExecContext(ctx, name, id)
	return err
}

func (d *DB) UpdateRobotScenario(ctx context.Context, robotID, scenarioID int64) error {
	stmt, err := d.SQL.PrepareContext(ctx, `UPDATE robots SET last_scenario_id = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	var val interface{}
	if scenarioID > 0 {
		val = scenarioID
	}
	_, err = stmt.ExecContext(ctx, val, robotID)
	return err
}

func (d *DB) UpdateRobotInstallConfigByID(ctx context.Context, robotID int64, cfg InstallConfig) error {
	stmt, err := d.SQL.PrepareContext(ctx, `UPDATE robots SET ssh_address = ?, ssh_user = ?, ssh_key = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.ExecContext(ctx, cfg.Address, cfg.User, cfg.SSHKey, robotID)
	return err
}

func (d *DB) UpdateRobotInstallConfigByName(ctx context.Context, name string, cfg InstallConfig) error {
	stmt, err := d.SQL.PrepareContext(ctx, `UPDATE robots SET ssh_address = ?, ssh_user = ?, ssh_key = ? WHERE name = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.ExecContext(ctx, cfg.Address, cfg.User, cfg.SSHKey, name)
	return err
}

func (d *DB) UpdateRobotTags(ctx context.Context, id int64, tags []string) error {
	tagStr := strings.Join(tags, ",")
	_, err := d.SQL.ExecContext(ctx, `UPDATE robots SET tags = ? WHERE id = ?`, tagStr, id)
	return err
}

func (d *DB) GetDefaultInstallConfig(ctx context.Context) (*InstallConfig, error) {
	var val sql.NullString
	err := d.SQL.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, defaultInstallConfigKey).Scan(&val)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if !val.Valid || val.String == "" {
		return nil, nil
	}
	var cfg InstallConfig
	if err := json.Unmarshal([]byte(val.String), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (d *DB) SaveDefaultInstallConfig(ctx context.Context, cfg InstallConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = d.SQL.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, defaultInstallConfigKey, string(data))
	return err
}

func (d *DB) GetGoldenImageConfig(ctx context.Context) (*GoldenImageConfig, error) {
	var val sql.NullString
	err := d.SQL.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, goldenImageConfigKey).Scan(&val)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if !val.Valid || val.String == "" {
		return nil, nil
	}
	var cfg GoldenImageConfig
	if err := json.Unmarshal([]byte(val.String), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (d *DB) SaveGoldenImageConfig(ctx context.Context, cfg GoldenImageConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = d.SQL.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, goldenImageConfigKey, string(data))
	return err
}

func (d *DB) ListScenarios(ctx context.Context) ([]Scenario, error) {
	stmt, err := d.SQL.PrepareContext(ctx, `SELECT id, name, description, config_yaml FROM scenarios ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	rows, err := stmt.QueryContext(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scenarios []Scenario
	for rows.Next() {
		var s Scenario
		if err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.ConfigYAML); err != nil {
			return nil, err
		}
		scenarios = append(scenarios, s)
	}
	if scenarios == nil {
		scenarios = []Scenario{}
	}
	return scenarios, rows.Err()
}

func (d *DB) GetScenarioByID(ctx context.Context, id int64) (Scenario, error) {
	stmt, err := d.SQL.PrepareContext(ctx, `SELECT id, name, description, config_yaml FROM scenarios WHERE id = ?`)
	if err != nil {
		return Scenario{}, err
	}
	defer stmt.Close()
	var s Scenario
	if err := stmt.QueryRowContext(ctx, id).Scan(&s.ID, &s.Name, &s.Description, &s.ConfigYAML); err != nil {
		return Scenario{}, err
	}
	return s, nil
}

func (d *DB) CreateScenario(ctx context.Context, s Scenario) (int64, error) {
	stmt, err := d.SQL.PrepareContext(ctx, `INSERT INTO scenarios (name, description, config_yaml) VALUES (?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	res, err := stmt.ExecContext(ctx, s.Name, s.Description, s.ConfigYAML)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (d *DB) UpdateScenario(ctx context.Context, s Scenario) error {
	stmt, err := d.SQL.PrepareContext(ctx, `UPDATE scenarios SET name = ?, description = ?, config_yaml = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.ExecContext(ctx, s.Name, s.Description, s.ConfigYAML, s.ID)
	return err
}

func (d *DB) DeleteScenario(ctx context.Context, id int64) error {
	stmt, err := d.SQL.PrepareContext(ctx, `DELETE FROM scenarios WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.ExecContext(ctx, id)
	return err
}

func (d *DB) CreateJob(ctx context.Context, j Job) (int64, error) {
	if j.CreatedAt.IsZero() {
		j.CreatedAt = time.Now().UTC()
	}
	if j.UpdatedAt.IsZero() {
		j.UpdatedAt = j.CreatedAt
	}
	stmt, err := d.SQL.PrepareContext(ctx, `INSERT INTO jobs (type, target_robot, payload_json, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	res, err := stmt.ExecContext(ctx, j.Type, j.TargetRobot, j.PayloadJSON, j.Status, j.CreatedAt, j.UpdatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (d *DB) UpdateJobStatus(ctx context.Context, id int64, status string) error {
	stmt, err := d.SQL.PrepareContext(ctx, `UPDATE jobs SET status = ?, updated_at = ? WHERE id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.ExecContext(ctx, status, time.Now().UTC(), id)
	return err
}

func (d *DB) ListJobs(ctx context.Context, target string) ([]Job, error) {
	var (
		stmt *sql.Stmt
		err  error
	)
	if target != "" {
		stmt, err = d.SQL.PrepareContext(ctx, `SELECT id, type, target_robot, payload_json, status, created_at, updated_at FROM jobs WHERE target_robot = ? ORDER BY created_at DESC`)
	} else {
		stmt, err = d.SQL.PrepareContext(ctx, `SELECT id, type, target_robot, payload_json, status, created_at, updated_at FROM jobs ORDER BY created_at DESC`)
	}
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	var rows *sql.Rows
	if target != "" {
		rows, err = stmt.QueryContext(ctx, target)
	} else {
		rows, err = stmt.QueryContext(ctx)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		var j Job
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(&j.ID, &j.Type, &j.TargetRobot, &j.PayloadJSON, &j.Status, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if createdAt.Valid {
			j.CreatedAt = createdAt.Time
		}
		if updatedAt.Valid {
			j.UpdatedAt = updatedAt.Time
		}
		jobs = append(jobs, j)
	}
	if jobs == nil {
		jobs = []Job{}
	}
	return jobs, rows.Err()
}

func (db *DB) RecordLogin(ctx context.Context, ip, userAgent string) error {
	query := `INSERT INTO login_events (timestamp, ip, user_agent) VALUES (?, ?, ?)`
	_, err := db.SQL.ExecContext(ctx, query, time.Now(), ip, userAgent)
	return err
}

func (db *DB) RecordInterest(ctx context.Context, email, ip string) error {
	query := `INSERT INTO interest_signups (email, timestamp, ip) VALUES (?, ?, ?)`
	_, err := db.SQL.ExecContext(ctx, query, email, time.Now(), ip)
	return err
}

func (d *DB) DeleteRobot(ctx context.Context, id int64) error {
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM robots WHERE id = ?`, id)
	return err
}
