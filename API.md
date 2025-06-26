# OpenCode gRPC API

This document describes the gRPC+grpc-gateway API for OpenCode, designed for single-session-per-container deployments.

## Overview

The OpenCode API provides a unified interface for interacting with OpenCode in a stateless, single-session-per-container model. Each container instance manages exactly one session, making it ideal for containerized deployments.

## Architecture

- **gRPC Server**: Native gRPC interface on port 8080
- **HTTP Gateway**: REST API via grpc-gateway on port 8081
- **Single Session**: One session per container instance
- **Auto-creation**: Session is created automatically on first interaction

## API Endpoints

### Health
- `GET /health` - Container health check

### Session Management
- `GET /session` - Get current session info (auto-creates if needed)
- `POST /session/reset` - Reset/clear current session
- `GET /session/stats` - Get session statistics

### Messages
- `POST /messages` - Send message with streaming response
- `GET /messages` - List message history
- `GET /messages/stream` - Stream real-time message updates
- `DELETE /messages` - Clear all messages

### Files
- `GET /files` - List workspace files
- `GET /files/{path}` - Read file content
- `PUT /files/{path}` - Write file content
- `DELETE /files/{path}` - Delete file
- `GET /files/changes` - Get file changes/diff

### Agent
- `POST /agent/cancel` - Cancel current agent operation
- `GET /agent/status` - Get agent status
- `GET /models` - List available models
- `PUT /agent/model` - Set agent model

## Usage

### Running the Server

```bash
# Development
go run ./cmd/server/

# Docker
docker build -f Dockerfile.api -t opencode-api .
docker run -p 8080:8080 -p 8081:8081 opencode-api

# Docker Compose
docker-compose -f docker-compose.api.yml up
```

### Testing

```bash
# Run the test script
./test_api.sh

# Manual testing
curl http://localhost:8081/health
curl http://localhost:8081/session
```

### Environment Variables

- `GRPC_PORT`: gRPC server port (default: 8080)
- `HTTP_PORT`: HTTP gateway port (default: 8081)
- `OPENCODE_DEBUG`: Enable debug logging (default: false)

## Protocol Buffers

The API is defined in `internal/proto/v1/opencode_service.proto` with shared types in `internal/proto/v1/common.proto`.

### Key Message Types

- `Session`: Session information
- `Message`: Chat message with content parts
- `ContentPart`: Text, binary, tool calls, etc.
- `FileInfo`: File metadata
- `ModelInfo`: Available AI models

## Implementation Status

✅ **Completed:**
- gRPC service definition
- HTTP gateway integration
- Basic server scaffolding
- Health, session, and stats endpoints
- Proto code generation
- Docker support

🚧 **In Progress:**
- Message handling implementation
- File operations
- Agent integration

📋 **Planned:**
- Streaming message responses
- File change tracking
- Model management
- Authentication/authorization

## Single-Session Model

Unlike the multi-session CLI version, this API follows a single-session-per-container model:

1. Each container instance manages exactly one session
2. Session is auto-created on first interaction
3. No session ID needed in requests (implicit)
4. Stateful within container lifetime
5. Reset creates new session, clearing old state

This design is optimal for:
- Microservice architectures
- Kubernetes deployments
- Isolated AI assistant instances
- External orchestration systems
