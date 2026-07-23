# WebSocket Chat Pub/Sub Server

[![CI/CD Pipeline](https://github.com/sanskarpan/websocket-chat-pub-sub/actions/workflows/ci.yml/badge.svg)](https://github.com/sanskarpan/websocket-chat-pub-sub/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.23%2B-blue)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

A high-performance, distributed, real-time WebSocket chat and Pub/Sub backend built with **Go**, **PostgreSQL**, **Redis**, **Gin**, and **Docker**. Designed for resilience under high throughput with horizontal scalability, structured logging, Prometheus observability, and robust security controls.

---

## Table of Contents

- [Architecture](#architecture)
- [Quickstart](#quickstart)
- [Configuration Reference](#configuration-reference)
- [REST API](#rest-api)
- [WebSocket Protocol](#websocket-protocol)
- [Testing](#testing)
- [Docker Deployment](#docker-deployment)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Monitoring & Observability](#monitoring--observability)
- [Security](#security)
- [Project Structure](#project-structure)
- [Contributing](#contributing)
- [License](#license)

---

## Architecture

### System Diagram

```text
                             +--------------------+
                             |   Reverse Proxy    |
                             |  (Nginx / Traefik) |
                             +---------+----------+
                                       |
                  +--------------------+--------------------+
                  |                                         |
         +--------v-------+                        +--------v-------+
         |  REST API      |                        |  WebSocket     |
         |  (Gin Engine)  |                        |  Server (Hub)  |
         |  Port: 8085    |                        |  Port: 8086    |
         +--------+-------+                        +--------+-------+
                  |                                         |
                  +--------------------+--------------------+
                                       |
                  +--------------------+--------------------+
                  |                                         |
         +--------v-------+                        +--------v-------+
         |  PostgreSQL    |                        |  Redis PubSub  |
         |  (Persistent)  |                        |  & Cache       |
         +----------------+                        +----------------+
```

### Server Components

The application launches three independent HTTP servers:

| Server | Default Port | Purpose |
|--------|-------------|---------|
| **REST API** | `8085` | Gin-based RESTful HTTP API for auth, rooms, and messages |
| **WebSocket** | `8086` | Real-time messaging via WebSocket connections |
| **Metrics** | `9090` | Prometheus metrics endpoint |

### Pub/Sub Channel Structure

Redis Pub/Sub channels used for real-time message distribution:

| Channel Pattern | Purpose |
|----------------|---------|
| `ws:room:<roomID>` | Room message events (new, edited, deleted) |
| `ws:room:<roomID>:events` | Room member events (joined, left) |
| `ws:presence` | Presence update events (broadcast) |

---

## Quickstart

### Prerequisites

- **Go** `1.23+`
- **Docker & Docker Compose** `24.0+`
- **Make** (optional)

### Local Setup

```bash
# 1. Clone and enter the repository
git clone https://github.com/sanskarpan/websocket-chat-pub-sub.git
cd websocket-chat-pub-sub

# 2. Start PostgreSQL and Redis infrastructure
docker compose up -d

# 3. Generate JWT signing keys (required for auth)
bash scripts/generate_keys.sh

# 4. Build and start the server
go run cmd/server/main.go
```

The server will be available at:
- REST API: `http://localhost:8085`
- WebSocket: `ws://localhost:8086/ws`
- Metrics: `http://localhost:9090/metrics`

---

## Configuration Reference

All settings are configured via `configs/config.yaml` and can be overridden using environment variables.

### Core Server

| Environment Variable | Config Path | Type | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `SERVER_PORT` | `server.port` | int | `8085` | REST API HTTP listener port |
| `APP_ENVIRONMENT` | `app.environment` | string | `development` | Runtime mode (`development` / `production`) |

### Authentication (JWT with RS256)

The server uses **RS256 asymmetric JWT signing** with RSA-2048 key pairs. Keys are auto-generated in development if not found.

| Environment Variable | Config Path | Type | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `JWT_PRIVATE_KEY` | `auth.jwt.private_key_path` | string (PEM) | — | RSA private key PEM content (overrides file) |
| `JWT_PUBLIC_KEY` | `auth.jwt.public_key_path` | string (PEM) | — | RSA public key PEM content (overrides file) |
| `auth.jwt.private_key_path` | — | file path | `configs/jwt-private.pem` | Path to private key file |
| `auth.jwt.public_key_path` | — | file path | `configs/jwt-public.pem` | Path to public key file |

### Database

| Environment Variable | Config Path | Type | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `DB_PASSWORD` | `database.postgresql.password` | string | `postgres` | PostgreSQL password (required in production) |

### Redis

| Environment Variable | Config Path | Type | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `REDIS_PASSWORD` | `redis.password` | string | `""` | Redis connection password |

### Rate Limiting

| Config Path | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `rate_limit.rules[n].key` | string | — | Rate limit scope (`message`, `connection`, `room_create`) |
| `rate_limit.rules[n].limit` | int | — | Maximum requests per window |
| `rate_limit.rules[n].window` | duration | — | Time window (e.g. `1m`, `1h`) |

### Observability

| Config Path | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `observability.logging.level` | string | `debug` | Log level (`debug`, `info`, `warn`, `error`) |
| `observability.logging.format` | string | `json` | Log format (`json`, `text`) |
| `observability.metrics.port` | int | `9090` | Prometheus metrics port |
| `observability.tracing.enabled` | bool | `false` | Enable distributed tracing |

---

## REST API

### Health & Discovery

```http
GET /                        # API root — service metadata and endpoint map
GET /healthz                 # Liveness probe (always 200)
GET /readyz                  # Readiness probe (200 if DB + Redis connected, 503 otherwise)
GET /health                  # Alias for /readyz
```

### Authentication (`/api/v1/auth`)

All auth endpoints are rate-limited (10 req/min per client).

#### Register

```http
POST /api/v1/auth/register
Content-Type: application/json

{
  "username": "johndoe",
  "email": "john@example.com",
  "password": "securePass123!",
  "display_name": "John Doe"
}
```

Response `201`:
```json
{
  "id": "snowflake-id",
  "username": "johndoe",
  "email": "john@example.com",
  "display_name": "John Doe"
}
```

#### Login

```http
POST /api/v1/auth/login
Content-Type: application/json

{
  "email": "john@example.com",
  "password": "securePass123!"
}
```

Response `200`:
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "refresh_token": "dGhpcyBpcyBhIHJlZnJl...",
  "token_type": "Bearer",
  "expires_in": 900
}
```

#### Refresh Token

```http
POST /api/v1/auth/refresh
Content-Type: application/json

{
  "refresh_token": "dGhpcyBpcyBhIHJlZnJl..."
}
```

Response `200`:
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "refresh_token": "bmV3IHJlZnJl...",
  "token_type": "Bearer"
}
```

### Rooms (`/api/v1/rooms`)

All room endpoints require `Authorization: Bearer <access_token>`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/rooms` | List rooms the user is a member of |
| `POST` | `/api/v1/rooms` | Create a room (`direct`, `group`, or `channel`) |
| `GET` | `/api/v1/rooms/:id` | Get room details |
| `GET` | `/api/v1/rooms/:id/messages` | Get paginated messages (`?limit=N&before=RFC3339`) |
| `POST` | `/api/v1/rooms/:id/join` | Join a room |
| `POST` | `/api/v1/rooms/:id/leave` | Leave a room |

#### Create Room

```http
POST /api/v1/rooms
Authorization: Bearer <access_token>
Content-Type: application/json

{
  "name": "general",
  "type": "channel",
  "description": "General discussion"
}
```

Response `201`:
```json
{
  "id": "room-snowflake-id",
  "name": "general",
  "type": "channel",
  "description": "General discussion",
  "created_by": "user-snowflake-id",
  "member_count": 1,
  "created_at": "2026-01-01T00:00:00Z",
  "settings": {
    "allow_reactions": true,
    "allow_threads": true,
    "message_retention": 365,
    "slow_mode_seconds": 0,
    "require_approval": false,
    "only_admins_can_post": false
  }
}
```

#### Get Messages

```http
GET /api/v1/rooms/:id/messages?limit=50&before=2026-01-01T00:00:00Z
Authorization: Bearer <access_token>
```

Response `200`:
```json
[
  {
    "id": "msg-snowflake-id",
    "room_id": "room-snowflake-id",
    "user_id": "user-snowflake-id",
    "content": "Hello world!",
    "content_type": "text",
    "created_at": "2026-01-01T00:00:00Z",
    "reactions": {
      "👍": ["user-id-1", "user-id-2"]
    }
  }
]
```

#### Error Responses

| Status Code | Meaning |
|-------------|---------|
| `400` | Invalid input / validation error |
| `401` | Missing or invalid authentication |
| `403` | Forbidden (not a room member) |
| `404` | Resource not found |
| `429` | Rate limit exceeded |
| `500` | Internal server error |

---

## WebSocket Protocol

### Connection

```
ws://localhost:8086/ws?token=<access_token>
```

Authentication is required via the `?token=` query parameter. Per-IP connection limit: 10 (configurable).

### Message Envelope

All messages (client-to-server and server-to-client) follow this structure:

```json
{
  "id": "snowflake-id",
  "type": "<message-type>",
  "timestamp": "2026-01-01T00:00:00Z",
  "data": { ... }
}
```

### Client-to-Server Messages

| Type | Data Fields | Description |
|------|-------------|-------------|
| `"subscribe"` | `{ "room_ids": [...], "presence_subscribe": [...] }` | Subscribe to rooms (must be a member) |
| `"unsubscribe"` | `{ "room_ids": [...], "presence_subscribe": [...] }` | Unsubscribe from rooms |
| `"message"` | `{ "room_id", "content", "client_id"?, "parent_id"? }` | Send a message (max 4000 chars) |
| `"typing"` | `{ "room_id", "user_id" }` | Typing indicator |
| `"read_receipt"` | `{ "room_id", "message_id", "user_id" }` | Mark message as read |
| `"reaction"` | `{ "room_id", "message_id", "emoji", "action" (add/remove), "user_id" }` | Add/remove reaction |
| `"edit"` | `{ "room_id", "message_id", "content" }` | Edit own message |
| `"delete"` | `{ "room_id", "message_id" }` | Delete a message |
| `"presence"` | `{ "status" }` (online/away/dnd/offline) | Set presence status |
| `"ping"` | `{}` | Keep-alive ping |

### Server-to-Client Messages

| Type | Data Fields | Description |
|------|-------------|-------------|
| `"connection"` | `{ "user_id", "session_id" }` | Sent after successful WebSocket upgrade |
| `"ack"` | `{ "client_msg_id", "server_msg_id"?, "status" }` | Confirmation for subscribe/message/edit/delete |
| `"error"` | `{ "client_msg_id"?, "code", "message", "retry_after"? }` | Error response |
| `"new_message"` | `{ "room_id", "message" }` | New message in subscribed room |
| `"message_updated"` | `{ "room_id", "message", "action" }` | Message edited or deleted |
| `"typing"` | `{ "room_id", "user_id" }` | User typing in subscribed room |
| `"reaction"` | `{ "room_id", "message_id", "emoji", "action", "user_id" }` | Reaction added/removed |
| `"presence"` | `{ "user_id", "status", "presence" }` | User presence changed |
| `"read_receipt"` | `{ "room_id", "message_id", "user_id" }` | Message read by user |
| `"member_joined"` | `{ "room_id", "user_id" }` | Member joined room |
| `"member_left"` | `{ "room_id", "user_id" }` | Member left room |
| `"pong"` | `{}` | Response to ping |

### WebSocket Error Codes

| Code | Description |
|------|-------------|
| `UNKNOWN_TYPE` | Unrecognized message type |
| `INVALID_INPUT` | Missing or malformed fields |
| `CONTENT_TOO_LONG` | Message exceeds 4000 characters |
| `FORBIDDEN` | Not a member of the target room |
| `NOT_SUBSCRIBED` | Must subscribe before sending messages |
| `EDIT_FAILED` | Could not edit the message |
| `DELETE_FAILED` | Could not delete the message |

---

## Testing

### Unit & Integration Tests

Run unit tests and integration tests (no external dependencies required):

```bash
go test -race -v ./...
```

### End-to-End Tests

E2E tests require PostgreSQL, Redis, and the server running. Build tag: `e2e`.

```bash
# 1. Start infrastructure
docker compose up -d

# 2. Generate JWT keys
bash scripts/generate_keys.sh

# 3. Build and start the server
go build -o server ./cmd/server && ./server &
sleep 2

# 4. Run E2E tests
go test -race -v -tags=e2e ./test/e2e/...

# 5. Cleanup
kill %1
docker compose down
```

### Lint & Security

```bash
# Static analysis
golangci-lint run

# Vulnerability scan
govulncheck ./...
```

---

## Docker Deployment

### Building the Image

```bash
docker build -t websocket-chat:latest -f Dockerfile .
```

### Running with Docker

```bash
docker run -p 8085:8085 -p 8086:8086 -p 9090:9090 \
  -e APP_ENVIRONMENT=production \
  -e DB_PASSWORD="your-db-password" \
  -v $(pwd)/configs/jwt-private.pem:/app/configs/jwt-private.pem:ro \
  -v $(pwd)/configs/jwt-public.pem:/app/configs/jwt-public.pem:ro \
  websocket-chat:latest
```

### Docker Compose

```bash
docker compose up -d --build
```

---

## Kubernetes Deployment

Deploy to Kubernetes using the provided manifests:

```bash
# Apply all manifests
kubectl apply -f deployments/kubernetes/

# Check status
kubectl get pods,services
```

The deployment includes:
- `deployment.yaml` — Application deployment with resource limits, probes, and config
- `service.yaml` — Internal ClusterIP service
- Auto-scaling based on CPU/memory usage

---

## Monitoring & Observability

### Health Endpoints

| Endpoint | Type | Expected Status |
|----------|------|-----------------|
| `GET /healthz` | Liveness | `200 OK` |
| `GET /readyz` | Readiness | `200 OK` (DB + Redis connected) |

### Prometheus Metrics (`:9090/metrics`)

| Metric | Type | Description |
|--------|------|-------------|
| `websocket_connections_active` | Gauge | Active WebSocket connections |
| `room_subscriptions_active` | Gauge | Active room subscriptions |
| `websocket_messages_sent_total` | Counter | Messages sent through WebSocket |
| `websocket_messages_received_total` | Counter | Messages received from WebSocket |
| `auth_attempts_total` | Counter | Auth attempts by status |
| `rate_limited_requests_total` | Counter | Rate-limited requests by key |
| `db_query_duration_seconds` | Histogram | Database query latency |
| `redis_operation_duration_seconds` | Histogram | Redis operation latency |

### Structured Logging

All logs are output as JSON with the following fields:
- `level` — Log level (`debug`, `info`, `warn`, `error`)
- `timestamp` — ISO 8601 timestamp
- `request_id` — Correlation ID for request tracing
- `component` — Source component
- `duration` — Request processing time

---

## Security

- **RS256 JWT**: Asymmetric RSA-2048 signing keys (auto-generated in dev, configurable in prod)
- **Timing-Attack Mitigation**: Login uses a dummy bcrypt hash when user not found to normalize response timing
- **Fail-Closed Rate Limiting**: Denies requests when Redis is unreachable
- **Password Hashing**: bcrypt with cost factor 12; hashes excluded from search API responses
- **XSS Protection**: Script tags stripped before HTML entity escaping
- **Message Deduplication**: Client-provided `client_id` prevents duplicate message processing (5-minute window)
- **Config Validation**: Production mode enforces JWT keys and database password
- **Graceful Shutdown**: 30-second context timeout with ordered server shutdown

---

## Project Structure

```
├── cmd/server/main.go           # Application entry point
├── internal/
│   ├── config/                  # Configuration loading & validation
│   ├── handlers/                # HTTP & WebSocket request handlers
│   ├── logging/                 # Structured logging setup
│   ├── metrics/                 # Prometheus metric definitions
│   ├── middleware/              # HTTP middleware (CORS, rate-limit, auth, request ID)
│   ├── model/                   # Domain models (User, Message, Room, Presence)
│   ├── protocol/                # WebSocket message types & constants
│   ├── pubsub/                  # Redis Pub/Sub implementation
│   ├── repository/              # Database access layer (PostgreSQL)
│   ├── server/                  # WebSocket server (hub, connection management)
│   ├── service/                 # Business logic layer
│   └── tracing/                 # Distributed tracing spans
├── pkg/
│   ├── sanitization/            # XSS sanitization utilities
│   └── snowflake/               # Snowflake ID generation
├── test/
│   ├── e2e/                     # End-to-end tests
│   └── integration/             # Integration tests
├── configs/
│   └── config.yaml              # Application configuration
├── deployments/kubernetes/      # Kubernetes manifests
├── scripts/
│   └── generate_keys.sh         # RSA key generation script
├── Dockerfile                   # Production Docker image
├── docker-compose.yaml          # Local development services
└── .github/workflows/ci.yml     # CI/CD pipeline
```

---

## Contributing

Please see [CONTRIBUTING.md](CONTRIBUTING.md) for branch policies, PR workflows, and code formatting guidelines.

---

## License

Distributed under the MIT License. See [LICENSE](LICENSE) for details.