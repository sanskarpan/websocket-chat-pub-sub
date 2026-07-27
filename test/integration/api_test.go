package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/websocket-chat/internal/config"
	"github.com/websocket-chat/internal/handlers"
	"github.com/websocket-chat/internal/middleware"
	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/repository"
	"github.com/websocket-chat/internal/service"
	"golang.org/x/crypto/bcrypt"
)

// ─── fake infrastructure ─────────────────────────────────────────────────────

type iUserRepo struct {
	mu    sync.RWMutex
	users map[string]*model.User
}

func newUserRepo() *iUserRepo { return &iUserRepo{users: make(map[string]*model.User)} }

func (r *iUserRepo) Create(_ context.Context, user *model.User) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if user.ID == "" { user.ID = "user-" + user.Username }
	user.CreatedAt = time.Now(); user.UpdatedAt = time.Now()
	r.users[user.ID] = user
	r.users["u:"+user.Username] = user
	r.users["e:"+user.Email] = user
	return nil
}
func (r *iUserRepo) GetByID(_ context.Context, id string) (*model.User, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if u, ok := r.users[id]; ok { return u, nil }
	return nil, errors.New("not found")
}
func (r *iUserRepo) GetByUsername(_ context.Context, username string) (*model.User, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if u, ok := r.users["u:"+username]; ok { return u, nil }
	return nil, errors.New("not found")
}
func (r *iUserRepo) GetByEmail(_ context.Context, email string) (*model.User, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if u, ok := r.users["e:"+email]; ok { return u, nil }
	return nil, errors.New("not found")
}
func (r *iUserRepo) Update(_ context.Context, user *model.User) error {
	r.mu.Lock(); defer r.mu.Unlock(); r.users[user.ID] = user; return nil
}
func (r *iUserRepo) Search(_ context.Context, query string, limit int) ([]*model.User, error) {
	return nil, nil
}

var _ repository.IUserRepository = (*iUserRepo)(nil)

type iRoomRepo struct {
	mu      sync.RWMutex
	rooms   map[string]*model.Room
	members map[string]*model.RoomMember
}

func newRoomRepo() *iRoomRepo {
	return &iRoomRepo{rooms: make(map[string]*model.Room), members: make(map[string]*model.RoomMember)}
}

func rk(a, b string) string { return a + ":" + b }

