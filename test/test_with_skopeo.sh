#!/bin/bash

# Test script using skopeo to test Docker registry functionality

set -e

echo "=== Testing Docker Registry with Skopeo ==="

# Check if skopeo is installed
if ! command -v skopeo &> /dev/null; then
    echo "Skopeo not found. Installing..."
    # For Ubuntu/Debian
    if command -v apt-get &> /dev/null; then
        sudo apt-get update && sudo apt-get install -y skopeo
    # For RHEL/CentOS/Fedora
    elif command -v yum &> /dev/null; then
        sudo yum install -y skopeo
    # For Alpine
    elif command -v apk &> /dev/null; then
        sudo apk add skopeo
    else
        echo "Cannot install skopeo automatically. Please install it manually."
        exit 1
    fi
fi

# Function to wait for server
wait_for_server() {
    local url=$1
    local max_attempts=30
    local attempt=0
    
    while [ $attempt -lt $max_attempts ]; do
        if curl -f -s "$url" > /dev/null 2>&1; then
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    
    return 1
}

# Assuming depot is already running on localhost:8443
DEPOT_URL="https://localhost:8443"
REGISTRY_PORT=5556

echo "Creating Docker repository..."
curl -k -X POST "$DEPOT_URL/api/v1/repositories" \
    -H "Content-Type: application/json" \
    -d "{
        \"name\": \"skopeo-test\",
        \"type\": \"docker\",
        \"description\": \"Test with skopeo\",
        \"config\": {
            \"http_port\": $REGISTRY_PORT,
            \"https_port\": 0
        }
    }"

echo "Waiting for registry to start..."
if ! wait_for_server "http://localhost:$REGISTRY_PORT/v2/"; then
    echo "Registry failed to start"
    exit 1
fi

echo "Registry is ready!"

# Test 1: Copy an image from Docker Hub to our registry
echo "Test 1: Copying busybox from Docker Hub..."
skopeo copy --dest-tls-verify=false \
    docker://busybox:latest \
    docker://localhost:$REGISTRY_PORT/busybox:latest

# Test 2: Inspect the image in our registry
echo "Test 2: Inspecting image in our registry..."
skopeo inspect --tls-verify=false \
    docker://localhost:$REGISTRY_PORT/busybox:latest

# Test 3: List tags
echo "Test 3: Listing tags..."
skopeo list-tags --tls-verify=false \
    docker://localhost:$REGISTRY_PORT/busybox

# Test 4: Copy between tags in our registry
echo "Test 4: Copying to new tag..."
skopeo copy --dest-tls-verify=false --src-tls-verify=false \
    docker://localhost:$REGISTRY_PORT/busybox:latest \
    docker://localhost:$REGISTRY_PORT/busybox:v1.0

# Test 5: Multi-arch image
echo "Test 5: Copying multi-arch image..."
skopeo copy --dest-tls-verify=false \
    docker://alpine:latest \
    docker://localhost:$REGISTRY_PORT/alpine:latest \
    --all

# Test 6: Check catalog
echo "Test 6: Checking catalog..."
curl -s "http://localhost:$REGISTRY_PORT/v2/_catalog" | jq .

# Test 7: Delete manifest
echo "Test 7: Getting manifest digest..."
DIGEST=$(skopeo inspect --tls-verify=false docker://localhost:$REGISTRY_PORT/busybox:v1.0 | jq -r .Digest)
echo "Deleting manifest $DIGEST..."
curl -X DELETE "http://localhost:$REGISTRY_PORT/v2/busybox/manifests/$DIGEST"

# Cleanup
echo "Cleaning up..."
curl -k -X DELETE "$DEPOT_URL/api/v1/repositories/skopeo-test"

echo "All tests completed successfully!"