# REST API Reference

Base URL: `http://host:8085`

All authenticated endpoints require `Authorization: Bearer <access_token>`.

---

## Health & discovery

### `GET /`

Service metadata and endpoint map.

```http
GET /
```

Response `200`:
```json
{
  "service": "websocket-chat",
  "version": "1.0.0",
  "endpoints": { ... }
}
```

### `GET /healthz`

Liveness probe. Always returns `200` if the process is running.

```http
GET /healthz
```

Response `200`:
```json
{"status": "ok"}
```

### `GET /readyz`

Readiness probe. Returns `200` only when both PostgreSQL and Redis are reachable.

```http
GET /readyz
```

Response `200` (ready):
```json
{"status": "ok"}
```

Response `503` (degraded):
```json
{"status": "error", "error": "database unreachable"}
```

### `GET /health`

Alias for `/readyz`.

---

## Authentication

All auth endpoints are rate-limited at **10 requests per minute per IP**.

### `POST /api/v1/auth/register`

Create a new user account.

```http
POST /api/v1/auth/register
Content-Type: application/json
```

Request body:
```json
{
  "username": "johndoe",
  "email": "john@example.com",
  "password": "securePass123!",
  "display_name": "John Doe"
}
```

| Field | Required | Constraints |
|---|---|---|
| `username` | yes | 3–30 chars, alphanumeric + `_-` |
| `email` | yes | valid email format |
| `password` | yes | min 8 chars |
| `display_name` | no | max 100 chars |

Response `201`:
```json
{
  "id": "user-snowflake-id",
  "username": "johndoe",
  "email": "john@example.com",
  "display_name": "John Doe",
  "created_at": "2026-01-01T00:00:00Z"
}
```

Response `400` (validation error):
```json
{"code": "VALIDATION_ERROR", "message": "username must be at least 3 characters"}
```

Response `409` (duplicate):
```json
{"code": "CONFLICT", "message": "email already registered"}
```

---

### `POST /api/v1/auth/login`

Authenticate and receive a token pair.

```http
POST /api/v1/auth/login
Content-Type: application/json
```

Request body:
```json
{
  "email": "john@example.com",
  "password": "securePass123!"
}
```

