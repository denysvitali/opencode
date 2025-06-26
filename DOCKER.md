# OpenCode Docker Guide

## Overview

OpenCode is available as a multi-architecture Docker image built for both AMD64 and ARM64 platforms.

## Quick Start

### Pull and Run

```bash
# Pull the latest image
docker pull ghcr.io/denysvitali/opencode:latest

# Run the server
docker run -p 8080:8080 -p 8081:8081 ghcr.io/denysvitali/opencode:latest

# Run with custom ports
docker run -p 9090:8080 -p 9091:8081 \
  -e GRPC_PORT=8080 -e HTTP_PORT=8081 \
  ghcr.io/denysvitali/opencode:latest

# Run with debug logging
docker run -p 8080:8080 -p 8081:8081 \
  -e OPENCODE_DEBUG=true \
  ghcr.io/denysvitali/opencode:latest
```

### Docker Compose

```yaml
version: '3.8'

services:
  opencode:
    image: ghcr.io/denysvitali/opencode:latest
    ports:
      - "8080:8080"  # gRPC
      - "8081:8081"  # HTTP Gateway
    environment:
      - OPENCODE_DEBUG=false
      - GRPC_PORT=8080
      - HTTP_PORT=8081
    volumes:
      - ./workspace:/workspace
    working_dir: /workspace
    restart: unless-stopped
```

## Available Tags

- `latest` - Latest stable release from main branch
- `main` - Latest build from main branch
- `v1.0.0`, `v1.0`, `v1` - Semantic version tags
- `sha-<commit>` - Specific commit builds

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GRPC_PORT` | `8080` | gRPC server port |
| `HTTP_PORT` | `8081` | HTTP gateway port |
| `OPENCODE_DEBUG` | `false` | Enable debug logging |

## Ports

- **8080**: gRPC API server
- **8081**: HTTP REST API gateway

## Volumes

Mount your workspace directory to `/workspace` for file operations:

```bash
docker run -v /path/to/your/project:/workspace ghcr.io/denysvitali/opencode:latest
```

## Health Check

The container includes a health check endpoint:

```bash
curl http://localhost:8081/health
```

## Architecture Support

The image is built for multiple architectures:
- `linux/amd64` (Intel/AMD 64-bit)
- `linux/arm64` (ARM 64-bit, Apple Silicon)

Docker will automatically pull the correct architecture for your platform.

## Building Locally

```bash
# Clone the repository
git clone https://github.com/opencode-ai/opencode.git
cd opencode

# Build the image
docker build -t opencode .

# Run your local build
docker run -p 8080:8080 -p 8081:8081 opencode
```

## CI/CD Integration

The Docker images are automatically built and published via GitHub Actions on:
- Push to main/master branch
- Git tags (for releases)
- Pull requests (for testing)

### Build Process

1. **Multi-architecture builds**: Separate jobs for AMD64 and ARM64
2. **Caching**: GitHub Actions cache for faster builds
3. **Testing**: Basic smoke tests on the built images
4. **Publishing**: Push to GitHub Container Registry (GHCR)

### Image Naming

- Repository: `ghcr.io/denysvitali/opencode`
- Tags: Automatic based on Git refs and semantic versions
