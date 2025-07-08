# Depot Build and Run Instructions

This document provides instructions for building and running the Depot asset repository.

## Prerequisites

- Go 1.21 or later
- Docker (for containerized deployment)
- OpenSSL (for generating certificates)

## Building the Project

### Local Build

```bash
# Download dependencies
go mod download

# Build the daemon
go build -o depot ./cmd/depot

# Run tests
go test ./...
```

### Docker Build

```bash
# Build the Docker image
docker build -t depot:latest .
```

## Running the Daemon

### Generate SSL Certificates

First, generate self-signed certificates for HTTPS:

```bash
# Create certificate directory
mkdir -p certs

# Generate private key
openssl genrsa -out certs/server.key 2048

# Generate certificate
openssl req -new -x509 -sha256 -key certs/server.key -out certs/server.crt -days 365 \
    -subj "/C=US/ST=State/L=City/O=Organization/CN=localhost"
```

### Run Locally

```bash
# Set environment variables (optional, defaults shown)
export DEPOT_HOST=0.0.0.0
export DEPOT_PORT=8443
export DEPOT_DATA_DIR=/var/depot/data
export DEPOT_CERT_FILE=./certs/server.crt
export DEPOT_KEY_FILE=./certs/server.key
export DEPOT_DB_PATH=/var/depot/data/depot.db

# Create data directory
mkdir -p /var/depot/data

# Run the daemon
./depot
```

### Run with Docker

```bash
# Create volumes for data and certificates
docker volume create depot-data
docker volume create depot-certs

# Copy certificates to volume (assuming you generated them locally)
docker run --rm -v depot-certs:/certs -v $(pwd)/certs:/source alpine cp -r /source/. /certs/

# Run the container
docker run -d \
    --name depot \
    -p 8443:8443 \
    -v depot-data:/var/depot/data \
    -v depot-certs:/var/depot/certs \
    depot:latest
```

## Testing the API

### Health Check

```bash
curl -k https://localhost:8443/api/v1/health
```

### Create a Repository

```bash
# Create a raw repository
curl -k -X POST https://localhost:8443/api/v1/repositories \
    -H "Content-Type: application/json" \
    -d '{
        "name": "my-raw-repo",
        "type": "raw",
        "description": "My raw artifact repository"
    }'

# Create a docker repository
curl -k -X POST https://localhost:8443/api/v1/repositories \
    -H "Content-Type: application/json" \
    -d '{
        "name": "my-docker-repo",
        "type": "docker",
        "description": "My docker registry"
    }'
```

### List Repositories

```bash
curl -k https://localhost:8443/api/v1/repositories
```

### Upload/Download Raw Artifacts

```bash
# Upload an artifact
curl -k -X PUT https://localhost:8443/repository/my-raw-repo/path/to/artifact.jar \
    --data-binary @artifact.jar

# Download an artifact
curl -k https://localhost:8443/repository/my-raw-repo/path/to/artifact.jar \
    -o downloaded-artifact.jar

# Check if artifact exists
curl -k -I https://localhost:8443/repository/my-raw-repo/path/to/artifact.jar

# Delete an artifact
curl -k -X DELETE https://localhost:8443/repository/my-raw-repo/path/to/artifact.jar
```

## Development

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run specific test
go test -v ./test -run TestServerStartStop
```

### Linting

```bash
# Install golangci-lint if not already installed
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run linter
golangci-lint run
```

## Architecture Overview

- **cmd/depot**: Main entry point for the daemon
- **internal/server**: HTTPS server implementation
- **internal/api**: REST API handlers
- **internal/repository**: Repository management
- **internal/storage**: Artifact storage abstraction
- **pkg/models**: Shared data models

## Next Steps

1. Implement Docker registry API endpoints
2. Add authentication and authorization
3. Create CLI administrative tool
4. Add support for repository groups
5. Implement cleanup policies
6. Add metrics and monitoring