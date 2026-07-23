package handlers

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/websocket-chat/internal/config"
	"github.com/websocket-chat/internal/metrics"
	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/pubsub"
	"github.com/websocket-chat/internal/service"
)

type Handlers struct {
	cfg            *config.Config
	authService    *service.AuthService
	roomService    *service.RoomService
	messageService *service.MessageService
	ps             pubsub.PubSub
}

func New(
	cfg *config.Config,
	authService *service.AuthService,
	roomService *service.RoomService,
	messageService *service.MessageService,
	ps pubsub.PubSub,
) *Handlers {
	return &Handlers{
		cfg:            cfg,
		authService:    authService,
		roomService:    roomService,
		messageService: messageService,
		ps:             ps,
	}
}

func (h *Handlers) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"name":    "WebSocket Chat API",
		"version": "1.0.0",
		"endpoints": map[string]string{
			"healthz": "/healthz",
			"readyz":  "/readyz",
			"ws":      "/ws",
			"auth":    "/api/v1/auth",
			"rooms":   "/api/v1/rooms",
		},
	})
}

func (h *Handlers) Register(c *gin.Context) {
	var input service.RegisterInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "Invalid registration request payload"})
		return
	}

	user, err := h.authService.Register(c.Request.Context(), input)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "REGISTRATION_FAILED", "message": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":           user.ID,
		"username":     user.Username,
		"email":        user.Email,
		"display_name": user.DisplayName,
	})
}

func (h *Handlers) Login(c *gin.Context) {
	var input service.LoginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "Invalid login request payload"})
		return
	}

	tokens, err := h.authService.Login(c.Request.Context(), input)
	if err != nil {
		metrics.AuthAttemptsTotal.WithLabelValues("failure").Inc()
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "Invalid credentials"})
		return
	}

	metrics.AuthAttemptsTotal.WithLabelValues("success").Inc()
	c.JSON(http.StatusOK, gin.H{
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"token_type":    "Bearer",
		"expires_in":    int(h.cfg.Auth.JWT.AccessTokenTTL.Seconds()),
	})
}

func (h *Handlers) Refresh(c *gin.Context) {
	var input struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "Invalid refresh payload"})
		return
	}

	tokens, err := h.authService.RefreshToken(c.Request.Context(), input.RefreshToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "Invalid refresh token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
		"token_type":    "Bearer",
	})
}

func AuthMiddleware(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token == "" {
			metrics.AuthAttemptsTotal.WithLabelValues("no_token").Inc()
			c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "Authorization header required"})
			c.Abort()
			return
		}

		if len(token) > 7 && (strings.EqualFold(token[:7], "Bearer ")) {
			token = token[7:]
		}

		user, err := authService.ValidateToken(c.Request.Context(), token)
		if err != nil {
			metrics.AuthAttemptsTotal.WithLabelValues("invalid_token").Inc()
			c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "Invalid or expired token"})
			c.Abort()
			return
		}

		c.Set("user_id", user.ID)
		c.Set("user", user)
		c.Next()
	}
}

func userID(c *gin.Context) (string, bool) {
	val, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "User not authenticated"})
		return "", false
	}
	userID, ok := val.(string)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "Invalid context state"})
		return "", false
	}
	return userID, true
}

func (h *Handlers) ListRooms(c *gin.Context) {
	uid, ok := userID(c)
	if !ok {
		return
	}
	rooms, err := h.roomService.GetUserRooms(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "Failed to retrieve user rooms"})
		return
	}
	c.JSON(http.StatusOK, rooms)
}

func (h *Handlers) CreateRoom(c *gin.Context) {
	var input struct {
		Name        string `json:"name" binding:"required"`
		Type        string `json:"type" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "Invalid room payload"})
		return
	}

	uid, ok := userID(c)
	if !ok {
		return
	}

	room, err := h.roomService.Create(c.Request.Context(), service.CreateRoomInput{
		Name:        input.Name,
		Type:        model.RoomType(input.Type),
		Description: input.Description,
		CreatedBy:   uid,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_ROOM_FAILED", "message": "Failed to create room"})
		return
	}

	c.JSON(http.StatusCreated, room)
}

func (h *Handlers) GetRoom(c *gin.Context) {
	roomID := c.Param("id")
	uid, ok := userID(c)
	if !ok {
		return
	}

	room, err := h.roomService.GetByID(c.Request.Context(), roomID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Room not found"})
		return
	}

	isMember, err := h.roomService.IsMember(c.Request.Context(), roomID, uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "Failed to check membership"})
		return
	}
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "Not a member of this room"})
		return
	}

	c.JSON(http.StatusOK, room)
}

func (h *Handlers) GetRoomMessages(c *gin.Context) {
	roomID := c.Param("id")
	uid, ok := userID(c)
	if !ok {
		return
	}

	isMember, err := h.roomService.IsMember(c.Request.Context(), roomID, uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "Failed to check membership"})
		return
	}
	if !isMember {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "Not a member of this room"})
		return
	}

	limit := 50
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	var before *time.Time
	if b := c.Query("before"); b != "" {
		if t, err := time.Parse(time.RFC3339, b); err == nil {
			before = &t
		}
	}

	messages, err := h.messageService.GetRoomMessages(c.Request.Context(), roomID, limit, before)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "Failed to retrieve room messages"})
		return
	}
	c.JSON(http.StatusOK, messages)
}

func (h *Handlers) JoinRoom(c *gin.Context) {
	roomID := c.Param("id")
	uid, ok := userID(c)
	if !ok {
		return
	}

	if err := h.roomService.JoinRoom(c.Request.Context(), roomID, uid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "JOIN_FAILED", "message": "Failed to join room"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "joined"})
}

func (h *Handlers) LeaveRoom(c *gin.Context) {
	roomID := c.Param("id")
	uid, ok := userID(c)
	if !ok {
		return
	}

	if err := h.roomService.LeaveRoom(c.Request.Context(), roomID, uid); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "LEAVE_FAILED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "left"})
}