func (r *iRoomRepo) Create(_ context.Context, room *model.Room) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if room.ID == "" { room.ID = "room-" + room.Name }
	room.CreatedAt = time.Now(); room.UpdatedAt = time.Now(); r.rooms[room.ID] = room; return nil
}
func (r *iRoomRepo) CreateRoomWithOwner(_ context.Context, room *model.Room, owner *model.RoomMember) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if room.ID == "" { room.ID = "room-" + room.Name }
	room.CreatedAt = time.Now(); room.UpdatedAt = time.Now(); r.rooms[room.ID] = room
	owner.RoomID = room.ID; owner.JoinedAt = time.Now(); r.members[rk(owner.RoomID, owner.UserID)] = owner
	return nil
}
func (r *iRoomRepo) GetByID(_ context.Context, id string) (*model.Room, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if room, ok := r.rooms[id]; ok { return room, nil }
	return nil, errors.New("not found")
}
func (r *iRoomRepo) Update(_ context.Context, room *model.Room) error {
	r.mu.Lock(); defer r.mu.Unlock(); r.rooms[room.ID] = room; return nil
}
func (r *iRoomRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	now := time.Now()
	if room, ok := r.rooms[id]; ok { room.ArchivedAt = &now }
	return nil
}
func (r *iRoomRepo) GetUserRooms(_ context.Context, userID string) ([]*model.Room, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	var rooms []*model.Room
	for _, m := range r.members {
		if m.UserID == userID && m.LeftAt == nil {
			if room, ok := r.rooms[m.RoomID]; ok { rooms = append(rooms, room) }
		}
	}
	return rooms, nil
}
func (r *iRoomRepo) AddMember(_ context.Context, member *model.RoomMember) error {
	r.mu.Lock(); defer r.mu.Unlock(); member.JoinedAt = time.Now(); r.members[rk(member.RoomID, member.UserID)] = member; return nil
}
func (r *iRoomRepo) JoinRoomTx(_ context.Context, member *model.RoomMember) error {
	r.mu.Lock(); defer r.mu.Unlock()
	member.JoinedAt = time.Now(); r.members[rk(member.RoomID, member.UserID)] = member
	if room, ok := r.rooms[member.RoomID]; ok { room.MemberCount++; room.UpdatedAt = time.Now() }
	return nil
}
func (r *iRoomRepo) LeaveRoomTx(_ context.Context, roomID, userID string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if m, ok := r.members[rk(roomID, userID)]; ok { now := time.Now(); m.LeftAt = &now }
	if room, ok := r.rooms[roomID]; ok && room.MemberCount > 0 { room.MemberCount--; room.UpdatedAt = time.Now() }
	return nil
}
func (r *iRoomRepo) RemoveMember(_ context.Context, roomID, userID string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if m, ok := r.members[rk(roomID, userID)]; ok { now := time.Now(); m.LeftAt = &now }
	return nil
}
func (r *iRoomRepo) GetMembers(_ context.Context, roomID string) ([]*model.RoomMember, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	var out []*model.RoomMember
	for _, m := range r.members {
		if m.RoomID == roomID && m.LeftAt == nil { out = append(out, m) }
	}
	return out, nil
}
func (r *iRoomRepo) GetMember(_ context.Context, roomID, userID string) (*model.RoomMember, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if m, ok := r.members[rk(roomID, userID)]; ok && m.LeftAt == nil { return m, nil }
	return nil, errors.New("not a member")
}
func (r *iRoomRepo) IsMember(_ context.Context, roomID, userID string) (bool, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if m, ok := r.members[rk(roomID, userID)]; ok && m.LeftAt == nil { return true, nil }
	return false, nil
}
func (r *iRoomRepo) IncrementMemberCount(_ context.Context, roomID string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if room, ok := r.rooms[roomID]; ok { room.MemberCount++ }
	return nil
}
func (r *iRoomRepo) DecrementMemberCount(_ context.Context, roomID string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if room, ok := r.rooms[roomID]; ok && room.MemberCount > 0 { room.MemberCount-- }
	return nil
}
func (r *iRoomRepo) MarkRead(_ context.Context, roomID, userID, msgID string) error { return nil }

var _ repository.IRoomRepository = (*iRoomRepo)(nil)

var iMsgSeq uint64

type iMsgRepo struct {
	mu       sync.RWMutex
	messages map[string]*model.Message
}

func newMsgRepo() *iMsgRepo { return &iMsgRepo{messages: make(map[string]*model.Message)} }

