# Turtlebot Fleet Starter

Turtlebot Fleet Starter is a batteries-included controller + agent stack for orchestrating a classroom or lab of Turtlebot-based robots. The Go controller exposes a REST + Web UI for observing robot health, pushing commands over MQTT, installing agents via SSH, and managing scenario definitions that sync code repositories onto each robot. A lightweight Go agent runs on every robot, reports status heartbeats, and executes commands (repo updates, log resets, ROS restarts) it receives from the controller.

## Key Capabilities

- Real-time robot inventory sourced from MQTT heartbeats and persisted in SQLite.
- Remote, repeatable agent installation over SSH with automated systemd setup and workspace ownership fixes.
- Scenario management that stores declarative repo/branch/path definitions and applies them to multiple robots at once.
- Command/job queue that targets individual robots or broadcasts to `lab/commands/all`, with history surfaced over HTTP.
- Zero-config web dashboard (React + Vite) for operators to install agents, manage scenarios, and trigger commands.

## Architecture at a Glance

```text
┌──────────────┐      HTTP/WebSockets      ┌───────────────┐
│ React Web UI │  <--------------------->  │ Go Controller │
└──────────────┘                            │  /api + MQTT  │
        ▲                                   ├───────────────┤
        │        lab/status/* heartbeats    │ SQLite (DB)   │
        │        lab/commands/* jobs        └───────────────┘
        │                                           ▲
        │                                           │
 ┌──────────────┐  MQTT broker (Mosquitto)   ┌──────────────┐
 │ Go Agent(s)  │ <------------------------> │  MQTT Topic  │
└──────────────┘                            └──────────────┘
```

- `cmd/controller` exposes the HTTP API, serves the built web assets, and speaks MQTT.
- `cmd/agent` runs on every robot, polls commands from MQTT, and posts heartbeats.
- `internal/db` provides a ModernC SQLite layer (no CGO) with migrations.
- `internal/ssh` handles agent installation via SFTP + remote systemd configuration.
- `web/` contains the React dashboard (Vite + TypeScript).

## Repository Layout

- `cmd/controller`: HTTP + MQTT controller entrypoint.
- `cmd/agent`: Robot-side agent binary.
- `internal/controller`: HTTP handlers, job queueing, scenario logic.
- `internal/agent`: Agent config parsing and command handling implementations.
- `internal/http`: Server wiring (routing, static assets, MQTT subscriptions).
- `internal/db`: SQLite models/migrations for robots, jobs, scenarios, settings.
- `internal/mqtt`: Thin wrapper around Eclipse Paho.
- `internal/scenario`: YAML spec parser that becomes agent `update_repo` payloads.
- `internal/ssh`: Remote install helper (SFTP + systemd service management).
- `web`: React UI (Vite) consumed by operators.

## Prerequisites

- Go 1.23+
- Node.js 20+ (or any runtime compatible with Vite 5) and npm.
- Docker + Docker Compose (optional but recommended for end-to-end smoke tests).
- An MQTT broker (Docker compose spins up Eclipse Mosquitto by default).

## Quick Start (Docker Compose)

1. Copy `mosquitto.conf` as needed and ensure port `1883` is free.
2. Build and launch the stack:

   ```sh
   docker compose up --build
   ```

3. Open `http://localhost:8080` to access the dashboard (static assets served by the controller container).
4. The controller stores its SQLite database under the `controller-data` volume; stop the stack with `Ctrl+C` when done.

## Local Development (without containers)

### Backend API

```sh
export MQTT_BROKER="tcp://localhost:1883"  # point to your broker
export DB_PATH="controller.db"
export WEB_ROOT="$(pwd)/web/dist"         # optional, see frontend section
npm --prefix web install && npm --prefix web run build
GO111MODULE=on go run ./cmd/controller
```

Visit `http://localhost:8080/healthz` for a liveness probe and `http://localhost:8080` for the bundled UI.

### Frontend (Vite Dev Server)

```sh
cd web
npm install
npm run dev -- --host 0.0.0.0 --port 5173
```

Set `WEB_ROOT` to the dev server URL if you prefer proxying through the controller, or access the Vite dev server directly while using `VITE_API_BASE=http://localhost:8080` in the browser (the default `fetch` calls same-origin `window.location`).

### Agent

1. Create a config file (`/etc/turtlebot-agent/config.yaml` in production):

   ```yaml
   agent_id: turtlebot-01
   mqtt_broker: tcp://localhost:1883
   workspace_path: /home/ubuntu/ros_ws
   workspace_owner: ubuntu
   ```

2. Run the agent:

   ```sh
   AGENT_CONFIG_PATH=$PWD/agent.local.yaml go run ./cmd/agent
   ```

The agent subscribes to `lab/commands/<agent_id>` and `lab/commands/all`, and posts status to `lab/status/<agent_id>` every 10 seconds.

## Configuration Reference

### Controller Environment Variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `DB_PATH` | `controller.db` | SQLite database path. |
| `MQTT_BROKER` | `tcp://192.168.100.122:1883` | Broker URL for controller-issued commands. |
| `HTTP_ADDR` | `:8080` | Bind address for the HTTP server. |
| `WEB_ROOT` | `./web/dist` | Directory served at `/` (built web UI). |
| `AGENT_BINARY_PATH` | `/app/agent` | Path to the agent binary pushed during remote installs. |
| `AGENT_MQTT_BROKER` | Inferred from `MQTT_PUBLIC_BROKER` or `MQTT_BROKER` | Broker URL that newly installed agents will use. |
| `MQTT_PUBLIC_BROKER` | _unset_ | Optional override (e.g., public IP) used for installers. |
| `AGENT_WORKSPACE_PATH` | `/home/ubuntu/ros_ws/src/course` | Default workspace passed to agents on install. |
| `AGENT_WORKSPACE_OWNER` | _unset_ | Force ownership (e.g., `ubuntu:ubuntu`) when installing. |
| `DEFAULT_WORKSPACE_OWNER` | `ubuntu` | Fallback owner if `AGENT_WORKSPACE_OWNER` is empty. |
| `AGENT_SUDO_PASSWORD` | `ubuntu` when sudo required | Used when remote installs need sudo privileges. |

