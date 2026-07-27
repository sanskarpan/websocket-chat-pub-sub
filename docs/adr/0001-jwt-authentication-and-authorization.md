# ADR 0001: JWT Authentication with RS256 and JTI Blacklisting

## Status

Accepted

---

## Context

The real-time WebSocket chat platform requires stateless, scalable authentication across REST endpoints and WebSocket upgrade requests without forcing per-request database lookups.

Token revocation is a fundamental requirement: users must be able to log out and have their session immediately invalidated — even before the access token's natural expiry.

---

## Decision

**RS256 asymmetric JWT signing** is used for all tokens:

- **Access tokens** (15-minute TTL): contain `sub` (User ID), `jti` (UUID, unique per token), `iss`, `aud`, `type: "access"`.
- **Refresh tokens** (7-day TTL): contain `sub`, `jti` (UUID, unique per token), `iss`, `aud`, `type: "refresh"`.
- **Both token types carry `jti`**, not just refresh tokens. This enables blacklisting of access tokens on logout.

Token validation runs the following checks in order:

1. RS256 signature verification against the public key
2. `exp` claim (not expired)
3. `type` claim (must match expected type — access vs refresh)
4. **Redis blacklist check**: `SISMEMBER blacklist:<jti>` — if found, reject immediately

On `POST /api/v1/auth/logout`, both the access token's `jti` and the refresh token's `jti` are written to Redis with TTLs equal to their remaining validity. This makes logout immediate and cross-node.

WebSocket authentication uses the access token via the `?token=` query parameter on the upgrade request. The same validation logic runs before the WebSocket upgrade completes.

---

## Consequences

### Positive

- **Stateless validation**: RSA signature verification requires no database lookup on every request. Only the blacklist check hits Redis — a sub-millisecond operation.
- **Immediate revocation**: JTI blacklisting means logout takes effect on the very next request, not at token expiry. This is a security property most JWT implementations lack.
- **Cross-node consistency**: Redis is the shared blacklist store across all pods. No pod-local state can be out of sync.
- **Horizontal scaling**: stateless verification enables linear scaling across multiple REST and WS nodes.
- **Asymmetric key flexibility**: the public key can be shared with edge services or API gateways for verification without granting signing ability.

### Negative

- **Redis dependency for every token validation**: a Redis outage blocks all authenticated requests. Mitigated by Redis high availability (Sentinel or Cluster in production).
- **Short access token TTL increases refresh frequency**: 15-minute access tokens mean clients must call `POST /auth/refresh` roughly every 14 minutes for long sessions.
- **`ParseUnverified` in logout handler**: the logout handler uses `jwt.Parser.ParseUnverified` to extract the JTI without re-verifying the signature (since AuthMiddleware already did that). This is intentional — it handles near-expired tokens — but must not be used anywhere else.

---

## Alternatives considered

### HS256 (symmetric HMAC)

The original SPEC described HS256. Rejected because:
- The signing secret must be shared across all nodes that verify tokens, including any future edge/API-gateway layer
- A leaked secret compromises all issued tokens, not just one
- RS256 asymmetric keys allow verification without sharing signing capability

### Session ID with database/Redis lookup

Per-request database lookups don't scale to 10,000+ concurrent WebSocket connections where every message frame would require a lookup. Redis session store would work but adds session state that JWT avoids.

### Opaque tokens

Simpler to implement but require a central lookup on every validation — same scaling concern as database sessions.
