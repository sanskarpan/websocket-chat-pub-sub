package handlers_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/websocket-chat/internal/config"
	"github.com/websocket-chat/internal/handlers"
	"github.com/websocket-chat/internal/service"
	"golang.org/x/crypto/bcrypt"
)

// ─── test helpers ─────────────────────────────────────────────────────────────

func testRSAKeyPair(t *testing.T) (priv, pub string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privBytes, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	priv = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}))
	pubBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	pub = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}))
	return
}

func testCfg(t *testing.T) *config.Config {
	t.Helper()
	priv, pub := testRSAKeyPair(t)
	return &config.Config{
		App: config.AppConfig{Name: "test", Version: "1.0.0", Environment: "development"},
		Auth: config.AuthConfig{
			JWT: config.JWTConfig{
				Algorithm:       "RS256",
				PrivateKey:      priv,
				PublicKey:       pub,
				AccessTokenTTL:  15 * time.Minute,
				RefreshTokenTTL: 7 * 24 * time.Hour,
				Issuer:          "test-issuer",
				Audience:        []string{"test-audience"},
			},
			BCrypt: config.BCryptConfig{Cost: bcrypt.MinCost},
		},
	}
}

type testRouter struct {
	engine   *gin.Engine
	authSvc  *service.AuthService
	inv      *fakeInvalidator
	userRepo *fakeUserRepo
	roomRepo *fakeRoomRepo
}

func newTestRouter(t *testing.T) *testRouter {
	t.Helper()
	gin.SetMode(gin.TestMode)

	cfg := testCfg(t)
	userRepo := newFakeUserRepo()
	roomRepo := newFakeRoomRepo()
	msgRepo := newFakeMessageRepo()
	inv := newFakeInvalidator()

	authSvc := service.NewAuthService(cfg, userRepo)
	authSvc.SetTokenInvalidator(inv)
	roomSvc := service.NewRoomService(roomRepo, userRepo, nil, nil)
	msgSvc := service.NewMessageService(msgRepo, roomRepo, nil, cfg)

	h := handlers.New(cfg, authSvc, roomSvc, msgSvc, nil)

	r := gin.New()
	r.GET("/health", h.Health)

	auth := r.Group("/auth")
	auth.POST("/register", h.Register)
	auth.POST("/login", h.Login)
	auth.POST("/refresh", h.Refresh)
	auth.POST("/logout", handlers.AuthMiddleware(authSvc), h.Logout)

	rooms := r.Group("/rooms")
	rooms.Use(handlers.AuthMiddleware(authSvc))
	rooms.GET("", h.ListRooms)
	rooms.POST("", h.CreateRoom)
	rooms.GET("/:id", h.GetRoom)
	rooms.GET("/:id/messages", h.GetRoomMessages)
	rooms.POST("/:id/join", h.JoinRoom)
	rooms.POST("/:id/leave", h.LeaveRoom)

	return &testRouter{engine: r, authSvc: authSvc, inv: inv, userRepo: userRepo, roomRepo: roomRepo}
}

func (tr *testRouter) do(method, path string, body interface{}, token string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	tr.engine.ServeHTTP(w, req)
	return w
}

func (tr *testRouter) registerAndLogin(t *testing.T, username, email, password string) string {
	t.Helper()
	w := tr.do("POST", "/auth/register", map[string]string{
		"username": username, "email": email, "password": password,
	}, "")
	require.Equal(t, http.StatusCreated, w.Code)
	w = tr.do("POST", "/auth/login", map[string]string{"email": email, "password": password}, "")
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp["access_token"].(string)
}

// ─── health ───────────────────────────────────────────────────────────────────

func TestHandler_Health(t *testing.T) {
	tr := newTestRouter(t)
	w := tr.do("GET", "/health", nil, "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "WebSocket Chat API")
}

// ─── register ─────────────────────────────────────────────────────────────────

func TestHandler_Register_Success(t *testing.T) {
	tr := newTestRouter(t)
	w := tr.do("POST", "/auth/register", map[string]string{
		"username": "alice", "email": "alice@example.com", "password": "password123",
	}, "")
	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "alice", resp["username"])
	assert.Equal(t, "alice@example.com", resp["email"])
	assert.NotEmpty(t, resp["id"])
}

