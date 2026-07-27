# Security Model

Defense-in-depth: authentication, authorization, rate limiting, input sanitization, token revocation, and fail-closed defaults throughout.

---

## Authentication

### RS256 JWT

All API and WebSocket requests require a valid RS256 JWT. RSA-2048 asymmetric signing means:

- The private key (signer) never leaves the server
- The public key (verifier) can be shared with edge services without granting signing ability
- Compromise of the public key alone does not allow forging tokens

See [Authentication](authentication.md) for the full token lifecycle.

### Token blacklisting

Every token carries a `jti` (JWT ID). On `POST /api/v1/auth/logout`, both the access and refresh token JTIs are written to Redis with TTLs matching their remaining validity. Every `ValidateToken` call checks the Redis blacklist before accepting a token.

This means logout is **immediate** — there is no window between logout and the access token's natural expiry during which the token remains usable.

### Refresh token rotation

Each use of a refresh token invalidates the old one and issues a new pair. If an attacker steals and uses a refresh token, the legitimate user's next refresh fails (the old JTI is blacklisted), providing a detectable signal.

---

## Authorization

### Room membership

Room-scoped endpoints (`/rooms/:id/*`, WebSocket `message`, `edit`, `delete`) verify that the authenticated user is a current member of the target room. The check uses `GetMember` with a `LEFT AT IS NULL` filter — users who have left a room cannot re-access it without re-joining.

### Message mutation

`EditMessage` and `DeleteMessage` enforce:

- **Edit**: only the message author may edit
- **Delete**: the message author, or any user with `admin` or `owner` role in the room

The authorization check runs in the **service layer**, not just the handler — it's enforced regardless of how the service is called.

### Sentinel errors for domain boundaries

`ErrRoomNotFound` and `ErrUserBanned` are exported sentinel errors from the service layer. Handlers use `errors.Is` to map them to specific HTTP status codes (404 and 403 respectively). This prevents information leakage — banned users get `403 Forbidden`, not a generic `500 Internal Server Error` that might reveal internal state.

---

## Rate limiting

All rate limits use a **sliding-window Lua script** in Redis — the window slides per request, preventing burst bypass at window boundaries.

| Scope | Default limit | Window |
|---|---|---|
| Auth endpoints | 10 | 1 minute |
| Message sending | 100 | 1 minute |
| WebSocket connections | 5 | 1 minute |
| Room creation | 10 | 1 hour |

**Fail-closed**: if Redis is unreachable, the rate limiter denies requests rather than passing them through. This prevents a Redis outage from becoming an open door to brute-force or spam attacks.

**`Retry-After` header**: every `429` response includes a `Retry-After: <seconds>` HTTP header, making it straightforward for clients to implement exponential backoff.

---

## Input sanitization

### XSS protection

Message content goes through two passes before persistence:

1. **Script tag stripping** — removes `<script>...</script>` blocks
2. **HTML entity escaping** — converts `<`, `>`, `&`, `"` to their entity equivalents

This is applied in `pkg/sanitization/` before the content reaches the database or is broadcast to other clients.

### Content length limit

WebSocket message content is capped at **4,000 characters**. Requests exceeding this limit are rejected with `CONTENT_TOO_LONG` before any service layer processing.

### Parameterized queries

All database access uses `pgx/v5` with parameterized queries throughout `internal/repository/`. There is no string concatenation of user input into SQL.

---

## Password security

- **bcrypt** with cost factor 12 (configurable)
- **Timing-attack mitigation**: when a login email is not found, the server still runs a dummy bcrypt hash comparison to normalize response time. An attacker cannot distinguish "email not found" from "wrong password" by timing.
- Password hashes are never returned in API responses

---

## CORS

CORS is config-driven: `cfg.Server.Websocket.AllowedOrigins` sets the allowed origin list. The middleware rejects pre-flight and actual requests from unlisted origins with `403`.

Configure allowed origins in production:

```yaml
server:
  websocket:
    allowed_origins:
      - "https://chat.example.com"
```

---

## Message deduplication

Client-provided `client_id` fields are stored in Redis with a 5-minute TTL using `SET NX EX`. This prevents duplicate messages when a client retransmits after a reconnect. Crucially, the dedup key is stored in **Redis** (not in-memory), so it works correctly across all pods in a k8s deployment.

---

## Production security checklist

| Control | Status |
|---|---|
| RS256 JWT with per-token JTI | ✅ |
| Token blacklist on logout (both access + refresh) | ✅ |
| Refresh token rotation (single-use) | ✅ |
| Sliding-window rate limiting with `Retry-After` | ✅ |
| Rate limit fail-closed on Redis failure | ✅ |
| Room membership check before any message operation | ✅ |
| bcrypt password hashing (cost 12) | ✅ |
| Timing-attack mitigation on login | ✅ |
| XSS sanitization of message content | ✅ |
| SQL injection prevention (parameterized queries) | ✅ |
| Config-driven CORS (not hardcoded) | ✅ |
| Distributed message deduplication | ✅ |
| Sentinel errors map to correct HTTP codes (no 500 leakage) | ✅ |
| Production mode enforces explicit JWT keys | ✅ |
| TLS termination at ingress | recommend (infra) |
| Key rotation procedure | recommend (ops) |

---

## Threat model

### In scope

- Credential theft via brute force → mitigated by rate limiting + bcrypt
- Session hijacking after logout → mitigated by JTI blacklisting
- Replay attacks with old refresh tokens → mitigated by token rotation
- Cross-site scripting in messages → mitigated by XSS sanitization
- SQL injection → mitigated by parameterized queries
- Unauthorized room access → mitigated by membership checks
- Duplicate message injection on reconnect → mitigated by distributed dedup

### Out of scope (infra responsibility)

- TLS/transport encryption — terminate at ingress
- Secrets management — use k8s Secrets, Vault, or a cloud secrets manager
- Database encryption at rest — configure at the PostgreSQL/disk level
- DDoS at the network layer — deploy behind a WAF or cloud DDoS protection
