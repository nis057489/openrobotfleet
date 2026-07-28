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
docker compose up -d mqtt
```

Run the Controller with `go run`. It needs `MQTT_BROKER`, `DB_PATH`, and `ADMIN_PASSWORD` at minimum:

```bash
export MQTT_BROKER=tcp://localhost:1883
export DB_PATH=./controller.db
export ADMIN_PASSWORD=turtle2025
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
AGENT_ID=dev1 AGENT_TYPE=robot MQTT_BROKER=tcp://localhost:1883 ./agent
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
  -e ADMIN_PASSWORD=turtle2025 \
  -e SCAN_SUBNETS=192.168.1.0/24 \
  -v controller-dev-data:/data \
  openrobotfleet-controller:dev
```

Pushing to `main` triggers the same build via [.github/workflows/docker.yml](.github/workflows/docker.yml) — building locally first lets you catch Dockerfile or cross-compilation issues before CI does.

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