### Agent Environment Variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `AGENT_CONFIG_PATH` | `/etc/turtlebot-agent/config.yaml` | Location of the YAML config consumed at startup. |
| `ROS_RESTART_CMD` | _unset_ (falls back to `systemctl restart <ROS_SERVICE_NAME>`) | Custom command the agent will run for `restart_ros`. |
| `ROS_SERVICE_NAME` | `ros` | Systemd service the agent restarts when no custom command is provided. |

### Agent Config Schema (`internal/agent/config.go`)

```yaml
agent_id: string        # required unique ID per robot
mqtt_broker: string     # e.g., tcp://broker:1883
workspace_path: string  # repo root used for git sync + log management
workspace_owner: string # chown target when installer runs as root
```

## MQTT Topics

- `lab/status/<agent_id>`: JSON heartbeats `{status, ts, ip, name}` consumed by the controller to upsert robot rows.
- `lab/commands/<agent_id>`: Individual robot job queue published by the controller.
- `lab/commands/all`: Broadcast channel for fleet-wide commands.

## REST/API Overview

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/healthz` | Simple controller health probe. |
| `GET` | `/api/robots` | List robots (status, last seen, scenario, install config). |
| `GET` | `/api/robots/:id` | Fetch a single robot. |
| `POST` | `/api/robots/:id/command` | Queue a command for a robot (`update_repo`, `reset_logs`, `restart_ros`, …). |
| `POST` | `/api/robots/command/broadcast` | Send the same command to `lab/commands/all`. |
| `PUT` | `/api/robots/:id/install-config` | Persist SSH install settings for a robot. |
| `GET` | `/api/scenarios` | List scenario definitions. |
| `POST` | `/api/scenarios` | Create a scenario (`name`, `description`, `config_yaml`). |
| `GET/PUT/DELETE` | `/api/scenarios/:id` | Inspect or mutate an existing scenario. |
| `POST` | `/api/scenarios/:id/apply` | Queue scenario repo syncs for `robot_ids` array. |
| `GET` | `/api/jobs?robot=<agent_id>` | View job history (optionally filtered by target). |
| `POST` | `/api/install-agent` | Push the agent binary + config over SSH and tag the robot record. |
| `GET/PUT` | `/api/settings/install-defaults` | Read/update global SSH defaults stored in the `settings` table. |

All endpoints accept/return JSON and surface errors as `{ "error": "message" }` with appropriate HTTP status codes.

## Scenario Spec (`config_yaml`)

Scenario configs are YAML documents parsed by `internal/scenario` and converted into `update_repo` commands. Minimal example:

```yaml
repo:
  url: https://github.com/acme/ros-course.git
  branch: lab1
  path: src/course
```

When applied, each target robot receives `{"type":"update_repo","data":{...}}` with normalized branch/path defaults.

## Installing Agents via the Controller

1. Save per-robot SSH defaults under Settings (optional).
2. Issue `POST /api/install-agent` with:

   ```json
   {
     "name": "turtlebot-01",
     "address": "192.168.1.101",
     "user": "ubuntu",
     "ssh_key": "-----BEGIN OPENSSH PRIVATE KEY-----...",
     "sudo": true
   }
   ```

3. The controller uploads the compiled agent binary, config, and systemd service, ensures ownership, restarts the service, and persists the SSH metadata for future installs.
4. Check `/api/robots` (or the UI) to confirm the robot is now `installed` and heartbeats are flowing.

## Jobs and Commands

- Every command (broadcast or per-robot) creates a `jobs` row with timestamps for traceability.
- Jobs are published to MQTT immediately; long-running execution happens on the robot.
- Use `/api/jobs` to audit what actions were triggered across the fleet.

## Web Dashboard Highlights

- **Robots tab**: table of robots, side panel for issuing commands (`update_repo`, `reset_logs`, `restart_ros`).
- **Scenarios tab**: CRUD UI for YAML specs, plus multi-select apply flow that queues commands and tags robots with their last scenario.
- **Install Agent tab**: wraps `/api/install-agent` with form validation.
- **Settings tab**: manages default SSH install config shared across robots.

## Development Workflow

- Format Go code with `go fmt ./...` and test with `go test ./...` (no CGO needed thanks to ModernC SQLite).
- Lint/format the web app via `npm run build` before committing to ensure type safety.
- The controller binary already bundles the agent binary for remote installs (`Dockerfile.controller` builds both).
- Mosquitto configuration lives in `mosquitto.conf`; adjust for authentication or TLS as needed.

## Troubleshooting

- **No robots listed**: verify agents are publishing to `lab/status/*` and the controller log shows `controller subscribing to lab/status/#`.
- **Install agent fails**: ensure SSH key has the correct permissions and `sudo` password is provided when necessary.
- **Commands not executing**: confirm `agent_id` matches between robot records and the agent config; jobs table should show a row per command.

## Next Steps

- Add authentication around the HTTP API and dashboard.
- Surface MQTT/agent logs inside the UI for easier debugging.
- Expand scenario specs beyond git syncs (e.g., ROS launch configurations, parameter uploads).
