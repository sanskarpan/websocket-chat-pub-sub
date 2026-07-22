# ADR 0003: Graceful Shutdown Sequence and Connection Draining

## Status
Accepted

## Context
Deploying updates or restarting container instances must not drop active in-flight messages or corrupt database operations.

## Decision
We enforce a strict shutdown sequence triggered by `SIGINT` or `SIGTERM`:
1. Stop accepting new HTTP and WebSocket connections.
2. Shutdown WebSocket Hub (`wsServer.Shutdown`), sending close frames to connected clients and allowing active write pumps to flush buffers within a 30-second timeout context.
3. Shutdown REST API (`apiServer.Shutdown`) and Metrics HTTP server (`metricsServer.Shutdown`).
4. Wait for background subscription goroutines to terminate cleanly.
5. Close Redis client connections and PostgreSQL connection pool (`db.Close()`).

## Consequences
### Positive
- Zero in-flight message loss during blue/green or rolling container deployments.
- Avoids database connection pool errors during service shutdown.
