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
See [CONTRIBUTING.md](../CONTRIBUTING.md) for the full walkthrough. Short version:
```bash
# 1. Infrastructure (MQTT broker service is named `mqtt`, not `mosquitto`)
docker compose up -d mqtt

# 2. Controller
export MQTT_BROKER=tcp://localhost:1883
export DB_PATH=./controller.db
export ADMIN_PASSWORD=turtle2025
go run ./cmd/controller

# 3. Web dashboard (separate terminal)
cd web && npm install && npm run dev

# 4. Agent (simulated, config via env vars — not a config file)
go build -o agent ./cmd/agent
AGENT_ID=dev1 AGENT_TYPE=robot MQTT_BROKER=tcp://localhost:1883 ./agent
```

### Container Images & CI
- `Dockerfile.controller` is a multi-stage build (Node web build → Go build → Debian runtime). The Go stage is **cross-compile aware**: it uses `--platform=$BUILDPLATFORM` plus `ARG TARGETOS`/`TARGETARCH` so `docker buildx` produces a correctly-architected `controller` binary per target platform. Don't reintroduce a hardcoded `GOOS=linux go build` for the controller binary — that silently ships an amd64 binary inside arm64 images.
- `Dockerfile.agent`, `Dockerfile.laptop`, `Dockerfile.robot` are separate images for those roles; only `Dockerfile.controller` is currently built by CI.
- `.github/workflows/docker.yml` builds `Dockerfile.controller` on push to `main` and on `v*` tags, for `linux/amd64` + `linux/arm64`, and pushes to `ghcr.io/nis057489/openrobotfleet-controller` (`latest`, `sha-<short>`, and `vX.Y.Z` tags).
- `docker-compose.yml`'s `controller` service pulls that GHCR image (`image:`), it does not `build:` locally. To test Dockerfile changes, build locally first: `docker build -f Dockerfile.controller -t openrobotfleet-controller:dev .` (see CONTRIBUTING.md).

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
