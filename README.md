# Depot

Depot is a lightweight, self-hosted artifact repository manager that supports multiple repository types including raw artifacts and Docker registries. It provides a simple REST API for managing repositories and artifacts, making it easy to host your own private artifact storage.

## Features

- **Multiple Repository Types**
  - **Raw Repositories**: Store any type of file (JARs, ZIPs, binaries, etc.)
  - **Docker Registries**: Full Docker Registry V2 API implementation with multi-arch support

- **Docker Registry Features**
  - Complete Docker Registry V2 API compatibility
  - Support for multi-architecture images and manifest lists
  - OCI image format support
  - Content-addressable storage for efficient layer deduplication
  - Multiple registries on different ports
  - Option to serve a registry on the main server port

- **Simple Management**
  - RESTful API for repository management
  - HTTPS support with TLS
  - Lightweight and easy to deploy
  - File-based storage with efficient organization

## Quick Start

### Prerequisites

- Go 1.21 or later
- OpenSSL (for generating certificates)
- Docker (optional, for containerized deployment)

### Build and Run

1. **Clone the repository**
   ```bash
   git clone https://github.com/depot/depot
   cd depot
   ```

2. **Build the binary**
   ```bash
   go build -o depot ./cmd/depot
   ```

3. **Generate SSL certificates**
   ```bash
   mkdir -p certs
   openssl genrsa -out certs/server.key 2048
   openssl req -new -x509 -sha256 -key certs/server.key -out certs/server.crt -days 365 \
       -subj "/C=US/ST=State/L=City/O=Organization/CN=localhost"
   ```

4. **Run Depot**
   ```bash
   export DEPOT_CERT_FILE=./certs/server.crt
   export DEPOT_KEY_FILE=./certs/server.key
   ./depot
   ```

The server will start on `https://localhost:8443` by default.

## Usage Examples

### Create a Raw Repository

```bash
curl -k -X POST https://localhost:8443/api/v1/repositories \
    -H "Content-Type: application/json" \
    -d '{
        "name": "maven-releases",
        "type": "raw",
        "description": "Maven release artifacts"
    }'
```

### Upload an Artifact

```bash
curl -k -X PUT https://localhost:8443/repository/maven-releases/com/example/app/1.0/app-1.0.jar \
    --data-binary @app-1.0.jar
```

### Create a Docker Registry

```bash
curl -k -X POST https://localhost:8443/api/v1/repositories \
    -H "Content-Type: application/json" \
    -d '{
        "name": "docker-private",
        "type": "docker",
        "description": "Private Docker registry",
        "config": {
            "http_port": 5000,
            "https_port": 0
        }
    }'
```

### Use the Docker Registry

```bash
# Configure Docker to allow insecure registry (if not using HTTPS)
# Add "localhost:5000" to insecure-registries in Docker settings

# Tag and push an image
docker tag myapp:latest localhost:5000/myapp:latest
docker push localhost:5000/myapp:latest

# Pull the image
docker pull localhost:5000/myapp:latest
```

## Configuration

Depot can be configured using environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `DEPOT_HOST` | Host to bind to | `0.0.0.0` |
| `DEPOT_PORT` | HTTPS port | `8443` |
| `DEPOT_DATA_DIR` | Data storage directory | `/var/depot/data` |
| `DEPOT_CERT_FILE` | TLS certificate file | `./certs/server.crt` |
| `DEPOT_KEY_FILE` | TLS key file | `./certs/server.key` |
| `DEPOT_DB_PATH` | Database file path | `/var/depot/data/depot.db` |

## API Documentation

### Repository Management

- `GET /api/v1/health` - Health check endpoint
- `GET /api/v1/repositories` - List all repositories
- `POST /api/v1/repositories` - Create a new repository
- `GET /api/v1/repositories/{name}` - Get repository details
- `DELETE /api/v1/repositories/{name}` - Delete a repository

### Raw Repository Operations

- `GET /repository/{repo-name}/{path}` - Download an artifact
- `PUT /repository/{repo-name}/{path}` - Upload an artifact
- `HEAD /repository/{repo-name}/{path}` - Check if artifact exists
- `DELETE /repository/{repo-name}/{path}` - Delete an artifact

### Docker Registry API

When a Docker repository is created, it exposes the standard Docker Registry V2 API on the configured port:

- `GET /v2/` - API version check
- `GET /v2/_catalog` - List repositories
- `GET /v2/{name}/tags/list` - List tags
- `GET /v2/{name}/manifests/{reference}` - Get manifest
- `PUT /v2/{name}/manifests/{reference}` - Upload manifest
- `GET /v2/{name}/blobs/{digest}` - Download blob
- `POST /v2/{name}/blobs/uploads/` - Start blob upload
- And more...

## Docker Support

Depot includes a full implementation of the Docker Registry V2 API, allowing you to host private Docker registries. Each Docker repository can be configured to run on its own port, or one repository can use the main server port (port 0 configuration).

Features:
- Push and pull Docker images
- Multi-architecture image support
- Manifest lists for cross-platform images
- OCI image format compatibility
- Efficient storage with content deduplication

## Testing

### Run Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./...

# Run specific test
go test -v ./test -run TestDockerRegistry
```

### Test Docker Registry

The project includes a Go-based registry client for testing without Docker:

```bash
go test -v ./test -run TestDockerRegistryWithClient
```

Alternatively, you can use tools like `skopeo` to test the registry:

```bash
# Copy an image to your registry
skopeo copy --dest-tls-verify=false \
    docker://busybox:latest \
    docker://localhost:5000/busybox:latest

# Inspect the image
skopeo inspect --tls-verify=false \
    docker://localhost:5000/busybox:latest
```

## Development

### Project Structure

```
depot/
├── cmd/depot/          # Main application entry point
├── internal/
│   ├── api/           # REST API handlers
│   ├── docker/        # Docker Registry implementation
│   ├── repository/    # Repository management
│   ├── server/        # HTTPS server
│   └── storage/       # Storage abstraction
├── pkg/
│   └── models/        # Shared data models
├── test/              # Integration tests
└── examples/          # Usage examples
```

### Building with Docker

```bash
docker build -t depot:latest .

docker run -d \
    --name depot \
    -p 8443:8443 \
    -v depot-data:/var/depot/data \
    -v depot-certs:/var/depot/certs \
    depot:latest
```

## Security Considerations

- Always use HTTPS in production (proper certificates recommended)
- Consider implementing authentication and authorization
- Restrict network access to trusted clients
- Regular backups of the data directory
- Monitor disk usage and implement cleanup policies

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

[Specify your license here]

## Roadmap

- [ ] Authentication and authorization
- [ ] Web UI for repository browsing
- [ ] Repository groups and proxying
- [ ] Cleanup policies and garbage collection
- [ ] Metrics and monitoring integration
- [ ] S3-compatible storage backend
- [ ] Repository mirroring and replication