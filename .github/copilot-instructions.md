# Turtlebot Fleet – Copilot Instructions

## Project Overview
Turtlebot Fleet is a robotics fleet management system comprising a central **Controller**, distributed **Agents** (on robots/laptops), and a **Web Dashboard**. It orchestrates code deployment, status monitoring, and remote commands via MQTT and HTTP.

## Architecture & Components

### 1. Controller (`cmd/controller`)
- **Role**: Central brain. Manages state, serves API, bridges HTTP to MQTT.
- **Entry**: `cmd/controller/main.go`
- **HTTP Server**: `internal/http/server.go`. Uses `net/http` with **manual path parsing** (no external router).
- **Business Logic**: `internal/controller/`. Handlers are methods on `*Controller`.
- **Data**: SQLite via `internal/db`.
- **Communication**: Publishes to MQTT topics to control agents.

### 2. Agent (`cmd/agent`)
- **Role**: Runs on robots/laptops. Executes commands, reports status.
- **Entry**: `cmd/agent/main.go`
- **Config**: `internal/agent/config.go` (YAML).
- **Actions**: `internal/agent/actions.go`. Handles git operations, service restarts, log clearing.
- **Communication**: Subscribes to MQTT command topics, publishes heartbeats.

### 3. Web Dashboard (`web/`)
- **Role**: User interface for fleet management.
- **Stack**: React, Vite, Tailwind CSS.
- **API Client**: `web/src/api.ts`. Centralized fetch wrapper.
- **Types**: `web/src/types.ts` (Must match backend JSON structs).

### 4. Infrastructure
- **MQTT**: Mosquitto broker.
- **Database**: SQLite (`controller.db`).
- **Docker**: `docker-compose.yml` orchestrates the full stack.

## Data Flow & Communication

### Command Execution (User -> Robot)
1. **User** clicks button in Web UI.
2. **Web** calls HTTP API (e.g., `POST /api/robots/1/command`).
3. **Controller** (`internal/controller/robots.go`):
    - Validates request.
    - **Persists** command to DB (`db.Job`).
    - **Publishes** MQTT message to `lab/commands/<agent_id>`.
4. **Agent** (`cmd/agent/main.go`):
    - Receives MQTT message.
    - Executes action (`internal/agent/actions.go`).
    - Reports status back (optional/implicit via heartbeat).

### Status Reporting (Robot -> User)
1. **Agent** runs background ticker.
2. **Agent** publishes JSON payload to `lab/status/<agent_id>`.
3. **Controller** subscribes to `lab/status/+`.
4. **Controller** updates DB with last seen, battery, etc.
5. **Web** polls `/api/robots` to display latest status.

## Developer Workflows

### Running Locally (Backend)
```bash
# Run Controller (requires MQTT broker running, e.g., via docker-compose up mosquitto)
export MQTT_BROKER=tcp://localhost:1883
export DB_PATH=controller.db
go run ./cmd/controller

# Run Agent (simulated robot)
export AGENT_CONFIG_PATH=./agent.local.yaml
go run ./cmd/agent
```

### Running Locally (Frontend)
```bash
cd web
npm install
npm run dev
# Set WEB_ROOT in controller to point to dev server if needed, or use Vite proxy
```

### Database
- **Driver**: `modernc.org/sqlite` (Pure Go, no CGO).
- **Migrations**: Inline in `internal/db/db.go`.
- **Concurrency**: `SetMaxOpenConns(1)` is used to avoid `SQLITE_BUSY`.

## Conventions & Patterns

### Go (Backend)
- **Routing**: Do NOT use a router library. Use `http.NewServeMux` and helper functions like `parseIDFromPath` in `internal/controller`.
- **Error Handling**: Return JSON `{ "error": "message" }` using `respondError`.
- **Configuration**: Use environment variables for Controller, YAML for Agent.
- **MQTT Topics**:
    - Command: `lab/commands/<agent_id>` or `lab/commands/all`
    - Status: `lab/status/<agent_id>`

### TypeScript (Frontend)
- **API**: Always use `api.ts` for backend calls.
- **State**: React functional components with Hooks.
- **Styling**: Tailwind CSS utility classes.

## Key Files
- `internal/http/server.go`: API route definitions.
- `internal/controller/controller.go`: Shared controller logic & helpers.
- `internal/agent/actions.go`: Implementation of robot commands.
- `web/src/api.ts`: Frontend API definition.
- `docker-compose.yml`: Service orchestration.
