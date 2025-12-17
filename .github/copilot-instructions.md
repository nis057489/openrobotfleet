# OpenRobotFleet – Copilot Instructions

## Project Overview
OpenRobotFleet is a robotics fleet management system. It consists of a central **Controller**, distributed **Agents** (on robots/laptops), and a **Web Dashboard**. It orchestrates code deployment ("Scenarios"), status monitoring, and remote commands via MQTT and HTTP.

## Architecture & Components

### 1. Controller (`cmd/controller`)
- **Role**: Central brain. Manages state, serves API, bridges HTTP to MQTT.
- **Entry**: `cmd/controller/main.go`
- **HTTP Server**: `internal/http/server.go`. Uses `net/http` with **manual path parsing** (no external router).
- **Business Logic**: `internal/controller/`. Handlers are methods on `*Controller`.
- **Data**: SQLite via `internal/db`.
- **Discovery**: `internal/scan` uses ARP table analysis to find robots on the local network.

### 2. Agent (`cmd/agent`)
- **Role**: Runs on robots/laptops. Executes commands, reports status.
- **Architecture**: **Behavior Tree** driven (`internal/agent/engine.go`).
- **Entry**: `cmd/agent/main.go` initializes `AgentEngine`.
- **Logic**: `internal/agent/behavior/` defines nodes (Composites, Decorators, Leaves).
- **Execution**: The engine ticks the root node (Parallel) at 10Hz.
    - **Nodes**: `checkNetwork`, `processCommands`, `sendHeartbeat`.
- **Jobs**: `internal/agent/job_manager.go` handles long-running tasks (git pulls, installs) triggered by `processCommands`.

### 3. Web Dashboard (`web/`)
- **Role**: User interface for fleet management.
- **Stack**: React, Vite, Tailwind CSS.
- **API Client**: `web/src/api.ts`. Centralized fetch wrapper.
- **Types**: `web/src/types.ts` (Must match backend JSON structs).

## Data Flow & Communication

### Command Execution (User -> Robot)
1. **User** triggers action (e.g., "Deploy Scenario").
2. **Web** calls HTTP API (e.g., `POST /api/robots/1/command`).
3. **Controller** validates and publishes MQTT message to `lab/commands/<agent_id>`.
4. **Agent** (MQTT Handler) pushes command to `cmdChan`.
5. **Agent** (Behavior Tree `processCommands` node) picks up command on next tick.
6. **JobManager** executes the actual logic (e.g., `git pull`).

### Scenarios
- **Definition**: Declarative YAML defining repo, branch, and path (`internal/scenario/spec.go`).
- **Usage**: Used to batch-update robot code.

## Developer Workflows

### Running Locally
```bash
# 1. Infrastructure (MQTT)
docker compose up mosquitto

# 2. Controller
export MQTT_BROKER=tcp://localhost:1883
export DB_PATH=controller.db
go run ./cmd/controller

# 3. Agent (Simulated)
export AGENT_CONFIG_PATH=./agent.local.yaml
go run ./cmd/agent
```

### Database
- **Driver**: `modernc.org/sqlite` (Pure Go).
- **Migrations**: Inline in `internal/db/db.go`.

## Conventions & Patterns

### Go (Backend)
- **Routing**: Do NOT use a router library. Use `http.NewServeMux` and helper functions like `parseIDFromPath`.
- **Agent Logic**: Implement new agent behaviors as **Behavior Tree Nodes** in `internal/agent/behavior/`, not ad-hoc goroutines.
- **Error Handling**: Return JSON `{ "error": "message" }` using `respondError`.

### TypeScript (Frontend)
- **API**: Always use `api.ts` for backend calls.
- **State**: React functional components with Hooks.

## Key Files
- `internal/agent/engine.go`: Main agent loop and tree construction.
- `internal/agent/behavior/`: Behavior tree primitives.
- `internal/controller/controller.go`: Shared controller logic.
- `internal/scan/scan.go`: Network discovery logic.
- `web/src/api.ts`: Frontend API definition.
