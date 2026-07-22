# ADR 0001: JWT Authentication and Service Authorization

## Status
Accepted

## Context
The real-time WebSocket chat platform requires stateless, scalable authentication across REST endpoints and WebSocket upgrade requests without forcing per-request database lookups.

## Decision
We implement JSON Web Token (JWT) authentication using HMAC-SHA256 (HS256).
- Short-lived Access Tokens (15 minutes TTL) contain `sub` (User ID), `iss` (Issuer), `aud` (Audience), and `type` ("access").
- Refresh Tokens (7 days TTL) contain `jti` (UUID) and `type` ("refresh") to facilitate token rotation.
- Token validation is enforced before WebSocket upgrading via query parameter `?token=` or standard `Authorization: Bearer <token>` header.
- Explicit service-level authorization is enforced on all message editing and deletion endpoints.

## Consequences
### Positive
- Stateless verification enables linear horizontal scaling across multiple REST and WS nodes.
- Short access token lifetimes reduce impact of token compromise.
- Strict authorization checks in `message_service.go` prevent unauthorized mutation of chat resources.

### Negative
- Revocation of access tokens prior to expiration requires blacklist mechanisms or Redis checking if instant revocation is desired.

## Alternatives Considered
- **Session ID with Central Database/Redis**: High state overhead and database latency on every WebSocket frame check.