func (r *iMsgRepo) Create(_ context.Context, msg *model.Message) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if msg.ID == "" {
		seq := atomic.AddUint64(&iMsgSeq, 1)
		msg.ID = fmt.Sprintf("msg-%s-%s-%d", msg.RoomID, msg.UserID, seq)
	}
	msg.CreatedAt = time.Now(); r.messages[msg.ID] = msg; return nil
}
func (r *iMsgRepo) GetByID(_ context.Context, id string) (*model.Message, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	if m, ok := r.messages[id]; ok { return m, nil }
	return nil, errors.New("not found")
}
func (r *iMsgRepo) GetByRoom(_ context.Context, roomID string, limit int, before *time.Time) ([]*model.Message, error) {
	r.mu.RLock(); defer r.mu.RUnlock()
	var out []*model.Message
	for _, m := range r.messages {
		if m.RoomID == roomID && m.DeletedAt == nil { out = append(out, m) }
	}
	return out, nil
}
func (r *iMsgRepo) Update(_ context.Context, msg *model.Message) error {
	r.mu.Lock(); defer r.mu.Unlock(); now := time.Now(); msg.EditedAt = &now; r.messages[msg.ID] = msg; return nil
}
func (r *iMsgRepo) UpdateReactions(_ context.Context, msgID string, reactions map[string][]string) error {
	return nil
}
func (r *iMsgRepo) UpdateReactionsTx(_ context.Context, msgID string, fn func(map[string][]string) (map[string][]string, error)) error {
	return nil
}
func (r *iMsgRepo) Delete(_ context.Context, id, deletedBy string) error {
	r.mu.Lock(); defer r.mu.Unlock()
	if msg, ok := r.messages[id]; ok { now := time.Now(); msg.DeletedAt = &now; msg.DeletedBy = &deletedBy }
	return nil
}
func (r *iMsgRepo) GetThread(_ context.Context, parentID string, limit int) ([]*model.Message, error) {
	return nil, nil
}

var _ repository.IMessageRepository = (*iMsgRepo)(nil)

type iInvalidator struct {
	mu   sync.Mutex
	data map[string]bool
}

func newInvalidator() *iInvalidator { return &iInvalidator{data: make(map[string]bool)} }
func (f *iInvalidator) InvalidateToken(_ context.Context, jti string, _ time.Duration) error {
	f.mu.Lock(); defer f.mu.Unlock(); f.data[jti] = true; return nil
}
func (f *iInvalidator) IsTokenInvalidated(_ context.Context, jti string) (bool, error) {
	f.mu.Lock(); defer f.mu.Unlock(); return f.data[jti], nil
}

// ─── router builder ───────────────────────────────────────────────────────────

func buildRouter(t *testing.T) (*gin.Engine, *service.AuthService) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privBytes, _ := x509.MarshalPKCS8PrivateKey(privKey)
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}))
	pubBytes, _ := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	pubPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}))

	cfg := &config.Config{
		App: config.AppConfig{Name: "test", Version: "1.0.0", Environment: "development"},
		Auth: config.AuthConfig{
			JWT: config.JWTConfig{
				Algorithm:       "RS256",
				PrivateKey:      privPEM,
				PublicKey:       pubPEM,
				AccessTokenTTL:  15 * time.Minute,
				RefreshTokenTTL: 7 * 24 * time.Hour,
				Issuer:          "test-issuer",
				Audience:        []string{"test-audience"},
			},
			BCrypt: config.BCryptConfig{Cost: bcrypt.MinCost},
		},
		Server: config.ServerConfig{
			Websocket: config.WebsocketConfig{
				AllowedOrigins: []string{"http://localhost:3000"},
			},
		},
	}

	userRepo := newUserRepo()
	roomRepo := newRoomRepo()
	msgRepo := newMsgRepo()
	inv := newInvalidator()

	authSvc := service.NewAuthService(cfg, userRepo)
	authSvc.SetTokenInvalidator(inv)
	roomSvc := service.NewRoomService(roomRepo, userRepo, nil, nil)
	msgSvc := service.NewMessageService(msgRepo, roomRepo, nil, cfg)

	h := handlers.New(cfg, authSvc, roomSvc, msgSvc, nil)

	r := gin.New()
	r.Use(gin.Recovery(), middleware.CORSMiddleware(cfg.Server.Websocket.AllowedOrigins))

	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })

	api := r.Group("/api/v1")
	auth := api.Group("/auth")
	auth.POST("/register", h.Register)
	auth.POST("/login", h.Login)
	auth.POST("/refresh", h.Refresh)
	auth.POST("/logout", handlers.AuthMiddleware(authSvc), h.Logout)

	rooms := api.Group("/rooms")
	rooms.Use(handlers.AuthMiddleware(authSvc))
	rooms.GET("", h.ListRooms)
	rooms.POST("", h.CreateRoom)
	rooms.GET("/:id", h.GetRoom)
	rooms.GET("/:id/messages", h.GetRoomMessages)
	rooms.POST("/:id/join", h.JoinRoom)
	rooms.POST("/:id/leave", h.LeaveRoom)

	return r, authSvc
}

