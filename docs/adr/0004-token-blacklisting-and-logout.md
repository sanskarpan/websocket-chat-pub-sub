# ADR 0004: Token Blacklisting and Logout Endpoint

## Status

Accepted

---

## Context

JWT access tokens are stateless by design — once issued, they are valid until their `exp` claim expires. Without explicit revocation, a user who logs out still has a valid access token for up to 15 minutes. An attacker who steals that token can use it for the remainder of its lifetime.

The original implementation lacked a logout endpoint entirely. Refresh tokens had a `jti` field, but access tokens did not — making access token revocation impossible even if the infrastructure supported it.

---

## Decision

### `jti` on all token types

Access tokens now carry a `jti` (UUID) claim alongside refresh tokens. Both are generated at issuance via `uuid.New().String()` and stored in the signed payload.

### Blacklist storage in Redis

Revoked JTIs are stored in Redis as:

```
SET blacklist:<jti> "1" EX <remaining_ttl_seconds>
```

The TTL is set to the token's remaining validity at the time of revocation — not the original full TTL. This means the Redis key expires exactly when the token would have expired naturally, keeping the blacklist lean.

### `validateToken` enforces the blacklist

Every call to `authService.ValidateToken` (on every authenticated REST request and every WebSocket upgrade) runs:

```go
invalidated, err := s.tokenInvalidator.IsTokenInvalidated(ctx, jti)
if invalidated {
    return nil, errors.New("token has been revoked")
}
```

There is no fast path that skips this check.

### `POST /api/v1/auth/logout` endpoint

The endpoint:

1. Requires `Authorization: Bearer <access_token>` (enforced by `AuthMiddleware` on the route)
2. Uses `jwt.Parser.ParseUnverified` to extract both jtis **without re-verifying signatures**
3. Calls `invalidateToken` for the access token's jti (TTL = `Auth.JWT.AccessTokenTTL`)
4. If a `refresh_token` is in the request body: calls `invalidateToken` for its jti (TTL = `Auth.JWT.RefreshTokenTTL`)
5. Returns `200 {"status": "logged_out"}`

`ParseUnverified` is used because: (a) `AuthMiddleware` already validated the signature before the handler runs, and (b) a nearly-expired token might fail full validation due to clock skew — we still want to blacklist its jti.

### Token rotation on refresh

`POST /api/v1/auth/refresh` also blacklists the incoming refresh token's jti before issuing a new pair. This ensures each refresh token is single-use.

---

## Consequences

### Positive

- **Immediate logout**: a token blacklisted at logout is invalid on the very next request, not at expiry.
- **Both tokens revoked**: access and refresh tokens are both blacklisted on logout, closing both attack vectors.
- **Cross-node enforcement**: Redis is the shared blacklist store — all pods enforce revocation consistently.
- **Lean blacklist**: TTLs match remaining token validity; no manual cleanup needed.
- **Single-use refresh tokens**: rotation + immediate blacklisting of old refresh tokens detects stolen refresh tokens.

### Negative

- **Redis is in the hot path**: every token validation does one Redis read. At 10,000 concurrent connections each sending messages, this is ~10,000 Redis reads per second. Redis is well within capacity for this workload, but it is now a hard dependency for authentication.
- **`ParseUnverified` usage**: care must be taken to ensure `ParseUnverified` is only used in the logout handler, where the token has already been validated by middleware. It must not be used elsewhere as a shortcut to skip signature verification.
- **No "logout all sessions" yet**: the current implementation revokes individual tokens. A "logout from all devices" feature would require storing all active jtis per user and revoking the entire set — not yet implemented.

---

## Alternatives considered

### Short TTL only (no blacklist)

Accept that tokens are valid until expiry. Simple, but a 15-minute window is unacceptable for logout-sensitive use cases (e.g., shared/public devices).

### Opaque session tokens with database lookup

Session ID stored in a database table, invalidated on logout by deleting the row. Correct but requires a database round-trip on every request — doesn't scale to WebSocket frame-rate validation.

### Redis Set (SADD) instead of individual keys

Store all blacklisted JTIs in a single Redis Set. Simpler key structure but makes per-JTI TTLs impossible — the Set would need a separate cleanup job. Individual keys with natural TTL expiry are cleaner.
