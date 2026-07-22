package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/websocket-chat/internal/pubsub"
)

func RateLimitMiddleware(ps pubsub.PubSub, actionKey string, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if ps == nil || limit <= 0 {
			c.Next()
			return
		}

		clientIP := c.ClientIP()
		userID := c.GetString("user_id")
		identifier := clientIP
		if userID != "" {
			identifier = userID
		}

		key := actionKey + ":" + identifier
		allowed, err := ps.CheckRateLimit(c.Request.Context(), key, limit, window)
		if err != nil {
			// If Redis check fails, fail open to avoid service outage, but log error
			c.Next()
			return
		}

		if !allowed {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":        "RATE_LIMIT_EXCEEDED",
				"message":     "Too many requests, please try again later",
				"retry_after": int(window.Seconds()),
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
