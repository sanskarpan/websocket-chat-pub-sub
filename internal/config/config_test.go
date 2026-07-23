package config_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/websocket-chat/internal/config"
)

func generateTestRSAKeyPair(t *testing.T) (privPEM, pubPEM string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(t, err)

	privBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	assert.NoError(t, err)
	privPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}))

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	assert.NoError(t, err)
	pubPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}))

	return privPEM, pubPEM
}

func TestConfigLoadDefaults(t *testing.T) {
	cfg := config.Load()
	assert.NotNil(t, cfg)
	assert.Equal(t, "websocket-chat", cfg.App.Name)
	assert.Equal(t, 8085, cfg.Server.Port)
	assert.Equal(t, "RS256", cfg.Auth.JWT.Algorithm)
	assert.NotEmpty(t, cfg.Auth.JWT.PrivateKey)
	assert.NotEmpty(t, cfg.Auth.JWT.PublicKey)
	assert.NoError(t, cfg.Validate())
}

func TestConfigValidationInProduction(t *testing.T) {
	cfg := config.Load()
	cfg.App.Environment = "production"
	cfg.Auth.JWT.PrivateKey = ""
	cfg.Auth.JWT.PublicKey = ""

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "JWT private key")

	privPEM, pubPEM := generateTestRSAKeyPair(t)
	cfg.Auth.JWT.PrivateKey = privPEM
	cfg.Auth.JWT.PublicKey = pubPEM

	cfg.Database.Postgresql.Password = ""
	err = cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DB_PASSWORD environment variable")

	cfg.Database.Postgresql.Password = "securepass"
	assert.NoError(t, cfg.Validate())
}

func TestEnvironmentOverride(t *testing.T) {
	privPEM, pubPEM := generateTestRSAKeyPair(t)
	os.Setenv("JWT_PRIVATE_KEY", privPEM)
	os.Setenv("JWT_PUBLIC_KEY", pubPEM)
	defer os.Unsetenv("JWT_PRIVATE_KEY")
	defer os.Unsetenv("JWT_PUBLIC_KEY")

	cfg := config.Load()
	assert.Equal(t, privPEM, cfg.Auth.JWT.PrivateKey)
	assert.Equal(t, pubPEM, cfg.Auth.JWT.PublicKey)
}
