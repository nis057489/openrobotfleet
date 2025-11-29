# Turtlebot Fleet Starter – Copilot Instructions

## Architecture & Responsibilities
- **Controller** (`cmd/controller`): Orchestrates the system. Wires HTTP, MQTT, and DB.
    - `internal/http/server.go`: Handles API routes. Manually parses paths (e.g., `parseIDFromPath`).
    - `internal/controller`: Business logic. `ApplyScenario` parses YAML, updates repo data, and queues commands.
- **Agent** (`cmd/agent`): Runs on robots.
    - Listens to `lab/commands/<agent_id>` and `lab/commands/all`.
    - Sends 10s heartbeats to `lab/status/<agent_id>`.
    - `internal/agent/actions.go`: Handles commands (update repo, reset logs, restart ROS).
- **Web** (`web/`): React dashboard.
    - Fetches same-origin `/api/...`.
    - `web/src/api.ts`: Centralized API client. Handles errors by throwing with server message.
- **Infrastructure**:
    - `docker-compose.yml`: Mosquitto + Controller.
    - `Dockerfile.controller`: Multi-stage build for controller and agent binaries.

## Data Flow & Persistence
- **SQLite** (`internal/db`):
    - Uses `modernc.org/sqlite` (no CGO).
    - `Open` enables WAL and sets `SetMaxOpenConns(1)` to prevent `SQLITE_BUSY`.
    - Migrations are inline in `internal/db/db.go` (`migrate`).
- **MQTT**:
    - Topics: `lab/commands/<agent_id>`, `lab/commands/all`, `lab/status/<agent_id>`.
    - **Critical**: All outbound commands must be stored as `db.Job` rows *before* publishing via `internal/controller.queueRobotCommand`.

## Developer Workflows
- **Full Stack**: `docker compose up --build` (Mosquitto + Controller + Static UI).
- **Backend Dev**:
    - `MQTT_BROKER=tcp://localhost:1883 DB_PATH=controller.db go run ./cmd/controller`
- **Agent Dev**:
    - `AGENT_CONFIG_PATH=$PWD/agent.local.yaml go run ./cmd/agent`
- **Frontend Dev**:
    - `cd web && npm install && npm run dev -- --host 0.0.0.0`
    - Set `WEB_ROOT=http://localhost:5173` for the controller to proxy or use Vite proxy.

## Conventions & Patterns
- **Routing**: Use `net/http` with manual path parsing helpers in `internal/controller`. Avoid external router libraries.
- **Error Handling**: Return `{ "error": "..." }` via `respondError`. Log short prefixes in controller.
- **Agent Config**: YAML only (`internal/agent/config.go`). Remote installs rely on this schema.
- **Frontend**: Keep `web/src/types.ts` in sync with backend JSON structs. Use snake_case for shared properties.

## Integration Points
- **Remote Install**: `/api/install-agent` reads compiled agent from `AGENT_BINARY_PATH`, generates config, and uses `internal/ssh` to upload/install.
- **Golden Image**: `/api/golden-image` endpoints manage standard OS images for robots.