func doRequest(t *testing.T, r *gin.Engine, method, path string, body interface{}, token string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ─── tests ────────────────────────────────────────────────────────────────────

func TestIntegration_HealthEndpoints(t *testing.T) {
	r, _ := buildRouter(t)
	for _, path := range []string{"/healthz", "/readyz"} {
		w := doRequest(t, r, "GET", path, nil, "")
		assert.Equal(t, http.StatusOK, w.Code, path)
	}
}

func TestIntegration_CORSHeadersOnAllowedOrigin(t *testing.T) {
	r, _ := buildRouter(t)
	req := httptest.NewRequest("OPTIONS", "/api/v1/auth/login", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// CORS middleware sets allow-origin for listed origins
	assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestIntegration_RegisterAndLogin(t *testing.T) {
	r, _ := buildRouter(t)

	w := doRequest(t, r, "POST", "/api/v1/auth/register", map[string]string{
		"username": "alice", "email": "alice@example.com", "password": "password123",
	}, "")
	require.Equal(t, http.StatusCreated, w.Code)

	var reg map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &reg))
	assert.Equal(t, "alice", reg["username"])
	assert.NotEmpty(t, reg["id"])

	w = doRequest(t, r, "POST", "/api/v1/auth/login", map[string]string{
		"email": "alice@example.com", "password": "password123",
	}, "")
	require.Equal(t, http.StatusOK, w.Code)

	var login map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &login))
	assert.NotEmpty(t, login["access_token"])
	assert.NotEmpty(t, login["refresh_token"])
}

func TestIntegration_AuthMiddlewareBlocksUnauthenticated(t *testing.T) {
	r, _ := buildRouter(t)
	for _, path := range []string{"/api/v1/rooms", "/api/v1/rooms/x/messages"} {
		w := doRequest(t, r, "GET", path, nil, "")
		assert.Equal(t, http.StatusUnauthorized, w.Code, "unauthenticated %s must be blocked", path)
	}
}

func TestIntegration_FullRoomFlow(t *testing.T) {
	r, _ := buildRouter(t)

	// register + login
	doRequest(t, r, "POST", "/api/v1/auth/register", map[string]string{
		"username": "flowuser", "email": "flow@example.com", "password": "password123",
	}, "")
	w := doRequest(t, r, "POST", "/api/v1/auth/login", map[string]string{
		"email": "flow@example.com", "password": "password123",
	}, "")
	var login map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &login))
	token := login["access_token"].(string)

	// create room
	w = doRequest(t, r, "POST", "/api/v1/rooms", map[string]string{
		"name": "general", "type": "channel", "description": "Test room",
	}, token)
	require.Equal(t, http.StatusCreated, w.Code)
	var room map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &room))
	roomID := room["id"].(string)

	// list rooms — room should appear (creator is automatically a member)
	w = doRequest(t, r, "GET", "/api/v1/rooms", nil, token)
	require.Equal(t, http.StatusOK, w.Code)

	// get room
	w = doRequest(t, r, "GET", "/api/v1/rooms/"+roomID, nil, token)
	require.Equal(t, http.StatusOK, w.Code)

	// get messages (empty)
	w = doRequest(t, r, "GET", "/api/v1/rooms/"+roomID+"/messages", nil, token)
	require.Equal(t, http.StatusOK, w.Code)

	// leave room
	w = doRequest(t, r, "POST", "/api/v1/rooms/"+roomID+"/leave", nil, token)
	require.Equal(t, http.StatusOK, w.Code)

	// re-join room
	w = doRequest(t, r, "POST", "/api/v1/rooms/"+roomID+"/join", nil, token)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestIntegration_Logout_InvalidatesToken(t *testing.T) {
	r, _ := buildRouter(t)

	doRequest(t, r, "POST", "/api/v1/auth/register", map[string]string{
		"username": "logouttest", "email": "logouttest@example.com", "password": "password123",
	}, "")
	w := doRequest(t, r, "POST", "/api/v1/auth/login", map[string]string{
		"email": "logouttest@example.com", "password": "password123",
	}, "")
	var login map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &login))
	accessToken := login["access_token"].(string)
	refreshToken := login["refresh_token"].(string)

	// access works before logout
	w = doRequest(t, r, "GET", "/api/v1/rooms", nil, accessToken)
	require.Equal(t, http.StatusOK, w.Code)

	// logout
	w = doRequest(t, r, "POST", "/api/v1/auth/logout",
		map[string]string{"refresh_token": refreshToken}, accessToken)
	require.Equal(t, http.StatusOK, w.Code)

	// access token now rejected
	w = doRequest(t, r, "GET", "/api/v1/rooms", nil, accessToken)
	assert.Equal(t, http.StatusUnauthorized, w.Code, "access token must be rejected after logout")

	// refresh token now rejected
	w = doRequest(t, r, "POST", "/api/v1/auth/refresh",
		map[string]string{"refresh_token": refreshToken}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code, "refresh token must be rejected after logout")
}

