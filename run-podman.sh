#!/bin/bash
set -e

# Check for podman
if ! command -v podman &> /dev/null; then
    echo "Error: podman is not installed."
    exit 1
fi

# Determine compose command
if podman compose --help &> /dev/null; then
    COMPOSE_CMD="podman compose"
elif command -v podman-compose &> /dev/null; then
    COMPOSE_CMD="podman-compose"
else
    echo "Error: Neither 'podman compose' nor 'podman-compose' is available."
    exit 1
fi

# Detect Podman socket
echo "Detecting Podman socket..."
SOCKET_PATH=""

# Try podman info
if command -v podman &> /dev/null; then
    SOCKET_PATH=$(podman info --format '{{.Host.RemoteSocket.Path}}' 2>/dev/null || true)
fi

# Fallbacks
if [ -z "$SOCKET_PATH" ] || [ ! -e "$SOCKET_PATH" ]; then
    if [ -e "/run/podman/podman.sock" ]; then
        SOCKET_PATH="/run/podman/podman.sock"
    elif [ -n "$XDG_RUNTIME_DIR" ] && [ -e "$XDG_RUNTIME_DIR/podman/podman.sock" ]; then
        SOCKET_PATH="$XDG_RUNTIME_DIR/podman/podman.sock"
    elif [ -e "/var/run/docker.sock" ]; then
         # Fallback to docker socket if podman-docker is active
        SOCKET_PATH="/var/run/docker.sock"
    fi
fi

if [ -z "$SOCKET_PATH" ]; then
    echo "Error: Could not find Podman socket. Please ensure the podman socket is active."
    echo "Try running: systemctl --user enable --now podman.socket (for rootless)"
    echo "Or: systemctl enable --now podman.socket (for root)"
    exit 1
fi

echo "Using Podman socket: $SOCKET_PATH"

# Run compose
export PODMAN_SOCKET="$SOCKET_PATH"
echo "Starting services with $COMPOSE_CMD..."
$COMPOSE_CMD -f podman-compose.yml up --build
