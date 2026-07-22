package server_test

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/websocket-chat/internal/server"
)

func TestHubLifecycle(t *testing.T) {
	logger := zerolog.Nop()
	hub := server.NewHub(nil, &logger)

	go hub.Run()

	t.Run("CreateHub", func(t *testing.T) {
		assert.NotNil(t, hub)
	})

	t.Run("ShutdownHub", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		done := make(chan struct{})
		go func() {
			hub.CloseAll()
			close(done)
		}()

		select {
		case <-done:
			assert.True(t, true)
		case <-ctx.Done():
			t.Fatal("hub shutdown timed out")
		}
	})
}
