# Docker Registry Example

This example demonstrates how to use Depot as a Docker registry.

## 1. Create a Docker Repository

Create a Docker repository with a specific port:

```bash
curl -k -X POST https://localhost:8443/api/v1/repositories \
    -H "Content-Type: application/json" \
    -d '{
        "name": "my-docker-registry",
        "type": "docker",
        "description": "My private Docker registry",
        "config": {
            "http_port": 5000,
            "https_port": 0
        }
    }'
```

Or create a Docker repository on the main server port (only one repository can use port 0):

```bash
curl -k -X POST https://localhost:8443/api/v1/repositories \
    -H "Content-Type: application/json" \
    -d '{
        "name": "main-docker-registry",
        "type": "docker",
        "description": "Docker registry on main port",
        "config": {
            "http_port": 0,
            "https_port": 0
        }
    }'
```

## 2. Configure Docker Client

For non-HTTPS registries, you need to configure Docker to allow insecure registries.

### Docker Desktop (macOS/Windows)
1. Open Docker Desktop settings
2. Go to Docker Engine settings
3. Add your registry to insecure-registries:
```json
{
  "insecure-registries": ["localhost:5000"]
}
```
4. Apply & Restart

### Linux
Edit `/etc/docker/daemon.json`:
```json
{
  "insecure-registries": ["localhost:5000"]
}
```
Then restart Docker:
```bash
sudo systemctl restart docker
```

## 3. Push Images

```bash
# Pull an image from Docker Hub
docker pull nginx:latest

# Tag it for your registry
docker tag nginx:latest localhost:5000/nginx:latest

# Push to your registry
docker push localhost:5000/nginx:latest
```

## 4. Pull Images

```bash
# Pull from your registry
docker pull localhost:5000/nginx:latest
```

## 5. Registry API Operations

### List repositories (catalog)
```bash
curl http://localhost:5000/v2/_catalog
```

### List tags for a repository
```bash
curl http://localhost:5000/v2/nginx/tags/list
```

### Get manifest
```bash
curl http://localhost:5000/v2/nginx/manifests/latest
```

## 6. Multi-arch Support

The Docker registry supports multi-architecture images and manifest lists:

```bash
# Build and push multi-arch image using buildx
docker buildx create --use
docker buildx build --platform linux/amd64,linux/arm64 \
    -t localhost:5000/myapp:latest --push .
```

## 7. Delete a Docker Repository

```bash
# This will stop the registry and delete the repository
curl -k -X DELETE https://localhost:8443/api/v1/repositories/my-docker-registry
```

## Notes

- Each Docker repository runs on its own port (unless configured with port 0)
- Only one repository can be configured with port 0 (main server port)
- The registry implements Docker Registry V2 API
- Supports both Docker and OCI image formats
- Full support for multi-architecture images and manifest lists
- Content-addressable storage for efficient layer deduplication