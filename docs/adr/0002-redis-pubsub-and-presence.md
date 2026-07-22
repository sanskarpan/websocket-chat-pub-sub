# ADR 0002: Redis PubSub for Cross-Node Message Fanout and Presence tracking

## Status
Accepted

## Context
When running multiple server instances behind a load balancer, clients connected to Instance A need to receive messages and presence updates sent by users connected to Instance B.

## Decision
We utilize Redis PubSub and Redis Hashes for real-time channel fanout and presence management:
- `ws:room:<room_id>`: Channel for broadcasting room chat messages, edits, and deletions.
- `ws:room:<room_id>:events`: Channel for member join and leave events.
- `ws:presence`: Channel for broadcasting user online/offline status changes.
- `presence:<user_id>`: Key storing presence state with 5-minute TTL.
- Exponential backoff reconnect loops wrap subscription goroutines (`subscribeToRoomMessages` and `subscribeToPresence`) to guarantee resilience against transient Redis network blips.

## Consequences
### Positive
- Enables seamless multi-node scale-out without inter-node mesh connections.
- Automatic recovery from Redis disconnections via exponential backoff reconnect loops.

### Negative
- Redis PubSub does not guarantee message persistence if no subscribers are active at message publication time (mitigated by storing all messages in PostgreSQL first).
