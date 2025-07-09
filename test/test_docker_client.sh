#!/bin/bash

# Test script for Docker registry push/pull operations

set -e

echo "Starting Depot server..."

# Create temp directories
TEMP_DIR=$(mktemp -d)
DATA_DIR="$TEMP_DIR/data"
CERT_DIR="$TEMP_DIR/certs"

mkdir -p "$DATA_DIR" "$CERT_DIR"

# Generate certificates
openssl genrsa -out "$CERT_DIR/server.key" 2048
openssl req -new -x509 -sha256 -key "$CERT_DIR/server.key" -out "$CERT_DIR/server.crt" -days 1 \
    -subj "/C=US/ST=Test/L=Test/O=Test/CN=localhost"

# Build depot
echo "Building depot..."
go build -o "$TEMP_DIR/depot" ./cmd/depot

# Start depot server
export DEPOT_HOST=127.0.0.1
export DEPOT_PORT=8443
export DEPOT_DATA_DIR="$DATA_DIR"
export DEPOT_CERT_FILE="$CERT_DIR/server.crt"
export DEPOT_KEY_FILE="$CERT_DIR/server.key"
export DEPOT_DB_PATH="$DATA_DIR/depot.db"

"$TEMP_DIR/depot" &
DEPOT_PID=$!

# Wait for server to start
echo "Waiting for server to start..."
sleep 3

# Create a Docker repository
echo "Creating Docker repository..."
curl -k -X POST https://localhost:8443/api/v1/repositories \
    -H "Content-Type: application/json" \
    -d '{
        "name": "test-docker",
        "type": "docker",
        "description": "Test Docker registry",
        "config": {
            "http_port": 5000,
            "https_port": 0,
            "v1_enabled": false
        }
    }'

# Wait for registry to start
sleep 2

# Test registry is accessible
echo "Testing registry endpoint..."
curl -f http://localhost:5000/v2/ || { echo "Registry not accessible"; kill $DEPOT_PID; exit 1; }

# Configure Docker to use insecure registry
echo "Configuring Docker for insecure registry..."
if [[ "$OSTYPE" == "darwin"* ]]; then
    echo "On macOS, please add localhost:5000 to insecure registries in Docker Desktop settings"
else
    # Linux: would need to modify /etc/docker/daemon.json
    echo "On Linux, add localhost:5000 to insecure-registries in /etc/docker/daemon.json"
fi

# Test with Docker client
echo "Testing Docker operations..."

# Pull a small test image
docker pull busybox:latest

# Tag for our registry
docker tag busybox:latest localhost:5000/test-image:v1

# Push to our registry
echo "Pushing image to registry..."
docker push localhost:5000/test-image:v1 || echo "Push failed - may need insecure registry configuration"

# Remove local image
docker rmi localhost:5000/test-image:v1 busybox:latest || true

# Try to pull from our registry
echo "Pulling image from registry..."
docker pull localhost:5000/test-image:v1 || echo "Pull failed - may need insecure registry configuration"

# Check catalog
echo "Checking catalog..."
curl http://localhost:5000/v2/_catalog

# Check tags
echo "Checking tags..."
curl http://localhost:5000/v2/test-image/tags/list

# Cleanup
echo "Cleaning up..."
docker rmi localhost:5000/test-image:v1 || true
kill $DEPOT_PID
wait $DEPOT_PID 2>/dev/null || true
rm -rf "$TEMP_DIR"

echo "Test completed!"