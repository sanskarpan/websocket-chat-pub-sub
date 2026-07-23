package service_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/websocket-chat/internal/config"
	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/service"
	"golang.org/x/crypto/bcrypt"
)

func testRSAKeyPair(t *testing.T) (privPEM, pubPEM string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	privBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)
	privPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}))

	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)
	pubPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes}))

	return privPEM, pubPEM
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	privPEM, pubPEM := testRSAKeyPair(t)
	return &config.Config{
		App: config.AppConfig{Name: "test", Version: "1.0.0", Environment: "development"},
		Auth: config.AuthConfig{
			JWT: config.JWTConfig{
				Algorithm:       "RS256",
				PrivateKey:      privPEM,
				PublicKey:       pubPEM,
				AccessTokenTTL:  15 * time.Minute,
				RefreshTokenTTL: 7 * 24 * time.Hour,
				Issuer:          "test-issuer",
				Audience:        []string{"test-audience"},
			},
			BCrypt: config.BCryptConfig{Cost: bcrypt.MinCost},
		},
	}
}

func TestAuthService_Register_Success(t *testing.T) {
	cfg := testConfig(t)
	userRepo := NewFakeUserRepository()
	authService := service.NewAuthService(cfg, userRepo)

	user, err := authService.Register(context.Background(), service.RegisterInput{
		Username:    "testuser",
		Email:       "test@example.com",
		Password:    "password123",
		DisplayName: "Test User",
	})

	require.NoError(t, err)
	assert.NotEmpty(t, user.ID)
	assert.Equal(t, "testuser", user.Username)
	assert.Equal(t, "test@example.com", user.Email)
	assert.NotEmpty(t, user.PasswordHash)
	assert.Equal(t, "Test User", user.DisplayName)
	assert.Equal(t, model.StatusOffline, user.Status)
}

func TestAuthService_Register_UsernameExists(t *testing.T) {
	cfg := testConfig(t)
	userRepo := NewFakeUserRepository()
	authService := service.NewAuthService(cfg, userRepo)

	_, err := authService.Register(context.Background(), service.RegisterInput{
		Username: "existinguser",
		Email:    "first@example.com",
		Password: "password123",
	})
	require.NoError(t, err)

	_, err = authService.Register(context.Background(), service.RegisterInput{
		Username: "existinguser",
		Email:    "second@example.com",
		Password: "password456",
	})
	assert.ErrorIs(t, err, service.ErrUserExists)
}

func TestAuthService_Register_EmailExists(t *testing.T) {
	cfg := testConfig(t)
	userRepo := NewFakeUserRepository()
	authService := service.NewAuthService(cfg, userRepo)

	_, err := authService.Register(context.Background(), service.RegisterInput{
		Username: "firstuser",
		Email:    "same@example.com",
		Password: "password123",
	})
	require.NoError(t, err)

	_, err = authService.Register(context.Background(), service.RegisterInput{
		Username: "seconduser",
		Email:    "same@example.com",
		Password: "password456",
	})
	assert.ErrorIs(t, err, service.ErrUserExists)
}

func TestAuthService_Register_InvalidEmail(t *testing.T) {
	cfg := testConfig(t)
	userRepo := NewFakeUserRepository()
	authService := service.NewAuthService(cfg, userRepo)

	_, err := authService.Register(context.Background(), service.RegisterInput{
		Username: "newuser",
		Email:    "not-an-email",
		Password: "password123",
	})
	assert.Error(t, err)
}

func TestAuthService_Register_ShortUsername(t *testing.T) {
	cfg := testConfig(t)
	userRepo := NewFakeUserRepository()
	authService := service.NewAuthService(cfg, userRepo)

	_, err := authService.Register(context.Background(), service.RegisterInput{
		Username: "ab",
		Email:    "valid@example.com",
		Password: "password123",
	})
	assert.Error(t, err)
}

func TestAuthService_Register_ShortPassword(t *testing.T) {
	cfg := testConfig(t)
	userRepo := NewFakeUserRepository()
	authService := service.NewAuthService(cfg, userRepo)

	_, err := authService.Register(context.Background(), service.RegisterInput{
		Username: "validuser",
		Email:    "valid@example.com",
		Password: "short",
	})
	assert.Error(t, err)
}

