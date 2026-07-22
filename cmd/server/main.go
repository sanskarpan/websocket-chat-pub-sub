package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/websocket-chat/internal/cache"
	"github.com/websocket-chat/internal/config"
	"github.com/websocket-chat/internal/health"
	"github.com/websocket-chat/internal/logging"
	"github.com/websocket-chat/internal/metrics"
	"github.com/websocket-chat/internal/middleware"
	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/pubsub"
	"github.com/websocket-chat/internal/repository"
	"github.com/websocket-chat/internal/server"
	"github.com/websocket-chat/internal/service"
	"github.com/websocket-chat/internal/tracing"
)

func main() {
	cfg := config.Load()
	logger := logging.NewLogger(cfg)

	if err := cfg.Validate(); err != nil {
		logger.Fatal().Err(err).Msg("Invalid configuration")
	}

	if cfg.Observability.Tracing.Enabled {
		tracer, err := tracing.InitTracer(cfg)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to initialize tracer")
		} else {
			defer tracer.Shutdown(context.Background())
		}
	}

	db, err := repository.NewPostgresDB(cfg)
	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer db.Close()

	redisCfg := &pubsub.Config{
		Addrs:    cfg.Redis.Addrs,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
		PoolSize: cfg.Redis.PoolSize,
	}
	redisClient := pubsub.NewRedisClient(redisCfg)
	defer redisClient.Close()

	redisCache := cache.NewRedisCache(redisClient)
	ps := pubsub.NewRedisPubSub(redisClient)

	userRepo := repository.NewUserRepository(db)
	roomRepo := repository.NewRoomRepository(db)
	messageRepo := repository.NewMessageRepository(db)

	authService := service.NewAuthService(cfg, userRepo)
	userService := service.NewUserService(userRepo, redisCache)
	roomService := service.NewRoomService(roomRepo, userRepo, redisCache)
	messageService := service.NewMessageService(messageRepo, roomRepo, ps, cfg)
	presenceService := service.NewPresenceService(ps)

	metricsServer := metrics.NewMetricsServer(cfg.Observability.Metrics.Port)
	go func() {
		if err := metricsServer.Start(); err != nil && err != http.ErrServerClosed {
			logger.Error().Err(err).Msg("Metrics server error")
		}
	}()

	wsServer := server.NewServer(cfg, logger, authService, userService, roomService, messageService, presenceService, ps)

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	allowedOrigins := []string{"http://localhost:3000", "http://localhost:8085", "http://127.0.0.1:3000", "http://127.0.0.1:8085"}
	router.Use(gin.Recovery(), middleware.CORSMiddleware(allowedOrigins), middleware.RequestIDMiddleware())

	healthChecker := health.NewChecker(db, ps)
	setupRoutes(router, cfg, authService, roomService, messageService, ps, healthChecker)

	apiServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.HTTP.ReadTimeout,
		WriteTimeout: cfg.Server.HTTP.WriteTimeout,
		IdleTimeout:  cfg.Server.HTTP.IdleTimeout,
	}

	go func() {
		logger.Info().Str("addr", apiServer.Addr).Msg("Starting REST API")
		if err := apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error().Err(err).Msg("REST API error")
		}
	}()

	go func() {
		wsPort := cfg.Server.Port + 1
		logger.Info().Str("host", cfg.Server.Host).Int("port", wsPort).Msg("Starting WebSocket server")
		if err := wsServer.Start(); err != nil && err != http.ErrServerClosed {
			logger.Fatal().Err(err).Msg("WebSocket server error")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info().Msg("Shutting down server gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := wsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("WebSocket server forced to shutdown")
	}

	if err := apiServer.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("REST API forced to shutdown")
	}

	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("Metrics server forced to shutdown")
	}

	logger.Info().Msg("Server exited cleanly")
}

