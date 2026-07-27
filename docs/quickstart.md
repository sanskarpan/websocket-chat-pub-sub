# Quickstart

Up and running in under 5 minutes.

---

## Prerequisites

| Tool | Minimum version |
|---|---|
| Go | `1.23` |
| Docker & Docker Compose | `24.0` |
| `make` | any |
| `bash` | any (for key generation) |

---

## 1. Clone

```bash
git clone https://github.com/sanskarpan/websocket-chat-pub-sub.git
cd websocket-chat-pub-sub
```

---

## 2. Start infrastructure

Docker Compose starts PostgreSQL and Redis:

```bash
docker compose up -d
```

Expected output:
```
✔ Container websocket-chat-pub-sub-redis-1     Started
✔ Container websocket-chat-pub-sub-postgres-1  Started
```

Wait ~3 seconds for PostgreSQL to initialize, then verify:

```bash
docker compose ps
```

Both services should show `Up`.

---

## 3. Generate JWT signing keys

The server uses RS256 asymmetric signing. Keys are auto-generated in development mode if missing, but running the script explicitly is cleaner:

```bash
bash scripts/generate_keys.sh
```

This writes `configs/jwt-private.pem` and `configs/jwt-public.pem`.

!!! info "Development vs production"
    In development, the server auto-generates keys on startup if the files don't exist. In production, you must inject them via environment variables or a secrets manager. See [Authentication](authentication.md) for details.

---

## 4. Start the server

```bash
go run cmd/server/main.go
```

You should see:

```json
{"level":"info","message":"REST API server started","port":8085}
{"level":"info","message":"WebSocket server started","port":8086}
{"level":"info","message":"Metrics server started","port":9090}
```

The three endpoints are now live:

| Endpoint | URL |
|---|---|
| REST API | `http://localhost:8085` |
| WebSocket | `ws://localhost:8086/ws` |
| Prometheus metrics | `http://localhost:9090/metrics` |

---

## 5. First API call

Check readiness:

```bash
curl http://localhost:8085/readyz
```

```json
{"status": "ok"}
```

### Register a user

```bash
curl -s -X POST http://localhost:8085/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "alice",
    "email": "alice@example.com",
    "password": "securePass123!",
    "display_name": "Alice"
  }' | jq .
```

### Login

```bash
TOKEN=$(curl -s -X POST http://localhost:8085/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"alice@example.com","password":"securePass123!"}' \
  | jq -r '.access_token')

echo "Access token: $TOKEN"
```

### Create a room

```bash
curl -s -X POST http://localhost:8085/api/v1/rooms \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"general","type":"channel","description":"General chat"}' | jq .
```

---

## 6. First WebSocket connection

Open a second terminal and connect using `websocat` (or any WebSocket client):

```bash
# Install websocat: brew install websocat
websocat "ws://localhost:8086/ws?token=$TOKEN"
```

After connecting, you'll receive:
```json
{"id":"...","type":"connection","data":{"user_id":"...","session_id":"..."}}
```

Subscribe to the room you created:
```json
{"type":"subscribe","data":{"room_ids":["<room-id>"]}}
```

Then send a message:
```json
{"type":"message","data":{"room_id":"<room-id>","content":"Hello world!"}}
```

---

## 7. Run the tests

Unit and integration tests run without any external services:

```bash
go test -race ./...
```

All 11 packages pass. See [Testing](testing.md) for E2E and race detector details.

---

## What's next

- [Configuration](configuration.md) — every config knob and environment variable
- [REST API](rest-api.md) — full endpoint reference
- [WebSocket Protocol](websocket-protocol.md) — all message types and connection lifecycle
- [Architecture](architecture.md) — how the pieces fit together
