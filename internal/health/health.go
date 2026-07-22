package health

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/websocket-chat/internal/pubsub"
)

type Checker struct {
	db *pgxpool.Pool
	ps pubsub.PubSub
}

func NewChecker(db *pgxpool.Pool, ps pubsub.PubSub) *Checker {
	return &Checker{
		db: db,
		ps: ps,
	}
}

func (h *Checker) LivenessHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "alive",
		"timestamp": time.Now().Unix(),
	})
}

func (h *Checker) ReadinessHandler(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	status := gin.H{
		"status":    "ready",
		"timestamp": time.Now().Unix(),
		"checks":    gin.H{},
	}
	ready := true
	checks := status["checks"].(gin.H)

	if h.db != nil {
		if err := h.db.Ping(ctx); err != nil {
			checks["database"] = "down: " + err.Error()
			ready = false
		} else {
			checks["database"] = "up"
		}
	} else {
		checks["database"] = "unconfigured"
	}

	if h.ps != nil {
		// Verify Redis connectivity by checking presence for system health check
		if _, err := h.ps.GetPresence(ctx, "healthcheck_ping"); err != nil {
			checks["redis"] = "down: " + err.Error()
			ready = false
		} else {
			checks["redis"] = "up"
		}
	} else {
		checks["redis"] = "unconfigured"
	}

	if ready {
		c.JSON(http.StatusOK, status)
	} else {
		status["status"] = "degraded"
		c.JSON(http.StatusServiceUnavailable, status)
	}
}