func setupRoutes(
	router *gin.Engine,
	cfg *config.Config,
	authService *service.AuthService,
	roomService *service.RoomService,
	messageService *service.MessageService,
	ps pubsub.PubSub,
	healthChecker *health.Checker,
) {
	router.GET("/healthz", healthChecker.LivenessHandler)
	router.GET("/readyz", healthChecker.ReadinessHandler)
	router.GET("/health", healthChecker.ReadinessHandler)

	router.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"name":    "WebSocket Chat API",
			"version": "1.0.0",
			"endpoints": map[string]string{
				"healthz": "/healthz",
				"readyz":  "/readyz",
				"ws":      fmt.Sprintf("/ws (port %d)", cfg.Server.Port+1),
				"auth":    "/api/v1/auth",
				"rooms":   "/api/v1/rooms",
				"metrics": fmt.Sprintf("/metrics (port %d)", cfg.Observability.Metrics.Port),
			},
		})
	})

	api := router.Group("/api/v1")
	{
		authRateLimit := middleware.RateLimitMiddleware(ps, "auth", 10, time.Minute)

		auth := api.Group("/auth")
		auth.Use(authRateLimit)
		{
			auth.POST("/register", func(c *gin.Context) {
				var input service.RegisterInput
				if err := c.ShouldBindJSON(&input); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "Invalid registration request payload"})
					return
				}

				user, err := authService.Register(c.Request.Context(), input)
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
			})

			auth.POST("/login", func(c *gin.Context) {
				var input service.LoginInput
				if err := c.ShouldBindJSON(&input); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "Invalid login request payload"})
					return
				}

				tokens, err := authService.Login(c.Request.Context(), input)
				if err != nil {
					c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "Invalid credentials"})
					return
				}

				c.JSON(http.StatusOK, gin.H{
					"access_token":  tokens.AccessToken,
					"refresh_token": tokens.RefreshToken,
					"token_type":    "Bearer",
					"expires_in":    int(cfg.Auth.JWT.AccessTokenTTL.Seconds()),
				})
			})

			auth.POST("/refresh", func(c *gin.Context) {
				var input struct {
					RefreshToken string `json:"refresh_token" binding:"required"`
				}
				if err := c.ShouldBindJSON(&input); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "Invalid refresh payload"})
					return
				}

				tokens, err := authService.RefreshToken(c.Request.Context(), input.RefreshToken)
				if err != nil {
					c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "Invalid refresh token"})
					return
				}

				c.JSON(http.StatusOK, gin.H{
					"access_token":  tokens.AccessToken,
					"refresh_token": tokens.RefreshToken,
					"token_type":    "Bearer",
				})
			})
		}

		authMiddleware := func(c *gin.Context) {
			token := c.GetHeader("Authorization")
			if token == "" {
				c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "Authorization header required"})
				c.Abort()
				return
			}

			if len(token) > 7 && token[:7] == "Bearer " {
				token = token[7:]
			}

			user, err := authService.ValidateToken(c.Request.Context(), token)
			if err != nil {
				c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "Invalid or expired token"})
				c.Abort()
				return
			}

			c.Set("user_id", user.ID)
			c.Set("user", user)
			c.Next()
		}

		rooms := api.Group("/rooms")
		rooms.Use(authMiddleware)
		{
			rooms.GET("", func(c *gin.Context) {
				val, exists := c.Get("user_id")
				if !exists {
					c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "User not authenticated"})
					return
				}
				userID, ok := val.(string)
				if !ok {
					c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "Invalid context state"})
					return
				}
				rooms, err := roomService.GetUserRooms(c.Request.Context(), userID)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "Failed to retrieve user rooms"})
					return
				}
				c.JSON(http.StatusOK, rooms)
			})

			rooms.POST("", func(c *gin.Context) {
				var input struct {
					Name        string `json:"name" binding:"required"`
					Type        string `json:"type" binding:"required"`
					Description string `json:"description"`
				}
				if err := c.ShouldBindJSON(&input); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_INPUT", "message": "Invalid room payload"})
					return
				}

				val, exists := c.Get("user_id")
				if !exists {
					c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "User not authenticated"})
					return
				}
				userID, _ := val.(string)

				room, err := roomService.Create(c.Request.Context(), service.CreateRoomInput{
					Name:        input.Name,
					Type:        model.RoomType(input.Type),
					Description: input.Description,
					CreatedBy:   userID,
				})
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_ROOM_FAILED", "message": "Failed to create room"})
					return
				}

				c.JSON(http.StatusCreated, room)
			})

			rooms.GET("/:id", func(c *gin.Context) {
				roomID := c.Param("id")
				room, err := roomService.GetByID(c.Request.Context(), roomID)
				if err != nil {
					c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Room not found"})
					return
				}
				c.JSON(http.StatusOK, room)
			})

			rooms.GET("/:id/messages", func(c *gin.Context) {
				roomID := c.Param("id")
				limit := 50
				messages, err := messageService.GetRoomMessages(c.Request.Context(), roomID, limit, nil)
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "Failed to retrieve room messages"})
					return
				}
				c.JSON(http.StatusOK, messages)
			})

			rooms.POST("/:id/join", func(c *gin.Context) {
				roomID := c.Param("id")
				val, exists := c.Get("user_id")
				if !exists {
					c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "User not authenticated"})
					return
				}
				userID, _ := val.(string)

				if err := roomService.JoinRoom(c.Request.Context(), roomID, userID, ps); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": "JOIN_FAILED", "message": "Failed to join room"})
					return
				}
				c.JSON(http.StatusOK, gin.H{"status": "joined"})
			})

			rooms.POST("/:id/leave", func(c *gin.Context) {
				roomID := c.Param("id")
				val, exists := c.Get("user_id")
				if !exists {
					c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "User not authenticated"})
					return
				}
				userID, _ := val.(string)

				if err := roomService.LeaveRoom(c.Request.Context(), roomID, userID, ps); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"code": "LEAVE_FAILED", "message": "Failed to leave room"})
					return
				}
				c.JSON(http.StatusOK, gin.H{"status": "left"})
			})
		}
	}
}
