FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags="-s -w" -trimpath -o agent ./cmd/agent

FROM alpine:latest
WORKDIR /app
COPY --from=build /app/agent /app/agent

# Install dependencies
RUN apk add --no-cache \
    ca-certificates \
    iproute2 \
    net-tools \
    alsa-utils \
    bash

# Create entrypoint script
RUN echo '#!/bin/bash' > /app/entrypoint.sh && \
    echo 'cat <<EOF > /app/agent.yaml' >> /app/entrypoint.sh && \
    echo 'agent_id: ${AGENT_ID:-robot-$(hostname)}' >> /app/entrypoint.sh && \
    echo 'type: robot' >> /app/entrypoint.sh && \
    echo 'mqtt_broker: ${MQTT_BROKER:-tcp://mqtt:1883}' >> /app/entrypoint.sh && \
    echo 'workspace_path: ${WORKSPACE_PATH:-/app/workspace}' >> /app/entrypoint.sh && \
    echo 'workspace_owner: ${WORKSPACE_OWNER:-root}' >> /app/entrypoint.sh && \
    echo 'EOF' >> /app/entrypoint.sh && \
    echo '' >> /app/entrypoint.sh && \
    echo 'export AGENT_CONFIG_PATH=/app/agent.yaml' >> /app/entrypoint.sh && \
    echo 'exec /app/agent' >> /app/entrypoint.sh && \
    chmod +x /app/entrypoint.sh

CMD ["/app/entrypoint.sh"]
