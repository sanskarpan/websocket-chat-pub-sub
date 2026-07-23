package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/websocket-chat/internal/cache"
	"github.com/websocket-chat/internal/config"
	"github.com/websocket-chat/internal/handlers"
	"github.com/websocket-chat/internal/health"
	"github.com/websocket-chat/internal/logging"
	"github.com/websocket-chat/internal/metrics"
	"github.com/websocket-chat/internal/middleware"
	"github.com/websocket-chat/internal/pubsub"
	"github.com/websocket-chat/internal/repository"
	"github.com/websocket-chat/internal/server"
	"github.com/websocket-chat/internal/service"
	"github.com/websocket-chat/internal/tracing"
	"github.com/websocket-chat/pkg/snowflake"
)

func main() {
	cfg := config.Load()
	logHandle := logging.NewLogger(cfg)
	logger := logHandle.Logger
	defer func() {
		if err := logHandle.Closer.Close(); err != nil {
			logger.Error().Err(err).Msg("Failed to close log file")
		}
	}()

	if nodeIDStr := os.Getenv("SNOWFLAKE_NODE_ID"); nodeIDStr != "" {
		if nodeID, err := strconv.ParseInt(nodeIDStr, 10, 64); err == nil {
			if err := snowflake.SetNodeID(nodeID); err != nil {
				logger.Warn().Err(err).Msg("Invalid SNOWFLAKE_NODE_ID")
			} else {
				logger.Info().Int64("node_id", nodeID).Msg("Snowflake node ID set from environment")
			}
		}
	}

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
	authService.SetTokenInvalidator(ps)
	userService := service.NewUserService(userRepo, redisCache)
	roomService := service.NewRoomService(roomRepo, userRepo, redisCache, ps)
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
	router.Use(gin.Recovery(), middleware.CORSMiddleware(allowedOrigins), middleware.RequestIDMiddleware(), middleware.RequestLoggingMiddleware(cfg.App.Name))

	healthChecker := health.NewChecker(db, ps)
	h := handlers.New(cfg, authService, roomService, messageService, ps)
	setupRoutes(router, cfg, authService, healthChecker, h, ps)

	apiServer := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  cfg.Server.HTTP.ReadTimeout,
		WriteTimeout: cfg.Server.HTTP.WriteTimeout,
		IdleTimeout:  cfg.Server.HTTP.IdleTimeout,
	}
	if cfg.Server.TLS.Enabled {
		apiServer.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	go func() {
		logger.Info().Str("addr", apiServer.Addr).Msg("Starting REST API")
		var err error
		if cfg.Server.TLS.Enabled && cfg.Server.TLS.CertFile != "" && cfg.Server.TLS.KeyFile != "" {
			err = apiServer.ListenAndServeTLS(cfg.Server.TLS.CertFile, cfg.Server.TLS.KeyFile)
		} else {
			err = apiServer.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
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
	healthChecker *health.Checker,
	h *handlers.Handlers,
	ps pubsub.PubSub,
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
		authRateLimit := 10
		if os.Getenv("AUTH_RATE_LIMIT_DISABLE") == "1" {
			authRateLimit = 0
		}
		auth := api.Group("/auth")
		auth.Use(middleware.RateLimitMiddleware(ps, "auth", authRateLimit, time.Minute))
		{
			auth.POST("/register", h.Register)
			auth.POST("/login", h.Login)
			auth.POST("/refresh", h.Refresh)
		}

		rooms := api.Group("/rooms")
		rooms.Use(handlers.AuthMiddleware(authService))
		{
			rooms.GET("", h.ListRooms)
			rooms.POST("", h.CreateRoom)
			rooms.GET("/:id", h.GetRoom)
			rooms.GET("/:id/messages", h.GetRoomMessages)
			rooms.POST("/:id/join", h.JoinRoom)
			rooms.POST("/:id/leave", h.LeaveRoom)
		}
	}
}