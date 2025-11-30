FROM golang:1.25 as build
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN go build -o agent ./cmd/agent

FROM ubuntu:22.04
WORKDIR /app
COPY --from=build /app/agent /app/agent

# Install dependencies
RUN apt-get update && apt-get install -y \
    ca-certificates \
    iproute2 \
    net-tools \
    && rm -rf /var/lib/apt/lists/*

# Create entrypoint script
RUN echo '#!/bin/bash\n\
cat <<EOF > /app/agent.yaml\n\
agent_id: ${AGENT_ID:-robot-$(hostname)}\n\
mqtt_broker: ${MQTT_BROKER:-tcp://mqtt:1883}\n\
workspace_path: ${WORKSPACE_PATH:-/app/workspace}\n\
workspace_owner: ${WORKSPACE_OWNER:-root}\n\
EOF\n\
\n\
export AGENT_CONFIG_PATH=/app/agent.yaml\n\
exec /app/agent\n\
' > /app/entrypoint.sh && chmod +x /app/entrypoint.sh

CMD ["/app/entrypoint.sh"]
