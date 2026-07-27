# ADR 0002: Redis Pub/Sub for Cross-Node Fanout and Distributed Deduplication

## Status

Accepted

---

## Context

When running multiple server instances behind a load balancer, clients connected to Instance A need to receive messages sent by users connected to Instance B. Without a cross-node broadcast mechanism, messages would only reach clients on the same pod as the sender.

Additionally, message deduplication (preventing duplicate messages on client reconnect) was originally implemented with a per-pod `sync.Map`. This is broken at `replicas: 3` — each pod has its own independent dedup map, so a reconnect to a different pod would not see the original dedup entry.

---

## Decision

### Pub/Sub fanout

Redis Pub/Sub channels are used for real-time cross-node message distribution:

| Channel | Events |
|---|---|
| `ws:room:<room_id>` | `new_message`, `message_updated` (edit/delete), reactions |
| `ws:room:<room_id>:events` | `member_joined`, `member_left` |
| `ws:presence` | User online/offline/away/dnd status changes |

Every pod subscribes to the channels for rooms with active local clients. When a message arrives via REST or WebSocket on any pod, that pod:

1. Persists to PostgreSQL
2. Publishes the serialized event to the relevant Redis channel
3. Every pod (including the publisher) receives the event via its subscription
4. Each pod fans out locally to clients subscribed to that room

This design means no sticky sessions are needed — any pod can handle any client and still receive all events.

**Exponential backoff reconnect loops** wrap all subscription goroutines (`subscribeToRoomMessages` and `subscribeToPresence`). If Redis drops the connection, the goroutine retries with increasing delay (1s, 2s, 4s, … up to 60s) until the subscription is re-established.

### Distributed deduplication

Client-provided `client_id` fields are stored in Redis with a 5-minute TTL:

```
SET dedup:<client_id>:<user_id> <message_id> NX EX 300
```

`NX` (only set if not exists) is atomic. The first pod to process a given `client_id` wins; all others see the key and return the original message. This works correctly across all replicas.

---

## Consequences

### Positive

- **No sticky sessions**: any pod can receive any client; load balancer can use round-robin.
- **Linear horizontal scaling**: adding pods adds connection capacity without coordination.
- **Automatic Redis reconnection**: exponential backoff prevents thundering herd on Redis restart.
- **Correct dedup under replicas**: Redis `SET NX EX` is atomic and shared across pods.
- **PostgreSQL as source of truth**: Redis Pub/Sub is delivery-only. Messages are persisted to PostgreSQL before publication, so a Redis event drop means "no real-time delivery" not "message lost".

### Negative

- **Redis Pub/Sub has no durability**: if no subscribers are active at the moment of publication (e.g., Redis just restarted and subscriptions haven't re-established), the event is dropped. Mitigated by:
  - PostgreSQL persistence before publication
  - Client-side reconnect with message history re-fetch via REST API
- **Redis is a required dependency**: all cross-node delivery depends on Redis. Redis Sentinel or Cluster is recommended for production high availability.
- **Fan-out to all pods**: in a large cluster (e.g., 50 pods), every pod receives every room event, even if it has no local subscribers for that room. This is acceptable at the expected scale; at extremely high pod counts, a more targeted routing layer would be needed.

---

## Alternatives considered

### In-memory mesh (direct pod-to-pod connections)

Each pod would need to know about all other pods and maintain connections to them. Requires a service discovery mechanism, connection management, and fails when pods are added or removed dynamically. Operational complexity is significantly higher than Redis Pub/Sub.

### WebSocket-aware load balancer with sticky sessions

Sticky sessions route the same client to the same pod. This avoids cross-pod fanout but creates uneven pod load and makes rolling updates tricky (must drain a pod's connections before removing it). It also doesn't solve the dedup problem.

### Redis Streams instead of Pub/Sub

Redis Streams provide message persistence and consumer groups. Suitable when you need guaranteed delivery from the queue layer, not just fan-out notification. For this use case, PostgreSQL is the persistence layer; Redis Pub/Sub is sufficient for real-time delivery notifications.