func TestHandler_Register_InvalidJSON(t *testing.T) {
	tr := newTestRouter(t)
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	tr.engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Register_InvalidEmail(t *testing.T) {
	tr := newTestRouter(t)
	w := tr.do("POST", "/auth/register", map[string]string{
		"username": "bob", "email": "not-an-email", "password": "password123",
	}, "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Register_DuplicateUser(t *testing.T) {
	tr := newTestRouter(t)
	body := map[string]string{"username": "dup", "email": "dup@example.com", "password": "password123"}
	w := tr.do("POST", "/auth/register", body, "")
	require.Equal(t, http.StatusCreated, w.Code)
	w = tr.do("POST", "/auth/register", body, "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ─── login ────────────────────────────────────────────────────────────────────

func TestHandler_Login_Success(t *testing.T) {
	tr := newTestRouter(t)
	tr.do("POST", "/auth/register", map[string]string{
		"username": "loginuser", "email": "login@example.com", "password": "password123",
	}, "")

	w := tr.do("POST", "/auth/login", map[string]string{
		"email": "login@example.com", "password": "password123",
	}, "")
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["access_token"])
	assert.NotEmpty(t, resp["refresh_token"])
	assert.Equal(t, "Bearer", resp["token_type"])
}

func TestHandler_Login_WrongPassword(t *testing.T) {
	tr := newTestRouter(t)
	tr.do("POST", "/auth/register", map[string]string{
		"username": "pwuser", "email": "pw@example.com", "password": "rightpass",
	}, "")
	w := tr.do("POST", "/auth/login", map[string]string{
		"email": "pw@example.com", "password": "wrongpass",
	}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Login_UnknownEmail(t *testing.T) {
	tr := newTestRouter(t)
	w := tr.do("POST", "/auth/login", map[string]string{
		"email": "ghost@example.com", "password": "anything",
	}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// ─── refresh ──────────────────────────────────────────────────────────────────

func TestHandler_Refresh_Success(t *testing.T) {
	tr := newTestRouter(t)
	tr.do("POST", "/auth/register", map[string]string{
		"username": "rfuser", "email": "rf@example.com", "password": "password123",
	}, "")
	w := tr.do("POST", "/auth/login", map[string]string{
		"email": "rf@example.com", "password": "password123",
	}, "")
	var login map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &login))

	w = tr.do("POST", "/auth/refresh", map[string]string{
		"refresh_token": login["refresh_token"].(string),
	}, "")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp["access_token"])
}

func TestHandler_Refresh_InvalidToken(t *testing.T) {
	tr := newTestRouter(t)
	w := tr.do("POST", "/auth/refresh", map[string]string{"refresh_token": "bad.token.here"}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Refresh_MissingBody(t *testing.T) {
	tr := newTestRouter(t)
	w := tr.do("POST", "/auth/refresh", map[string]string{}, "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ─── logout ───────────────────────────────────────────────────────────────────

func TestHandler_Logout_Success(t *testing.T) {
	tr := newTestRouter(t)
	token := tr.registerAndLogin(t, "logoutuser", "lo@example.com", "password123")

	w := tr.do("POST", "/auth/logout", nil, token)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "logged_out")
}

func TestHandler_Logout_BlacklistsAccessToken(t *testing.T) {
	tr := newTestRouter(t)
	token := tr.registerAndLogin(t, "bluser", "bl@example.com", "password123")

	// token works before logout
	w := tr.do("GET", "/rooms", nil, token)
	assert.Equal(t, http.StatusOK, w.Code)

	// logout
	w = tr.do("POST", "/auth/logout", nil, token)
	require.Equal(t, http.StatusOK, w.Code)

	// token is now rejected
	w = tr.do("GET", "/rooms", nil, token)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Logout_BlacklistsRefreshToken(t *testing.T) {
	tr := newTestRouter(t)
	tr.do("POST", "/auth/register", map[string]string{
		"username": "rflogout", "email": "rfl@example.com", "password": "password123",
	}, "")
	w := tr.do("POST", "/auth/login", map[string]string{
		"email": "rfl@example.com", "password": "password123",
	}, "")
	var login map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &login))
	accessToken := login["access_token"].(string)
	refreshToken := login["refresh_token"].(string)

	// logout with both tokens
	w = tr.do("POST", "/auth/logout", map[string]string{"refresh_token": refreshToken}, accessToken)
	require.Equal(t, http.StatusOK, w.Code)

	// refresh token is now rejected
	w = tr.do("POST", "/auth/refresh", map[string]string{"refresh_token": refreshToken}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Logout_RequiresAuth(t *testing.T) {
	tr := newTestRouter(t)
	w := tr.do("POST", "/auth/logout", nil, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// ─── list rooms ───────────────────────────────────────────────────────────────

func TestHandler_ListRooms_Unauthenticated(t *testing.T) {
	tr := newTestRouter(t)
	w := tr.do("GET", "/rooms", nil, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_ListRooms_EmptyForNewUser(t *testing.T) {
	tr := newTestRouter(t)
	token := tr.registerAndLogin(t, "newuser", "new@example.com", "password123")
	w := tr.do("GET", "/rooms", nil, token)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ─── create room ──────────────────────────────────────────────────────────────

func TestHandler_CreateRoom_Success(t *testing.T) {
	tr := newTestRouter(t)
	token := tr.registerAndLogin(t, "creator", "creator@example.com", "password123")

	w := tr.do("POST", "/rooms", map[string]string{
		"name": "general", "type": "channel", "description": "General chat",
	}, token)
	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "general", resp["name"])
	assert.NotEmpty(t, resp["id"])
}

func TestHandler_CreateRoom_MissingName(t *testing.T) {
	tr := newTestRouter(t)
	token := tr.registerAndLogin(t, "creator2", "c2@example.com", "password123")

	w := tr.do("POST", "/rooms", map[string]string{"type": "channel"}, token)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ─── get room ─────────────────────────────────────────────────────────────────

func TestHandler_GetRoom_NotFound(t *testing.T) {
	tr := newTestRouter(t)
	token := tr.registerAndLogin(t, "getter", "get@example.com", "password123")
	w := tr.do("GET", "/rooms/nonexistent-room-id", nil, token)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_GetRoom_NotMember(t *testing.T) {
	tr := newTestRouter(t)
	// owner creates a room
	ownerToken := tr.registerAndLogin(t, "owner", "owner@example.com", "password123")
	w := tr.do("POST", "/rooms", map[string]string{"name": "private", "type": "channel"}, ownerToken)
	require.Equal(t, http.StatusCreated, w.Code)
	var roomResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &roomResp))
	roomID := roomResp["id"].(string)

	// stranger cannot see the room
	strangerToken := tr.registerAndLogin(t, "stranger", "stranger@example.com", "password123")
	w = tr.do("GET", "/rooms/"+roomID, nil, strangerToken)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandler_GetRoom_Success(t *testing.T) {
	tr := newTestRouter(t)
	token := tr.registerAndLogin(t, "roomowner", "ro@example.com", "password123")
	w := tr.do("POST", "/rooms", map[string]string{"name": "myroom", "type": "channel"}, token)
	require.Equal(t, http.StatusCreated, w.Code)
	var roomResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &roomResp))
	roomID := roomResp["id"].(string)

	w = tr.do("GET", "/rooms/"+roomID, nil, token)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "myroom", resp["name"])
}

// ─── get room messages ────────────────────────────────────────────────────────

func TestHandler_GetRoomMessages_NotMember(t *testing.T) {
	tr := newTestRouter(t)
	ownerToken := tr.registerAndLogin(t, "msgowner", "mowner@example.com", "password123")
	w := tr.do("POST", "/rooms", map[string]string{"name": "msgroom", "type": "channel"}, ownerToken)
	require.Equal(t, http.StatusCreated, w.Code)
	var roomResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &roomResp))
	roomID := roomResp["id"].(string)

	strangerToken := tr.registerAndLogin(t, "msgstranger", "msgstr@example.com", "password123")
	w = tr.do("GET", "/rooms/"+roomID+"/messages", nil, strangerToken)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandler_GetRoomMessages_Success(t *testing.T) {
	tr := newTestRouter(t)
	token := tr.registerAndLogin(t, "msgreader", "mreader@example.com", "password123")
	w := tr.do("POST", "/rooms", map[string]string{"name": "readroom", "type": "channel"}, token)
	require.Equal(t, http.StatusCreated, w.Code)
	var roomResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &roomResp))
	roomID := roomResp["id"].(string)

	w = tr.do("GET", "/rooms/"+roomID+"/messages", nil, token)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ─── join room ────────────────────────────────────────────────────────────────

func TestHandler_JoinRoom_RoomNotFound_Returns404(t *testing.T) {
	tr := newTestRouter(t)
	token := tr.registerAndLogin(t, "joiner", "joiner@example.com", "password123")
	w := tr.do("POST", "/rooms/does-not-exist/join", nil, token)
	assert.Equal(t, http.StatusNotFound, w.Code, "joining non-existent room must return 404, not 500")
}

func TestHandler_JoinRoom_BannedUser_Returns403(t *testing.T) {
	tr := newTestRouter(t)
	ownerToken := tr.registerAndLogin(t, "bowner", "bowner@example.com", "password123")
	w := tr.do("POST", "/rooms", map[string]string{"name": "banroom", "type": "channel"}, ownerToken)
	require.Equal(t, http.StatusCreated, w.Code)
	var roomResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &roomResp))
	roomID := roomResp["id"].(string)

	bannedToken := tr.registerAndLogin(t, "banned", "banned@example.com", "password123")

	// First join succeeds
	w = tr.do("POST", "/rooms/"+roomID+"/join", nil, bannedToken)
	require.Equal(t, http.StatusOK, w.Code)

	// Ban the user — BannedAt is set; LeftAt stays nil (still "in" the room).
	// JoinRoom checks BannedAt before LeftAt, so the next join attempt returns ErrUserBanned → 403.
	tr.roomRepo.BanMember(roomID, "user-banned")

	w = tr.do("POST", "/rooms/"+roomID+"/join", nil, bannedToken)
	assert.Equal(t, http.StatusForbidden, w.Code, "banned user must receive 403, not 500")
}

func TestHandler_JoinRoom_Success(t *testing.T) {
	tr := newTestRouter(t)
	ownerToken := tr.registerAndLogin(t, "jowner", "jowner@example.com", "password123")
	w := tr.do("POST", "/rooms", map[string]string{"name": "joinroom", "type": "channel"}, ownerToken)
	require.Equal(t, http.StatusCreated, w.Code)
	var roomResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &roomResp))
	roomID := roomResp["id"].(string)

	joinerToken := tr.registerAndLogin(t, "jjoiner", "jjoiner@example.com", "password123")
	w = tr.do("POST", "/rooms/"+roomID+"/join", nil, joinerToken)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "joined")
}

// ─── leave room ───────────────────────────────────────────────────────────────

func TestHandler_LeaveRoom_Success(t *testing.T) {
	tr := newTestRouter(t)
	token := tr.registerAndLogin(t, "leaver", "leaver@example.com", "password123")
	w := tr.do("POST", "/rooms", map[string]string{"name": "leaveroom", "type": "channel"}, token)
	require.Equal(t, http.StatusCreated, w.Code)
	var roomResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &roomResp))
	roomID := roomResp["id"].(string)

	w = tr.do("POST", "/rooms/"+roomID+"/leave", nil, token)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "left")
}

