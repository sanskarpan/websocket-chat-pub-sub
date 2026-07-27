# Monitoring & Observability

Three pillars: Prometheus metrics, structured JSON logs, and health endpoints for k8s probes.

---

## Health endpoints

| Endpoint | Type | Success condition |
|---|---|---|
| `GET /healthz` | Liveness | Process is running — always `200` |
| `GET /readyz` | Readiness | PostgreSQL and Redis both reachable — `200` ok, `503` degraded |
| `GET /health` | Readiness | Alias for `/readyz` |

Configure your Kubernetes readiness probe against `/readyz` and liveness against `/healthz`. The 5-second initial delay is sufficient for the server to connect to its dependencies.

---

## Prometheus metrics

The metrics server runs on port `9090`. Scrape endpoint: `http://host:9090/metrics`.

### Application metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `websocket_connections_active` | Gauge | — | Active WebSocket connections across all rooms |
| `room_subscriptions_active` | Gauge | — | Active room subscriptions |
| `websocket_messages_sent_total` | Counter | — | Messages sent to WebSocket clients |
| `websocket_messages_received_total` | Counter | — | Messages received from WebSocket clients |
| `websocket_connection_errors_total` | Counter | `error_type` | Connection errors by type |
| `auth_attempts_total` | Counter | `status` | Auth attempts with `status=success|failure` |
| `rate_limited_requests_total` | Counter | `key` | Rate-limited requests by rule key |

### Infrastructure metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `db_query_duration_seconds` | Histogram | `query_type` | PostgreSQL query latency |
| `redis_operation_duration_seconds` | Histogram | `operation` | Redis operation latency |
| `http_request_duration_seconds` | Histogram | `method`, `path`, `status` | REST API request latency |

### Prometheus scrape config

```yaml
# prometheus.yml
scrape_configs:
  - job_name: websocket-chat
    static_configs:
      - targets:
          - "pod1:9090"
          - "pod2:9090"
          - "pod3:9090"
    scrape_interval: 15s
```

---

## Alerting rules

```yaml
# alert_rules.yml
groups:
  - name: websocket-chat
    rules:
      - alert: HighConnectionCount
        expr: websocket_connections_active > 8000
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "WebSocket connections approaching limit on {{ $labels.instance }}"

      - alert: HighErrorRate
        expr: rate(websocket_connection_errors_total[5m]) > 0.01
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "WebSocket error rate > 1% on {{ $labels.instance }}"

      - alert: ReadyzFailing
        expr: probe_success{job="websocket-chat-readyz"} == 0
        for: 60s
        labels:
          severity: critical
        annotations:
          summary: "Readiness probe failing on {{ $labels.instance }}"

      - alert: DBConnectionPoolHigh
        expr: db_connection_pool_in_use / db_connection_pool_size > 0.8
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "DB connection pool > 80% on {{ $labels.instance }}"

      - alert: HighAuthFailureRate
        expr: rate(auth_attempts_total{status="failure"}[5m]) > 10
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "High auth failure rate — possible credential stuffing"
```

---

## Structured logging

All logs are JSON-formatted (`zerolog`) and written to stdout. Log to stdout; let your infrastructure (Loki, CloudWatch, Datadog, etc.) aggregate them.

### Log fields

| Field | Description |
|---|---|
| `level` | `debug`, `info`, `warn`, `error` |
| `time` | ISO 8601 timestamp |
| `message` | Human-readable event description |
| `request_id` | UUID correlation ID, propagated across all log lines for a request |
| `component` | Source subsystem (`auth`, `hub`, `pubsub`, `repository`, etc.) |
| `duration_ms` | Request or operation duration in milliseconds |
| `user_id` | Authenticated user ID (where applicable) |
| `room_id` | Room ID (where applicable) |
| `error` | Error message string (on error-level logs) |

### Example log lines

```json
{"level":"info","time":"2026-01-01T00:00:00Z","message":"REST API server started","port":8085}
{"level":"info","time":"2026-01-01T00:00:01Z","message":"client connected","component":"hub","user_id":"u-123","session_id":"s-456"}
{"level":"warn","time":"2026-01-01T00:00:02Z","message":"rate limit exceeded","component":"middleware","key":"message","user_id":"u-123"}
{"level":"error","time":"2026-01-01T00:00:03Z","message":"failed to subscribe to room messages, retrying","component":"pubsub","error":"connection refused","retry_in":"2s"}
```

### Log levels

| Environment | Recommended level |
|---|---|
| Development | `debug` |
| Staging | `info` |
| Production | `info` |

Set via `observability.logging.level` in config or the `LOG_LEVEL` environment variable.

---

## Distributed tracing

OpenTelemetry tracing is supported but disabled by default. Enable in config:

```yaml
observability:
  tracing:
    enabled: true
    exporter: "jaeger"
    jaeger:
      endpoint: "http://jaeger:14268/api/traces"
    sampling_rate: 0.01   # 1% sampling in production
```

Trace spans are created for:
- REST API request handling
- PostgreSQL queries
- Redis operations
- WebSocket message handling
- Pub/Sub publish/subscribe

Set `sampling_rate: 1.0` temporarily when debugging a specific flow, then revert to `0.01`.

---

## Grafana dashboard

Import the dashboard from `deployments/grafana/dashboard.json` (or create one from the metric names above). Key panels:

1. **Active connections** — `websocket_connections_active` gauge
2. **Messages/sec** — `rate(websocket_messages_sent_total[1m])`
3. **REST API latency P99** — `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))`
4. **DB query latency P99** — `histogram_quantile(0.99, rate(db_query_duration_seconds_bucket[5m]))`
5. **Auth failure rate** — `rate(auth_attempts_total{status="failure"}[5m])`
6. **Rate-limited requests** — `rate(rate_limited_requests_total[1m])` by `key`

---

## pprof profiling

The metrics server exposes Go's built-in pprof endpoints:

```bash
# CPU profile (30 seconds)
go tool pprof http://localhost:9090/debug/pprof/profile?seconds=30

# Heap profile
go tool pprof http://localhost:9090/debug/pprof/heap

# Goroutine dump (useful for leak detection)
curl http://localhost:9090/debug/pprof/goroutine?debug=1
```

Use these when `websocket_connections_active` falls but memory stays high — that's a goroutine or client leak.
