package server_test

import (
	"context"
	"sync"
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

func TestHub_CloseAll_Idempotent(t *testing.T) {
	logger := zerolog.Nop()
	hub := server.NewHub(nil, &logger)
	go hub.Run()

	// Calling CloseAll twice must not panic (sync.Once protection)
	assert.NotPanics(t, func() {
		hub.CloseAll()
		hub.CloseAll()
	})
}

func TestHub_MultipleInstances_Independent(t *testing.T) {
	logger := zerolog.Nop()

	hub1 := server.NewHub(nil, &logger)
	hub2 := server.NewHub(nil, &logger)

	go hub1.Run()
	go hub2.Run()

	assert.NotNil(t, hub1)
	assert.NotNil(t, hub2)
	assert.NotEqual(t, hub1, hub2, "each NewHub call must produce a distinct instance")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		hub1.CloseAll()
		hub2.CloseAll()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("hubs did not shut down in time")
	}
}

func TestHub_ShutdownUnderConcurrency(t *testing.T) {
	logger := zerolog.Nop()
	hub := server.NewHub(nil, &logger)
	go hub.Run()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Concurrent CloseAll calls must not race or panic
	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				hub.CloseAll()
			}()
		}
		wg.Wait()
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("concurrent CloseAll timed out")
	}
}