func TestHandler_LeaveRoom_NotMember(t *testing.T) {
	tr := newTestRouter(t)
	ownerToken := tr.registerAndLogin(t, "lowner", "lowner@example.com", "password123")
	w := tr.do("POST", "/rooms", map[string]string{"name": "lroom", "type": "channel"}, ownerToken)
	require.Equal(t, http.StatusCreated, w.Code)
	var roomResp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &roomResp))
	roomID := roomResp["id"].(string)

	strangerToken := tr.registerAndLogin(t, "lstranger", "lstranger@example.com", "password123")
	w = tr.do("POST", "/rooms/"+roomID+"/leave", nil, strangerToken)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ─── auth middleware ──────────────────────────────────────────────────────────

func TestHandler_AuthMiddleware_NoHeader(t *testing.T) {
	tr := newTestRouter(t)
	w := tr.do("GET", "/rooms", nil, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_AuthMiddleware_MalformedToken(t *testing.T) {
	tr := newTestRouter(t)
	w := tr.do("GET", "/rooms", nil, "not.a.jwt")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_AuthMiddleware_BearerCaseInsensitive(t *testing.T) {
	tr := newTestRouter(t)
	token := tr.registerAndLogin(t, "caseuser", "case@example.com", "password123")

	req := httptest.NewRequest("GET", "/rooms", nil)
	req.Header.Set("Authorization", "bearer "+token) // lowercase
	w := httptest.NewRecorder()
	tr.engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
