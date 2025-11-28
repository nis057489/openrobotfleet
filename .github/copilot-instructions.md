# Turtlebot Fleet Starter – Copilot Instructions

## Architecture & responsibilities
- `cmd/controller` wires HTTP + MQTT + DB; `internal/http/server.go` manually handles routes and immediately subscribes to `lab/status/#` to upsert robot heartbeats via `internal/db.UpsertRobotStatus`.
- `cmd/agent` runs on robots, listens to `lab/commands/<agent_id>` and `lab/commands/all`, and sends 10s heartbeats to `lab/status/...`; new commands must be JSON-compatible with `internal/agent/commands.go`.
- The React dashboard in `web/` always fetches same-origin `/api/...` (see `web/src/api.ts`); when running the Vite dev server use `WEB_ROOT=<dev url>` or proxy so the Go server keeps handling API + MQTT.
- Docker Compose (`docker-compose.yml`) brings up Mosquitto + controller; the controller image (see `Dockerfile.controller`) builds both Go binaries so remote installs can scp the agent.

## Persistence & data flow
- SQLite is accessed through `internal/db` using ModernC (no CGO); `Open` enables WAL and forces `SetMaxOpenConns(1)` to avoid `SQLITE_BUSY`, so keep DB usage serialized or reuse the helper methods.
- Schema migrations live inline in `internal/db` (`migrate` + `ensureRobotSchema`); any new columns should follow the duplicate-safe pattern that keeps existing DBs usable.
- Robots/scenarios/jobs/settings each have CRUD helpers; job creation always goes through `CreateJob` before MQTT publish so history in `/api/jobs?robot=...` stays consistent.

## Controller patterns
- `internal/http.Server` exposes plain `net/http` handlers with manual path parsing helpers (`parseIDFromPath`, `parseCommandRobotID`, etc.); stick with that style to keep routing simple.
- Every controller action logs a short prefix and returns `{ "error": "..." }` on failure—reuse `respondJSON`/`respondError`.
- `internal/controller.ApplyScenario` parses YAML with `internal/scenario.Parse`, converts to `agent.UpdateRepoData`, queues commands per robot, and updates `robots.last_scenario_id`—mirror this flow for any multi-robot batch operations.

## MQTT + jobs
- All outbound commands are stored as `db.Job` rows then published via `internal/controller.queueRobotCommand`; avoid publishing directly so MQTT and DB never drift.
- Topics are fixed: per-robot `lab/commands/<agent_id>`, broadcast `lab/commands/all`, heartbeats `lab/status/<agent_id>`; if you add new topics document them in `README.md` and here.
- The MQTT client (`internal/mqtt/mqtt.go`) silently logs connect/subscribe errors—defensive coding should handle `nil` clients gracefully.

## Agent behavior
- Command handlers live in `internal/agent/actions.go`; `HandleUpdateRepo` wipes the destination before cloning and will `chown -R` when running as root, so validate paths before enqueuing `update_repo`.
- `HandleResetLogs` defaults to `workspace/logs` when no paths are provided; keep workspace-relative paths in controller payloads to avoid truncating arbitrary files.
- `HandleRestartROS` defers to `ROS_RESTART_CMD` or `ROS_SERVICE_NAME`; scenario/command authors should prefer these env overrides instead of hard-coding services.
- Agent config (`internal/agent/config.go`) is YAML only—remote installs rely on this schema when writing `/etc/turtlebot-agent/config.yaml`.

## Remote install workflow
- `/api/install-agent` (`internal/controller/install_agent.go`) reads the compiled agent from `AGENT_BINARY_PATH`, generates config using `AGENT_WORKSPACE_PATH` / owner envs, and calls `internal/ssh.InstallAgent`.
- `InstallAgent` uploads binaries via SFTP, then runs `systemctl daemon-reload && enable && restart` (see `internal/ssh/ssh.go`); when `UseSudo` is true it stages files in `/tmp` and moves them with `install -D`.
- Default SSH credentials for new robots live in the `settings` table under `default_install_config`; use `/api/settings/install-defaults` to maintain them so the UI pre-fills installers.

## Frontend conventions
- Types shared with the API live in `web/src/types.ts`; keep property names snake_case to match the backend JSON directly.
- Network errors throw with full status text (`web/src/api.ts`), so surface actionable server errors (`respondError`) to help UI toasts.
- `npm --prefix web install` and `npm --prefix web run build` are how the Go controller bundles static assets; avoid changing the output path without updating `WEB_ROOT`.

## Local workflows
- Backend: `MQTT_BROKER=tcp://localhost:1883 DB_PATH=controller.db go run ./cmd/controller`.
- Agent: `AGENT_CONFIG_PATH=$PWD/agent.local.yaml go run ./cmd/agent`.
- Web dev: `cd web && npm install && npm run dev -- --host 0.0.0.0 --port 5173`; point `WEB_ROOT` at `http://localhost:5173` (or use a Vite proxy) for hot reloads.
- Tests: `go test ./...` exercises both controller and agent packages; frontend lint/build via `npm --prefix web run build`.
- End-to-end smoke: `docker compose up --build` to run Mosquitto + controller + static UI, with controller data persisted in the `controller-data` volume.
