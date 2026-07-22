# Production Operational Runbook

## Service Overview
- **Service Name**: `websocket-chat`
- **Primary Listener Ports**: REST API (`8085`), WebSocket (`8086`), Prometheus Metrics (`9090`)
- **Health Endpoints**: Liveness (`/healthz`), Readiness (`/readyz`)

---

## Deployment & Rollback Procedures

### Deployment Protocol
1. Verify CI/CD pipeline green status on main branch.
2. Build production image with tag: `docker build -t websocket-chat:<version> .`
3. Execute rolling update in Kubernetes / Docker Swarm (maximum 25% unavailable rate).
4. Monitor `/readyz` endpoint and Prometheus metrics (`http_request_duration_seconds`, `websocket_connections_active`).

### Rollback Protocol
If 5xx error rate exceeds 1% or `/readyz` probe fails continuously for 60 seconds:
1. Revert container image tag to previous stable release: `kubectl rollout undo deployment/websocket-chat`
2. Confirm readiness probes return `200 OK`.
3. Inspect logs via zerolog JSON parser: `kubectl logs -l app=websocket-chat --tail=500 | jq '.'`

---

## Top 5 Production Failure Modes & Diagnosis

### 1. Database Connection Pool Exhaustion (`500 Internal Server Error`)
- **Symptom**: REST API returns `INTERNAL_ERROR`, high latency spikes on database queries.
- **Diagnosis**: Check `db_query_duration_seconds` and PostgreSQL active connections: `SELECT count(*) FROM pg_stat_activity;`.
- **Mitigation**: Increase `max_open_conns` in configuration or scale read replicas.

### 2. Redis PubSub Disconnection / Fanout Freeze
- **Symptom**: Chat messages save successfully via REST, but clients connected on other instances do not receive real-time updates.
- **Diagnosis**: Inspect server logs for `"Failed to subscribe to room messages, retrying..."`. Check Redis memory & CPU metrics.
- **Mitigation**: Verify Redis instance network availability; reconnect loop will automatically recover once Redis is reachable.

### 3. WebSocket Slow Client Drop / Channel Backpressure
- **Symptom**: High client disconnect logs (`Client disconnected`) with message buffer drop logs.
- **Diagnosis**: Check metric `websocket_connection_errors_total{error_type="write_timeout"}`.
- **Mitigation**: Client connection is unable to keep up with network throughput. Hub automatically cleans up resources without memory growth.

### 4. JWT Secret Misconfiguration on Startup
- **Symptom**: Server fails to start with log: `Invalid configuration: production environment requires a secure JWT_SECRET`.
- **Diagnosis**: `JWT_SECRET` environment variable is missing or fewer than 32 characters in production mode.
- **Mitigation**: Inject valid 32-byte secret into container environment variables.

### 5. High Memory Usage / Goroutine Accumulation
- **Symptom**: Memory consumption trends upwards continuously.
- **Diagnosis**: Check Prometheus gauge `websocket_connections_active` vs OS memory usage.
- **Mitigation**: Capture pprof heap profile: `go tool pprof http://localhost:9090/debug/pprof/heap`.

---

## On-Call Escalation Path
1. **Tier 1 (Automated Alert)**: Prometheus alert fires to PagerDuty.
2. **Tier 2 (On-Call Engineer)**: Performs triage using `/readyz` and log search.
3. **Tier 3 (Database / Infra Lead)**: Escalated if PostgreSQL cluster state is degraded or unrecoverable.
