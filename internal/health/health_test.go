package health_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/websocket-chat/internal/health"
)

func TestHealthEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	checker := health.NewChecker(nil, nil)

	r := gin.New()
	r.GET("/healthz", checker.LivenessHandler)
	r.GET("/readyz", checker.ReadinessHandler)

	t.Run("LivenessProbe", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/healthz", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var body map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &body)
		assert.NoError(t, err)
		assert.Equal(t, "alive", body["status"])
	})

	t.Run("ReadinessProbeWithoutDB", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/readyz", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var body map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &body)
		assert.NoError(t, err)
		assert.Equal(t, "ready", body["status"])
	})
}
