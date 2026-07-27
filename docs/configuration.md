# Configuration Reference

All settings live in `configs/config.yaml`. Environment variables override the corresponding YAML values at startup.

---

## Full config with defaults

```yaml
app:
  name: "websocket-chat"
  version: "1.0.0"
  environment: "development"   # development | production

server:
  host: "0.0.0.0"
  port: 8085                   # REST API port

  websocket:
    path: "/ws"
    port: 8086
    read_buffer_size: 1024
    write_buffer_size: 1024
    max_message_size: 65536    # 64 KB
    ping_interval: "30s"
    pong_timeout: "60s"
    write_timeout: "10s"
    max_connections_per_ip: 10
    allowed_origins:           # CORS allowed origins for WebSocket upgrade
      - "http://localhost:3000"

  http:
    read_timeout: "30s"
    write_timeout: "30s"
    idle_timeout: "120s"

database:
  postgresql:
    host: "localhost"
    port: 5432
    database: "chat"
    user: "chat"
    password: "postgres"       # Override with DB_PASSWORD in production
    ssl_mode: "disable"        # disable | require | verify-full
    max_open_conns: 25
    max_idle_conns: 10
    conn_max_lifetime: "30m"

redis:
  addr: "localhost:6379"
  password: ""                 # Override with REDIS_PASSWORD
  db: 0

auth:
  jwt:
    algorithm: "RS256"
    private_key_path: "configs/jwt-private.pem"
    public_key_path: "configs/jwt-public.pem"
    access_token_ttl: "15m"
    refresh_token_ttl: "168h"  # 7 days
    issuer: "chat-app"
    audience: ["chat-api"]
  bcrypt:
    cost: 12                   # Minimum 10 in production

rate_limit:
  enabled: true
  rules:
    - key: "message"
      limit: 100
      window: "1m"
    - key: "connection"
      limit: 5
      window: "1m"
    - key: "room_create"
      limit: 10
      window: "1h"
    - key: "auth"
      limit: 10
      window: "1m"

observability:
  logging:
    level: "debug"             # debug | info | warn | error
    format: "json"             # json | console
  metrics:
    enabled: true
    port: 9090
    path: "/metrics"
  tracing:
    enabled: false
    exporter: "jaeger"
    jaeger:
      endpoint: "http://localhost:14268/api/traces"
    sampling_rate: 0.01
```

---

## Environment variable overrides

| Environment variable | Config path | Required in prod |
|---|---|---|
| `APP_ENVIRONMENT` | `app.environment` | yes — set to `production` |
| `SERVER_PORT` | `server.port` | no |
| `JWT_PRIVATE_KEY` | overrides `auth.jwt.private_key_path` | yes |
| `JWT_PUBLIC_KEY` | overrides `auth.jwt.public_key_path` | yes |
| `DB_PASSWORD` | `database.postgresql.password` | yes |
| `DB_HOST` | `database.postgresql.host` | if not using docker-compose |
| `REDIS_PASSWORD` | `redis.password` | if Redis has auth |
| `REDIS_ADDR` | `redis.addr` | if not using docker-compose |

When `APP_ENVIRONMENT=production`, the server enforces:

- `JWT_PRIVATE_KEY` and `JWT_PUBLIC_KEY` must be explicitly set (auto-generation disabled)
- `DB_PASSWORD` must be explicitly set

---

## CORS

The REST API CORS middleware allows all origins by default in development. In production, configure the allowed origins list:

```yaml
server:
  websocket:
    allowed_origins:
      - "https://chat.example.com"
      - "https://app.example.com"
```

This list is also used by the WebSocket upgrade handler — only connections from listed origins are accepted.

!!! warning "Hardcoded origins"
    Earlier versions of this server had CORS origins hardcoded in `main.go`. The current code reads them from `cfg.Server.Websocket.AllowedOrigins`. Ensure your config file or environment properly sets this list in production.

---

## Rate limiting

Rate limits use a sliding-window Lua script in Redis. Each rule has:

- `key` — the rate limit scope identifier
- `limit` — maximum requests in the window
- `window` — duration string (`1m`, `1h`, etc.)

When a limit is exceeded, the server responds with `429` and a `Retry-After` header giving the number of seconds until the window resets.

Rate limiting is **fail-closed**: if Redis is unreachable, requests are denied (not passed through).

---

## JWT key paths

In development, if `configs/jwt-private.pem` and `configs/jwt-public.pem` don't exist, the server auto-generates them. The generated keys are written to those paths for reuse across restarts.

In production, set the PEM content directly in environment variables:

```bash
JWT_PRIVATE_KEY="-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEA...
-----END RSA PRIVATE KEY-----"

JWT_PUBLIC_KEY="-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG...
-----END PUBLIC KEY-----"
```

---

## Production configuration checklist

- [ ] `APP_ENVIRONMENT=production`
- [ ] RSA keys injected via `JWT_PRIVATE_KEY` / `JWT_PUBLIC_KEY` environment variables
- [ ] `DB_PASSWORD` set to a strong, random password
- [ ] `REDIS_PASSWORD` set if Redis requires authentication
- [ ] `server.websocket.allowed_origins` contains only your actual frontend domains
- [ ] `database.postgresql.ssl_mode` set to `require` or `verify-full`
- [ ] `auth.bcrypt.cost` set to at least `12`
- [ ] `observability.logging.level` set to `info` (not `debug`)
- [ ] `observability.tracing.enabled` set to `true` with a valid exporter endpoint
