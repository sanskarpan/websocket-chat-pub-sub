# WebSocket Chat Pub/Sub Server

[![CI/CD Pipeline](https://github.com/websocket-chat/actions/workflows/ci.yml/badge.svg)](https://github.com/websocket-chat/actions/workflows/ci.yml)
[![Go Version](https://img.shields.io/badge/Go-1.22%2B-blue)](https://golang.org)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

A high-performance, distributed, real-time WebSocket chat and Pub/Sub backend built with Go, PostgreSQL, Redis, Gin, and Docker. Designed for resilience under high throughput with horizontal scalability, structured logging, Prometheus observability, and robust security controls.

---

## System Architecture Map

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

---

## Quickstart (Local Setup in 4 Commands)

### Prerequisites
- **Go**: `1.22+`
- **Docker & Docker Compose**: `24.0+`
- **Make** (optional, for automation tasks)

```bash
# 1. Clone repository and navigate to root
cd WebSocket-Chat-Pub-Sub

# 2. Start PostgreSQL and Redis infrastructure containers
docker-compose up -d

# 3. Copy environment configuration
cp configs/config.yaml configs/config.local.yaml

# 4. Build and start server
go run cmd/server/main.go
```

---

## Configuration Reference

All settings can be configured via `configs/config.yaml` or overridden using environment variables:

| Environment Variable | Type | Default | Description | Required |
| :--- | :--- | :--- | :--- | :--- |
| `JWT_SECRET` | string | `dev-secret...` | Secret key for JWT signing (32+ chars in prod) | Yes (in Prod) |
| `DB_PASSWORD` | string | `postgres` | PostgreSQL database password | Yes (in Prod) |
| `REDIS_PASSWORD` | string | `""` | Redis connection password | No |
| `APP_ENVIRONMENT` | string | `development` | Environment mode (`development` / `production`) | No |
| `SERVER_PORT` | int | `8085` | REST API HTTP listener port | No |
| `METRICS_PORT` | int | `9090` | Prometheus metrics listener port | No |

---

## Testing & Quality Assurance

Run the test suite, race detector, and static analysis:

```bash
# Run unit & integration tests with race detector
go test -buildvcs=false -race -v ./...

# Run static linting analysis
golangci-lint run

# Run security vulnerability scan
govulncheck ./...
```

---

## Running with Docker

Build and execute the production container:

```bash
# Build production Docker image
docker build -t websocket-chat:latest -f Dockerfile .

# Run container with environment variables
docker run -p 8085:8085 -p 8086:8086 -p 9090:9090 \
  -e JWT_SECRET="your-32-byte-secure-jwt-secret-key-12345" \
  -e DB_PASSWORD="your-db-password" \
  websocket-chat:latest
```

---

## Production Health & Observability

- **Liveness Probe**: `GET http://localhost:8085/healthz` (`200 OK`)
- **Readiness Probe**: `GET http://localhost:8085/readyz` (`200 OK` when DB & Redis connected)
- **Prometheus Metrics**: `GET http://localhost:9090/metrics`

---

## Contributing & License

Please see [CONTRIBUTING.md](CONTRIBUTING.md) for branch policies, PR workflows, and code formatting guidelines.

Distributed under the MIT License. See [LICENSE](LICENSE) for details.
