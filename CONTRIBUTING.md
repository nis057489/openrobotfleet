# Contributing to OpenRobotFleet

Thank you for your interest in contributing to OpenRobotFleet! We welcome contributions from everyone, whether it's reporting a bug, suggesting a feature, or writing code.

## Getting Started

1. **Fork the repository** on GitHub.
2. **Clone your fork** locally.
3. **Install dependencies**:
    * Go 1.25+
    * Node.js 20+
    * Docker & Docker Compose

## Development Workflow

### Running Locally

#### Option A: run the backend and frontend directly

Start a broker (the repo's Mosquitto config is the easiest way):

```bash
cp .env.example .env
# Fill ADMIN_PASSWORD and two different random MQTT passwords in .env.
docker compose up -d mqtt
```

Run the Controller with `go run`. It needs `MQTT_BROKER`, `DB_PATH`, and `ADMIN_PASSWORD` at minimum:

```bash
set -a
. ./.env
set +a
export MQTT_BROKER=tcp://localhost:1883
export MQTT_USERNAME=controller
export MQTT_PASSWORD="$MQTT_CONTROLLER_PASSWORD"
export AGENT_MQTT_USERNAME=agents
export AGENT_MQTT_PASSWORD="$MQTT_AGENT_PASSWORD"
export DB_PATH=./controller.db
export SCAN_SUBNETS=192.168.1.0/24
go run ./cmd/controller
```

In a second terminal, run the frontend dev server against it (Vite proxies API calls to the Go server — check `web/vite.config.ts` if you need to point it at a different Controller address):

```bash
cd web
npm install
npm run dev
```

Open the URL Vite prints (typically `http://localhost:5173`).

The agent binary can be run the same way for testing against a local Controller:

```bash
go build -o agent ./cmd/agent
cat > /tmp/fleet-dev-agent.yaml <<'EOF'
agent_id: dev1
type: robot
mqtt_broker: tcp://localhost:1883
workspace_path: /tmp/fleet-dev-workspace
job_state_path: /tmp/fleet-dev-jobs.json
EOF
AGENT_CONFIG_PATH=/tmp/fleet-dev-agent.yaml \
  MQTT_USERNAME=agents MQTT_PASSWORD="$MQTT_AGENT_PASSWORD" ./agent
```

#### Option B: build the full container image locally

This mirrors exactly what CI builds and is the best way to test Dockerfile changes before pushing:

```bash
docker build -f Dockerfile.controller -t openrobotfleet-controller:dev .
```

To run the whole stack (mqtt, traefik, controller) from that local image instead of pulling from GHCR, temporarily point `docker-compose.yml`'s `controller.image` at your local tag, or override it inline:

```bash
docker compose up -d mqtt
docker run --rm -it \
  --network openrobotfleet_default \
  -p 8080:8080 \
  -e MQTT_BROKER=tcp://openrobot-mqtt:1883 \
  -e DB_PATH=/data/controller.db \
  -e ADMIN_PASSWORD \
  -e MQTT_USERNAME=controller -e MQTT_PASSWORD="$MQTT_CONTROLLER_PASSWORD" \
  -e AGENT_MQTT_USERNAME=agents -e AGENT_MQTT_PASSWORD="$MQTT_AGENT_PASSWORD" \
  -e SCAN_SUBNETS=192.168.1.0/24 \
  -v controller-dev-data:/data \
  openrobotfleet-controller:dev
```

Pushing to `main` triggers the same build via [.github/workflows/docker.yml](.github/workflows/docker.yml) — building locally first lets you catch Dockerfile or cross-compilation issues before CI does.

## Regression tests

```bash
go test -race ./...
go vet ./...
```

The MQTT reconnect test opens only a loopback socket and includes its own small
broker simulator. To test the actual Mosquitto authentication and ACLs without
connecting to the fleet, run this temporary container with networking disabled:

```bash
docker run --rm --network none --entrypoint /bin/sh \
  -e MQTT_CONTROLLER_PASSWORD=controller-test-only \
  -e MQTT_AGENT_PASSWORD=agents-test-only \
  -v "$PWD/mosquitto.conf:/mosquitto/config/mosquitto.conf:ro" \
  -v "$PWD/mqtt/acl:/mosquitto/config/acl:ro" \
  -v "$PWD/mqtt/entrypoint.sh:/mosquitto/config/fleet-entrypoint.sh:ro" \
  -v "$PWD/mqtt/test-acl.sh:/test-acl.sh:ro" \
  eclipse-mosquitto:2 /test-acl.sh
```

The broker uses [Mosquitto's password and topic ACL configuration](https://mosquitto.org/man/mosquitto-conf-5.html).
The shared agent account is intended for fleet members; client IDs identify topics,
not independently authenticated device identities. Never put the controller password
on a robot or in a golden image.

## Code Style

* **Go**: Follow standard Go conventions (`gofmt`, `go vet`).
* **TypeScript**: We use ESLint and Prettier. Run `npm run lint` in the `web` directory.
* **Fun**: Sunshine!

## Pull Requests

1. Create a new branch for your feature or fix.
2. Commit your changes with clear messages.
3. Push to your fork and submit a Pull Request.
4. Describe your changes and link to any relevant issues.

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
