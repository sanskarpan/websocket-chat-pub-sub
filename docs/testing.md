# Testing

Three test layers: unit (fake repos, no I/O), integration (real router + fake repos, no external services), and end-to-end (live infra required).

---

## Run all tests

```bash
# Unit + integration tests — no external services needed
go test -race ./...

# With verbose output
go test -race -v ./...

# With timeout (useful in CI)
go test -race ./... -timeout 120s
```

All 11 packages pass with zero data races.

---

## Test layers

### Unit tests (`internal/...`)

Unit tests use fake in-memory implementations of every repository and infrastructure interface. No PostgreSQL, no Redis, no network.

**Pattern**: each package that needs fakes has a `fakes_test.go` file in `package <name>_test`. Fakes are minimal — they store state in maps and implement the repository interfaces exactly.

Key design choices:

- **Atomic counters for fake IDs**: `var msgSeq uint64` with `atomic.AddUint64` ensures every fake message gets a unique ID, even when two messages come from the same user in the same room. Without this, dedup tests fail silently.
- **Exported sentinel errors for testable matching**: `service.ErrRoomNotFound` and `service.ErrUserBanned` are exported package-level errors so tests can use `errors.Is` instead of string matching.
- **`fakeTokenInvalidator`**: implements `service.TokenInvalidator` with a `sync.Mutex`-protected map. Used to test blacklisting without Redis.

Packages with unit tests:

| Package | Coverage highlights |
|---|---|
| `internal/service` | Auth blacklisting, logout, token rotation, dedup, non-member errors |
| `internal/handlers` | All 30+ HTTP endpoints, AuthMiddleware edge cases, 404/403 for domain errors |
| `internal/server` | Hub CloseAll idempotency, multiple independent instances, concurrent shutdown |

Run unit tests only:

```bash
go test -race ./internal/...
```

### Auth service tests

```bash
go test -race -v ./internal/service/ -run TestAuthService
```

Includes:

- `TestAuthService_ValidateToken_BlacklistedJTI` — a blacklisted JTI is rejected even if the token is otherwise valid
- `TestAuthService_Logout_BlacklistsBothTokens` — both access and refresh token JTIs are blacklisted after `Logout`
- `TestAuthService_RefreshToken_OldTokenRejectedAfterRotation` — after refresh, the old refresh token's JTI is immediately invalid

### Handler tests

```bash
go test -race -v ./internal/handlers/
```

Includes:

- `TestHandler_Logout_BlacklistsAccessToken` — logout, then verify the used access token returns 401
- `TestHandler_JoinRoom_RoomNotFound_Returns404` — 404, not 500
- `TestHandler_JoinRoom_BannedUser_Returns403` — 403, not 500
- `TestHandler_AuthMiddleware_BearerCaseInsensitive` — `"bearer token"` (lowercase) works

### Integration tests (`test/integration`)

Integration tests build a real `gin.Engine` wired to real services — but backed by fake in-memory repositories. They test the full HTTP request path including middleware, routing, and JSON serialization.

```bash
go test -race -v ./test/integration/
```

Tests:

- `TestIntegration_HealthEndpoints`
- `TestIntegration_CORSHeadersOnAllowedOrigin`
- `TestIntegration_RegisterAndLogin`
- `TestIntegration_AuthMiddlewareBlocksUnauthenticated`
- `TestIntegration_FullRoomFlow`
- `TestIntegration_Logout_InvalidatesToken`
- `TestIntegration_TokenRefreshRotation`
- `TestIntegration_JoinRoomNotFound_Returns404`
- `TestIntegration_Register_Validation`

---

## End-to-end tests (`test/e2e`)

E2E tests require live infrastructure: PostgreSQL, Redis, and the server running. They are gated behind a build tag so they never run in a `go test ./...` invocation without opting in.

```bash
# 1. Start infrastructure
docker compose up -d

# 2. Generate JWT keys
bash scripts/generate_keys.sh

# 3. Build and start the server
go build -o /tmp/ws-chat-server ./cmd/server && /tmp/ws-chat-server &
sleep 2

# 4. Run E2E tests
go test -race -v -tags=e2e ./test/e2e/...

# 5. Cleanup
kill %1
docker compose down
```

E2E tests include:

- `TestE2E_HealthCheck`
- `TestE2E_RegisterAndLogin`
- `TestE2E_TokenRefresh`
- `TestE2E_Logout` (5 sub-tests: access works before logout, logout succeeds, access rejected after logout, refresh rejected after logout, logout without auth rejected)
- `TestE2E_RoomFlow`
- `TestE2E_WebSocketConnect` (upgrade + `connection` message)
- `TestE2E_WebSocketSendMessage`

---

## Race detector

All tests run with `-race`. The Go race detector instruments memory accesses and reports any unsynchronized concurrent reads/writes at runtime.

The Hub's channel-based design (never sharing maps across goroutines) and the use of `sync.Once` for `CloseAll` mean there are zero reported races across all test runs.

```bash
# Confirm zero races
go test -race ./... 2>&1 | grep -E "DATA RACE|ok|FAIL"
```

---

## CI pipeline

The GitHub Actions workflow (`.github/workflows/ci.yml`) runs on every push and pull request:

```
build        → go build ./...
vet          → go vet ./...
lint         → golangci-lint run
test         → go test -race ./... -timeout 120s
govulncheck  → govulncheck ./...
```

E2E tests run in a separate CI job that spins up Docker services:

```yaml
services:
  postgres:
    image: postgres:16
  redis:
    image: redis:7-alpine
```

---

## Writing new tests

### Fake pattern

```go
// In your _test.go file:
type fakeUserRepo struct {
    mu    sync.Mutex
    users map[string]*model.User
}

func (f *fakeUserRepo) GetByID(ctx context.Context, id string) (*model.User, error) {
    f.mu.Lock()
    defer f.mu.Unlock()
    u, ok := f.users[id]
    if !ok {
        return nil, repository.ErrNotFound
    }
    return u, nil
}
// ... implement all interface methods
```

### Unique ID generation in fakes

```go
var testMsgSeq uint64

func (f *fakeMsgRepo) Create(ctx context.Context, msg *model.Message) error {
    seq := atomic.AddUint64(&testMsgSeq, 1)
    msg.ID = fmt.Sprintf("msg-%s-%d", msg.RoomID, seq)
    // store msg ...
    return nil
}
```

Without the atomic counter, two messages in the same room from the same user get the same ID — causing dedup tests to fail silently.

### Testing authenticated endpoints

```go
func TestMyHandler(t *testing.T) {
    tr := newTestRouter(t)
    token := tr.registerAndLogin(t, "user1", "u@example.com", "pass123!")

    w := tr.do("GET", "/api/v1/rooms", "", token)
    assert.Equal(t, http.StatusOK, w.Code)
}
```

`newTestRouter` builds a full Gin engine with real services (AuthService, RoomService, MessageService) backed by fake repos — no mocking of service internals.
