package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func CORSMiddleware(allowedOrigins []string) gin.HandlerFunc {
	hasWildcard := false
	for _, o := range allowedOrigins {
		if o == "*" {
			hasWildcard = true
			break
		}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		allowed := false
		originAllowed := false
		for _, o := range allowedOrigins {
			if o == origin {
				allowed = true
				originAllowed = true
				break
			}
			if o == "*" {
				allowed = true
			}
		}

		if allowed && !hasWildcard && originAllowed {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin")
		} else if hasWildcard {
			c.Header("Access-Control-Allow-Origin", "*")
		}

		c.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, X-Request-ID")
		c.Header("Access-Control-Allow-Methods", "POST, HEAD, PATCH, OPTIONS, GET, PUT, DELETE")
		c.Header("Access-Control-Max-Age", "86400")

		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = "req-" + uuid.New().String()
		}
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		c.Next()
	}
}
