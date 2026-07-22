package config_test

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/websocket-chat/internal/config"
)

func TestConfigLoadDefaults(t *testing.T) {
	cfg := config.Load()
	assert.NotNil(t, cfg)
	assert.Equal(t, "websocket-chat", cfg.App.Name)
	assert.Equal(t, 8085, cfg.Server.Port)
	assert.NoError(t, cfg.Validate())
}

func TestConfigValidationInProduction(t *testing.T) {
	cfg := config.Load()
	cfg.App.Environment = "production"
	cfg.Auth.JWT.PrivateKey = "short-secret"

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_SECRET must be at least 32 characters long")

	cfg.Auth.JWT.PrivateKey = "this-is-a-very-long-secret-key-that-is-at-least-32-bytes!"
	cfg.Database.Postgresql.Password = ""
	err = cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DB_PASSWORD environment variable")

	cfg.Database.Postgresql.Password = "securepass"
	assert.NoError(t, cfg.Validate())
}

func TestEnvironmentOverride(t *testing.T) {
	os.Setenv("JWT_SECRET", "custom-secret-key-that-has-over-thirty-two-bytes-length")
	defer os.Unsetenv("JWT_SECRET")

	cfg := config.Load()
	assert.Equal(t, "custom-secret-key-that-has-over-thirty-two-bytes-length", cfg.Auth.JWT.PrivateKey)
}