func TestAuthService_Register_SanitizesXSS(t *testing.T) {
	cfg := testConfig(t)
	userRepo := NewFakeUserRepository()
	authService := service.NewAuthService(cfg, userRepo)

	user, err := authService.Register(context.Background(), service.RegisterInput{
		Username:    "xssuser",
		Email:       "xss@example.com",
		Password:    "password123",
		DisplayName: "<script>alert('xss')</script>Safe Name",
	})
	require.NoError(t, err)
	assert.NotContains(t, user.DisplayName, "<script>")
}

func TestAuthService_Login_Success(t *testing.T) {
	cfg := testConfig(t)
	userRepo := NewFakeUserRepository()
	authService := service.NewAuthService(cfg, userRepo)

	_, err := authService.Register(context.Background(), service.RegisterInput{
		Username: "loginuser",
		Email:    "login@example.com",
		Password: "password123",
	})
	require.NoError(t, err)

	tokens, err := authService.Login(context.Background(), service.LoginInput{
		Email:    "login@example.com",
		Password: "password123",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, tokens.AccessToken)
	assert.NotEmpty(t, tokens.RefreshToken)
}

func TestAuthService_Login_InvalidEmail(t *testing.T) {
	cfg := testConfig(t)
	userRepo := NewFakeUserRepository()
	authService := service.NewAuthService(cfg, userRepo)

	_, err := authService.Login(context.Background(), service.LoginInput{
		Email:    "nonexistent@example.com",
		Password: "anypassword",
	})
	assert.ErrorIs(t, err, service.ErrInvalidCredentials)
}

func TestAuthService_Login_WrongPassword(t *testing.T) {
	cfg := testConfig(t)
	userRepo := NewFakeUserRepository()
	authService := service.NewAuthService(cfg, userRepo)

	_, err := authService.Register(context.Background(), service.RegisterInput{
		Username: "pwuser",
		Email:    "pw@example.com",
		Password: "rightpassword",
	})
	require.NoError(t, err)

	_, err = authService.Login(context.Background(), service.LoginInput{
		Email:    "pw@example.com",
		Password: "wrongpassword",
	})
	assert.ErrorIs(t, err, service.ErrInvalidCredentials)
}

func TestAuthService_ValidateToken(t *testing.T) {
	cfg := testConfig(t)
	userRepo := NewFakeUserRepository()
	authService := service.NewAuthService(cfg, userRepo)

	_, err := authService.Register(context.Background(), service.RegisterInput{
		Username: "validuser",
		Email:    "valid@example.com",
		Password: "password123",
	})
	require.NoError(t, err)

	tokens, err := authService.Login(context.Background(), service.LoginInput{
		Email:    "valid@example.com",
		Password: "password123",
	})
	require.NoError(t, err)

	user, err := authService.ValidateToken(context.Background(), tokens.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, "validuser", user.Username)
}

func TestAuthService_ValidateToken_Invalid(t *testing.T) {
	cfg := testConfig(t)
	userRepo := NewFakeUserRepository()
	authService := service.NewAuthService(cfg, userRepo)

	_, err := authService.ValidateToken(context.Background(), "invalid.token.here")
	assert.Error(t, err)
}

func TestAuthService_RefreshToken_InvalidatesOld(t *testing.T) {
	cfg := testConfig(t)
	userRepo := NewFakeUserRepository()
	authService := service.NewAuthService(cfg, userRepo)

	_, err := authService.Register(context.Background(), service.RegisterInput{
		Username: "refreshuser",
		Email:    "refresh@example.com",
		Password: "password123",
	})
	require.NoError(t, err)

	tokens1, err := authService.Login(context.Background(), service.LoginInput{
		Email:    "refresh@example.com",
		Password: "password123",
	})
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)
	tokens2, err := authService.RefreshToken(context.Background(), tokens1.RefreshToken)
	require.NoError(t, err)
	assert.NotEqual(t, tokens1.RefreshToken, tokens2.RefreshToken)
}

func TestPasswordHashing(t *testing.T) {
	password := "testpassword123"

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	assert.NoError(t, err)
	assert.NotEmpty(t, hash)

	err = bcrypt.CompareHashAndPassword(hash, []byte(password))
	assert.NoError(t, err)

	err = bcrypt.CompareHashAndPassword(hash, []byte("wrongpassword"))
	assert.Error(t, err)
}