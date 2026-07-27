# System Architecture

A single binary launches three independent HTTP servers. Redis Pub/Sub connects multiple replicas so any pod can receive any message.

---

## High-level diagram

```mermaid
graph TD
    Client["Browser / Mobile"] -->|"HTTP REST"| LB["Load Balancer<br>(Nginx / Traefik)"]
    Client -->|"WebSocket"| LB
    LB -->|":8085"| API["REST API<br>(Gin)"]
    LB -->|":8086"| WS1["WebSocket Hub<br>(Pod 1)"]
    LB -->|":8086"| WS2["WebSocket Hub<br>(Pod 2)"]
    LB -->|":8086"| WS3["WebSocket Hub<br>(Pod 3)"]
    API --> PG[("PostgreSQL")]
    API --> RD[("Redis")]
    WS1 --> PG
    WS2 --> PG
    WS3 --> PG
    WS1 <-->|"Pub/Sub fanout"| RD
    WS2 <-->|"Pub/Sub fanout"| RD
    WS3 <-->|"Pub/Sub fanout"| RD
    Prom["Prometheus"] -->|"scrape :9090"| M1["Metrics<br>(Pod 1)"]
    Prom -->|"scrape :9090"| M2["Metrics<br>(Pod 2)"]
    Prom -->|"scrape :9090"| M3["Metrics<br>(Pod 3)"]
```

---

## Server components

Three `net/http` servers launch from the same binary inside `cmd/server/main.go`:

| Server | Port | Router | Purpose |
|---|---|---|---|
| REST API | `8085` | Gin | Auth, rooms, messages — synchronous request/response |
| WebSocket Hub | `8086` | `gorilla/websocket` | Real-time bidirectional connections |
| Metrics | `9090` | `net/http` | Prometheus scrape target (`/metrics`) |

All three share the same service layer and connection pools. There is no inter-server IPC: the only shared state lives in PostgreSQL and Redis.

---

## Hub / Client pattern

```mermaid
sequenceDiagram
    participant C as Client
    participant H as Hub goroutine
    participant R as Redis Sub goroutine

    C->>H: register(client)
    H->>H: store in clients map
    R-->>H: Redis message received
    H->>C: broadcast via client.send channel
    C->>H: unregister(client)
    H->>H: delete from clients map, close send channel
```

`Hub` is the single goroutine that owns the `clients`, `rooms`, and `users` maps. It never shares these maps with other goroutines — all mutations go through its `register`, `unregister`, and `broadcast` channels. This is the Go channels-over-mutexes idiom applied strictly.

Each `Client` has:
- A read pump goroutine (reads from the WebSocket, writes to the Hub's channels)
- A write pump goroutine (reads from `client.send`, writes to the WebSocket)
- A buffered `send` channel (256 messages)

If `send` fills up (slow client), the hub closes the connection immediately — no goroutine leak, no unbounded memory growth.

---

## Redis Pub/Sub fanout

The key insight: every WebSocket pod subscribes to the same Redis channels. When Pod 1 receives a message from a user, it:

1. Persists to PostgreSQL
2. Publishes the serialized event to `ws:room:<room_id>` on Redis
3. All pods (including Pod 1) receive the event via their Redis subscription
4. Each pod fans the event out to local clients subscribed to that room

```mermaid
sequenceDiagram
    participant U as User (Pod 1)
    participant P1 as Pod 1 Hub
    participant R as Redis
    participant P2 as Pod 2 Hub
    participant V as Viewer (Pod 2)

    U->>P1: WebSocket "message" frame
    P1->>PG: INSERT INTO messages
    P1->>R: PUBLISH ws:room:X {event}
    R-->>P1: event (own subscription)
    R-->>P2: event
    P1->>U: new_message (local fan-out)
    P2->>V: new_message (remote fan-out)
```

Redis Pub/Sub channels used:

| Channel | Event types |
|---|---|
| `ws:room:<room_id>` | `new_message`, `message_updated` (edit/delete), reactions |
| `ws:room:<room_id>:events` | `member_joined`, `member_left` |
| `ws:presence` | `presence` (online/away/dnd/offline) |

---

## Distributed deduplication

A client sends a `client_id` field with each message. Before persisting, the service calls `pubsub.SetDedup(ctx, clientID, userID)` which does a Redis `SET NX EX 300` (5 minutes). If the key already exists, the original message is returned instead of creating a duplicate.

This is correct under k8s `replicas: 3` because Redis is the shared dedup store — not a per-pod `sync.Map`.

---

## Redis key inventory

| Key pattern | Type | TTL | Purpose |
|---|---|---|---|
| `ratelimit:<key>` | Sorted Set | window duration | Sliding-window rate limit counters |
| `presence:<user_id>` | String (JSON) | 5 min | User presence state |
| `blacklist:<jti>` | String | token TTL | Revoked JWT IDs |
| `dedup:<client_id>:<user_id>` | String | 5 min | Message dedup |
| `room:<id>` | String (JSON) | 5 min | Room object cache |

---

## Data flow: send a message

```mermaid
flowchart TD
    A["Client sends 'message' frame"] --> B["ReadPump receives frame"]
    B --> C["JSON decode + validate"]
    C --> D["CheckRateLimit via Redis Lua"]
    D -->|"over limit"| E["Send 'error' RATE_LIMITED to client"]
    D -->|"ok"| F["Check room subscription"]
    F -->|"not subscribed"| G["Send 'error' NOT_SUBSCRIBED"]
    F -->|"subscribed"| H["GetDedup — check client_id"]
    H -->|"duplicate"| I["Return original message, send 'ack'"]
    H -->|"new"| J["INSERT INTO messages (PostgreSQL)"]
    J --> K["PUBLISH to Redis ws:room:<id>"]
    K --> L["Redis delivers to all pods"]
    L --> M["Hub fans out 'new_message' to subscribed clients"]
    J --> N["Send 'ack' to sender"]
```

---

## Graceful shutdown sequence

On `SIGINT` or `SIGTERM`, the ordered shutdown runs inside a 30-second context:

1. Stop accepting new HTTP connections (REST API)
2. Call `wsServer.Shutdown` — send WebSocket close frames, wait for write pumps to flush
3. Shutdown REST API server and Metrics server
4. Wait for background Redis subscription goroutines to exit
5. Close Redis client connections
6. Close PostgreSQL connection pool (`db.Close()`)

This order ensures: no new work is accepted before existing work drains, and dependencies are closed last. See [ADR 0003](adr/0003-graceful-shutdown-and-connection-draining.md).

---

## Package layout

```
cmd/server/main.go           ← entry point: wires all dependencies, starts servers
internal/
  config/                    ← YAML config loading + env var overrides + validation
  handlers/                  ← HTTP handlers (REST endpoints)
  logging/                   ← zerolog structured JSON logging setup
  metrics/                   ← Prometheus metric definitions
  middleware/                 ← CORS, rate-limit, auth, request-ID middleware
  model/                     ← domain types: User, Room, Message, Presence
  protocol/                  ← WebSocket message types and constants
  pubsub/                    ← Redis Pub/Sub + rate limit + dedup + presence
  repository/                ← PostgreSQL data access layer (pgx/v5)
  server/                    ← WebSocket Hub + Client (read/write pumps)
  service/                   ← business logic: AuthService, RoomService, MessageService
  tracing/                   ← OpenTelemetry span helpers
pkg/
  sanitization/              ← XSS strip + HTML entity escape
  snowflake/                 ← Snowflake ID generator (unique message IDs)
```
