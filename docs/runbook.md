# Production Runbook

On-call playbook for `websocket-chat`.

---

## Service overview

| Component | Port | Health URL |
|---|---|---|
| REST API | `8085` | `GET /readyz` |
| WebSocket Hub | `8086` | (TCP connect) |
| Prometheus Metrics | `9090` | `GET /metrics` |

Liveness probe: `GET /healthz` — always `200` if the process is running.

Readiness probe: `GET /readyz` — `200` only when both PostgreSQL and Redis are reachable.

---

## Deployment & rollback

### Rolling update

```bash
kubectl set image deployment/websocket-chat server=websocket-chat:<new-version>
kubectl rollout status deployment/websocket-chat
kubectl get pods -l app=websocket-chat
```

The server handles `SIGTERM` gracefully with a 30-second drain. Set `terminationGracePeriodSeconds: 35` in the deployment manifest.

### Rollback

If 5xx error rate exceeds 1% or `/readyz` fails continuously for 60 seconds:

```bash
kubectl rollout undo deployment/websocket-chat
kubectl rollout status deployment/websocket-chat
curl http://pod-ip:8085/readyz
```

---

## Top failure modes

### 1. Database connection pool exhaustion (500 errors)

**Symptoms**: REST API returns `INTERNAL_ERROR`, high latency on database queries.

**Diagnosis**:
```bash
psql -h $DB_HOST -U chat -d chat -c "SELECT count(*) FROM pg_stat_activity WHERE state = 'active';"
curl http://pod-ip:9090/metrics | grep db_connection_pool
```

**Mitigation**: Kill long-running idle transactions; increase `database.postgresql.max_open_conns` if connections are legitimately needed.

---

### 2. Redis Pub/Sub disconnection / fanout freeze

**Symptoms**: Messages save to PostgreSQL (REST returns 200) but clients on other pods don't receive real-time updates.

**Diagnosis**:
```bash
kubectl logs -l app=websocket-chat --tail=200 | grep -E "subscribe|redis|pubsub" | jq .
kubectl exec -it <pod> -- redis-cli -h $REDIS_HOST ping
```

**Mitigation**: The exponential backoff reconnect loop re-establishes subscriptions automatically. Restore Redis availability if down; check network policies if Redis is up but pods aren't reconnecting.

---

### 3. WebSocket slow client / channel backpressure

**Symptoms**: High disconnect rate, `websocket_connection_errors_total{error_type="write_timeout"}` increasing.

**Mitigation**: The Hub automatically closes connections when the send channel fills (256 messages). This is correct behavior — no action needed unless the rate is abnormally high.

---

### 4. JWT key misconfiguration on startup

**Symptoms**: Server fails to start with `"production environment requires explicit JWT keys"`.

**Diagnosis**:
```bash
kubectl logs <pod> | head -30
kubectl describe secret chat-secrets
echo "$JWT_PRIVATE_KEY" | openssl rsa -check
```

!!! warning "Correct environment variable"
    Use `JWT_PRIVATE_KEY` (PEM content), not `JWT_SECRET`. The old RUNBOOK.md incorrectly referenced `JWT_SECRET` from an earlier HS256 implementation that has been replaced by RS256.

---

### 5. Logout failure — tokens remain valid after logout

**Symptoms**: After `POST /api/v1/auth/logout` returns `200`, subsequent requests with the same access token still return `200` instead of `401`.

**Diagnosis**:
```bash
# Decode the token to get the jti, then check Redis
redis-cli -h $REDIS_HOST GET "blacklist:<jti>"
# Should return "1"
```

**Mitigation**: If Redis was unreachable at logout time, blacklisting silently failed. Fix Redis connectivity. As emergency mitigation, rotate the RSA private key — all previously-issued tokens fail signature verification immediately.

---

### 6. High memory / goroutine accumulation

**Symptoms**: Memory grows continuously; `websocket_connections_active` is low.

**Diagnosis**:
```bash
curl http://pod-ip:9090/debug/pprof/goroutine?debug=1 | head -20
go tool pprof http://pod-ip:9090/debug/pprof/heap
```

**Mitigation**: Look for `ReadPump` goroutines blocked on network reads with no associated unregister event. Restart the affected pod as immediate mitigation.

---

## On-call escalation path

1. **Tier 1 (automated alert)**: Prometheus alert → PagerDuty
2. **Tier 2 (on-call engineer)**: triage via `/readyz`, `kubectl logs`, Prometheus dashboards
3. **Tier 3 (database / infra lead)**: escalate for PostgreSQL cluster degradation, Redis cluster issues, or TLS certificate problems