Response `200`:
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "refresh_token": "dGhpcyBpcyBhIHJlZnJl...",
  "token_type": "Bearer",
  "expires_in": 900
}
```

`expires_in` is in seconds (default: 900 = 15 minutes for access tokens).

Response `401`:
```json
{"code": "INVALID_CREDENTIALS", "message": "Invalid email or password"}
```

!!! note "Timing attack mitigation"
    When the email is not found, the server still runs a dummy bcrypt comparison to normalize response time. This prevents user enumeration via timing.

---

### `POST /api/v1/auth/refresh`

Exchange a refresh token for a new token pair. The old refresh token is invalidated (token rotation).

```http
POST /api/v1/auth/refresh
Content-Type: application/json
```

Request body:
```json
{
  "refresh_token": "dGhpcyBpcyBhIHJlZnJl..."
}
```

Response `200`:
```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "refresh_token": "bmV3IHJlZnJlc2g...",
  "token_type": "Bearer"
}
```

Response `401` (expired or revoked refresh token):
```json
{"code": "INVALID_TOKEN", "message": "Refresh token is invalid or expired"}
```

---

### `POST /api/v1/auth/logout`

**Requires:** `Authorization: Bearer <access_token>`

Invalidate the current session. Blacklists the `jti` of both the access token and the refresh token in Redis. Both tokens are immediately unusable.

```http
POST /api/v1/auth/logout
Authorization: Bearer <access_token>
Content-Type: application/json
```

Request body (optional — include refresh token to blacklist it too):
```json
{
  "refresh_token": "dGhpcyBpcyBhIHJlZnJl..."
}
```

Response `200`:
```json
{"status": "logged_out"}
```

!!! tip "Best practice"
    Always send both the access token (in the `Authorization` header) and the refresh token (in the body). This ensures neither can be used after logout, even before the access token expires.

---

## Rooms

All room endpoints require `Authorization: Bearer <access_token>`.

### `GET /api/v1/rooms`

List rooms the authenticated user is a member of.

```http
GET /api/v1/rooms
Authorization: Bearer <access_token>
```

Response `200`:
```json
[
  {
    "id": "room-snowflake-id",
    "name": "general",
    "type": "channel",
    "description": "General discussion",
    "created_by": "user-snowflake-id",
    "member_count": 42,
    "created_at": "2026-01-01T00:00:00Z",
    "settings": {
      "allow_reactions": true,
      "allow_threads": true,
      "message_retention": 365,
      "slow_mode_seconds": 0,
      "require_approval": false,
      "only_admins_can_post": false
    }
  }
]
```

---

### `POST /api/v1/rooms`

Create a new room. Rate-limited at **10 room creations per hour per user**.

```http
POST /api/v1/rooms
Authorization: Bearer <access_token>
Content-Type: application/json
```

Request body:
```json
{
  "name": "general",
  "type": "channel",
  "description": "General discussion"
}
```

| Field | Required | Values |
|---|---|---|
| `name` | yes | 1–100 chars |
| `type` | yes | `direct`, `group`, `channel` |
| `description` | no | free text |

Response `201`:
```json
{
  "id": "room-snowflake-id",
  "name": "general",
  "type": "channel",
  "description": "General discussion",
  "created_by": "user-snowflake-id",
  "member_count": 1,
  "created_at": "2026-01-01T00:00:00Z",
  "settings": { ... }
}
```

---

### `GET /api/v1/rooms/:id`

Get room details.

```http
GET /api/v1/rooms/:id
Authorization: Bearer <access_token>
```

Response `200`: room object (same shape as above)

Response `403`: requesting user is not a member of the room

Response `404`: room does not exist

---

### `GET /api/v1/rooms/:id/messages`

Paginated message history. Results are ordered newest-first.

```http
GET /api/v1/rooms/:id/messages?limit=50&before=2026-01-01T00:00:00Z
Authorization: Bearer <access_token>
```

| Query param | Default | Description |
|---|---|---|
| `limit` | `50` | Maximum messages to return (1–100) |
| `before` | — | RFC3339 timestamp cursor; returns messages created before this time |

Response `200`:
```json
[
  {
    "id": "msg-snowflake-id",
    "room_id": "room-snowflake-id",
    "user_id": "user-snowflake-id",
    "content": "Hello world!",
    "content_type": "text",
    "parent_id": null,
    "edited_at": null,
    "deleted_at": null,
    "reactions": {
      "👍": ["user-id-1", "user-id-2"]
    },
    "created_at": "2026-01-01T00:00:00Z"
  }
]
```

To paginate backwards:
```bash
# Get the oldest message's `created_at` from the first response
# Use it as the `before` cursor for the next request
GET /api/v1/rooms/:id/messages?limit=50&before=2025-12-31T23:59:59Z
```

---

### `POST /api/v1/rooms/:id/join`

Join a room.

```http
POST /api/v1/rooms/:id/join
Authorization: Bearer <access_token>
```

Response `200`: `{"status": "joined"}`

Response `404`: room not found (`NOT_FOUND`)

Response `403`: user is banned from the room (`FORBIDDEN`)

---

### `POST /api/v1/rooms/:id/leave`

Leave a room.

```http
POST /api/v1/rooms/:id/leave
Authorization: Bearer <access_token>
```

Response `200`: `{"status": "left"}`

---

## Error response format

All error responses use this shape:

```json
{
  "code": "MACHINE_READABLE_CODE",
  "message": "Human-readable explanation"
}
```

### Status codes

| Status | Meaning |
|---|---|
| `200` | Success |
| `201` | Created |
| `400` | Validation error or bad request body |
| `401` | Missing, expired, or revoked access token |
| `403` | Authenticated but not authorized (wrong room, banned) |
| `404` | Resource not found |
| `409` | Conflict (duplicate username/email) |
| `429` | Rate limit exceeded — check `Retry-After` header |
| `500` | Internal server error |

### Rate limit response

When rate-limited, the response includes the `Retry-After` header:

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 47
Content-Type: application/json

{"code": "RATE_LIMITED", "message": "Too many requests"}
```

`Retry-After` value is the number of seconds until the rate limit window resets.
