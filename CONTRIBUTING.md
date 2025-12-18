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

We use Docker Compose for the infrastructure (MQTT, Database) but often run the Controller and Agent locally for faster iteration.

1. **Start Infrastructure**:

    ```bash
    docker compose up mosquitto
    ```

2. **Run Controller**:

    ```bash
    export MQTT_BROKER=tcp://localhost:1883
    export DB_PATH=controller.db
    go run ./cmd/controller
    ```

3. **Run Web Dashboard**:

    ```bash
    cd web
    npm install
    npm run dev
    ```

4. **Run Agent (Simulated)**:

    ```bash
    export AGENT_CONFIG_PATH=./agent.local.yaml
    go run ./cmd/agent
    ```

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