func TestIntegration_TokenRefreshRotation(t *testing.T) {
	r, _ := buildRouter(t)

	doRequest(t, r, "POST", "/api/v1/auth/register", map[string]string{
		"username": "rotateuser", "email": "rotate@example.com", "password": "password123",
	}, "")
	w := doRequest(t, r, "POST", "/api/v1/auth/login", map[string]string{
		"email": "rotate@example.com", "password": "password123",
	}, "")
	var login map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &login))
	refreshToken := login["refresh_token"].(string)

	// rotate the refresh token
	w = doRequest(t, r, "POST", "/api/v1/auth/refresh",
		map[string]string{"refresh_token": refreshToken}, "")
	require.Equal(t, http.StatusOK, w.Code)
	var refresh map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refresh))
	assert.NotEmpty(t, refresh["access_token"])

	// old refresh token is now invalidated
	w = doRequest(t, r, "POST", "/api/v1/auth/refresh",
		map[string]string{"refresh_token": refreshToken}, "")
	assert.Equal(t, http.StatusUnauthorized, w.Code, "old refresh token must be rejected after rotation")
}

func TestIntegration_JoinRoomNotFound_Returns404(t *testing.T) {
	r, _ := buildRouter(t)

	doRequest(t, r, "POST", "/api/v1/auth/register", map[string]string{
		"username": "joiner404", "email": "j404@example.com", "password": "password123",
	}, "")
	w := doRequest(t, r, "POST", "/api/v1/auth/login", map[string]string{
		"email": "j404@example.com", "password": "password123",
	}, "")
	var login map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &login))
	token := login["access_token"].(string)

	w = doRequest(t, r, "POST", "/api/v1/rooms/nonexistent/join", nil, token)
	assert.Equal(t, http.StatusNotFound, w.Code, "joining non-existent room must return 404")
}

func TestIntegration_Register_Validation(t *testing.T) {
	r, _ := buildRouter(t)

	tests := []struct {
		name    string
		payload map[string]string
		code    int
	}{
		{"missing username", map[string]string{"email": "x@example.com", "password": "password123"}, http.StatusBadRequest},
		{"invalid email", map[string]string{"username": "user", "email": "not-email", "password": "password123"}, http.StatusBadRequest},
		{"short password", map[string]string{"username": "user", "email": "u@example.com", "password": "short"}, http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := doRequest(t, r, "POST", "/api/v1/auth/register", tc.payload, "")
			assert.Equal(t, tc.code, w.Code)
		})
	}
}
