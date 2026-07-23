package service

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/websocket-chat/internal/config"
	"github.com/websocket-chat/internal/model"
	"github.com/websocket-chat/internal/repository"
	"github.com/websocket-chat/internal/tracing"
	"github.com/websocket-chat/pkg/sanitization"
	"github.com/websocket-chat/pkg/validator"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserExists         = errors.New("user already exists")
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidToken       = errors.New("invalid token")
	ErrTokenExpired       = errors.New("token expired")
	ErrInvalidTokenType   = errors.New("invalid token type")
)

type AuthService struct {
	cfg              *config.Config
	userRepo         repository.IUserRepository
	tokenInvalidator TokenInvalidator
	dummyHash        []byte
	rsaPrivateKey    *rsa.PrivateKey
	rsaPublicKey     *rsa.PublicKey
}

func NewAuthService(cfg *config.Config, userRepo repository.IUserRepository) *AuthService {
	dummyHash, _ := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing-attack-mitigation"), bcrypt.MinCost)

	var rsaPriv *rsa.PrivateKey
	var rsaPub *rsa.PublicKey

	if key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(cfg.Auth.JWT.PrivateKey)); err == nil {
		rsaPriv = key
	}
	if key, err := jwt.ParseRSAPublicKeyFromPEM([]byte(cfg.Auth.JWT.PublicKey)); err == nil {
		rsaPub = key
	}

	return &AuthService{cfg: cfg, userRepo: userRepo, dummyHash: dummyHash, rsaPrivateKey: rsaPriv, rsaPublicKey: rsaPub}
}

func (s *AuthService) SetTokenInvalidator(inv TokenInvalidator) {
	s.tokenInvalidator = inv
}

type RegisterInput struct {
	Username    string `json:"username" binding:"required,min=3,max=30"`
	Email       string `json:"email" binding:"required,email"`
	Password    string `json:"password" binding:"required,min=8"`
	DisplayName string `json:"display_name"`
}

type LoginInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
}

func (s *AuthService) Register(ctx context.Context, input RegisterInput) (*model.User, error) {
	ctx, spanEnd := tracing.StartSpan(ctx, "auth.register")
	defer spanEnd()
	input.Username = sanitization.SanitizeUsername(input.Username)
	input.Email = validator.SanitizeString(input.Email)
	input.DisplayName = sanitization.SanitizeMessage(input.DisplayName)

	if err := validator.ValidateEmail(input.Email); err != nil {
		return nil, err
	}
	if err := validator.ValidateUsername(input.Username); err != nil {
		return nil, err
	}
	if err := validator.ValidatePassword(input.Password); err != nil {
		return nil, err
	}

	existing, _ := s.userRepo.GetByUsername(ctx, input.Username)
	if existing != nil {
		return nil, ErrUserExists
	}

	existing, _ = s.userRepo.GetByEmail(ctx, input.Email)
	if existing != nil {
		return nil, ErrUserExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), s.cfg.Auth.BCrypt.Cost)
	if err != nil {
		return nil, err
	}

	user := &model.User{
		Username:     input.Username,
		Email:        input.Email,
		PasswordHash: string(hash),
		DisplayName:  input.DisplayName,
		Status:       model.StatusOffline,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

func (s *AuthService) Login(ctx context.Context, input LoginInput) (*TokenPair, error) {
	ctx, spanEnd := tracing.StartSpan(ctx, "auth.login")
	defer spanEnd()
	user, err := s.userRepo.GetByEmail(ctx, input.Email)
	if err != nil {
		bcrypt.CompareHashAndPassword(s.dummyHash, []byte(input.Password))
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	accessToken, err := s.generateAccessToken(user)
	if err != nil {
		return nil, err
	}

	refreshToken, err := s.generateRefreshToken(user)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

func (s *AuthService) ValidateToken(ctx context.Context, tokenString string) (*model.User, error) {
	ctx, spanEnd := tracing.StartSpan(ctx, "auth.validate_token")
	defer spanEnd()
	return s.validateToken(ctx, tokenString, "access")
}

func (s *AuthService) validateAudience(claims jwt.MapClaims) bool {
	if len(s.cfg.Auth.JWT.Audience) == 0 {
		return true
	}

	aud, ok := claims["aud"]
	if !ok {
		return false
	}

	switch v := aud.(type) {
	case string:
		for _, a := range s.cfg.Auth.JWT.Audience {
			if a == v {
				return true
			}
		}
	case []interface{}:
		for _, a := range v {
			if audStr, ok := a.(string); ok {
				for _, allowed := range s.cfg.Auth.JWT.Audience {
					if allowed == audStr {
						return true
					}
				}
			}
		}
	}
	return false
}

func (s *AuthService) ValidateRefreshToken(ctx context.Context, tokenString string) (*model.User, error) {
	user, err := s.validateToken(ctx, tokenString, "refresh")
	if err != nil {
		return nil, err
	}

	if s.tokenInvalidator != nil {
		var parser jwt.Parser
		claims := jwt.MapClaims{}
		_, _, err := parser.ParseUnverified(tokenString, claims)
		if err == nil {
			if jti, ok := claims["jti"].(string); ok && jti != "" {
				invalidated, err := s.tokenInvalidator.IsTokenInvalidated(ctx, jti)
				if err == nil && invalidated {
					return nil, ErrInvalidToken
				}
			}
		}
	}

	return user, nil
}

func (s *AuthService) validateToken(ctx context.Context, tokenString string, expectedType string) (*model.User, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, ErrInvalidToken
		}
		return s.rsaPublicKey, nil
	}, jwt.WithIssuer(s.cfg.Auth.JWT.Issuer))

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrInvalidToken
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrInvalidToken
	}

	if !s.validateAudience(claims) {
		return nil, ErrInvalidToken
	}

	tokenType, ok := claims["type"].(string)
	if !ok || tokenType != expectedType {
		return nil, ErrInvalidTokenType
	}

	userID, ok := claims["sub"].(string)
	if !ok {
		return nil, ErrInvalidToken
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, ErrInvalidToken
	}

	return user, nil
}

func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, error) {
	ctx, spanEnd := tracing.StartSpan(ctx, "auth.refresh_token")
	defer spanEnd()
	parsed, err := jwt.Parse(refreshToken, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, ErrInvalidToken
		}
		return s.rsaPublicKey, nil
	}, jwt.WithIssuer(s.cfg.Auth.JWT.Issuer))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrInvalidToken
	}

	if !parsed.Valid {
		return nil, ErrInvalidToken
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrInvalidToken
	}

	tokenType, ok := claims["type"].(string)
	if !ok || tokenType != "refresh" {
		return nil, ErrInvalidTokenType
	}

	if !s.validateAudience(claims) {
		return nil, ErrInvalidToken
	}

	jti, _ := claims["jti"].(string)
	if jti != "" && s.tokenInvalidator != nil {
		invalidated, err := s.tokenInvalidator.IsTokenInvalidated(ctx, jti)
		if err == nil && invalidated {
			return nil, ErrInvalidToken
		}
	}

	userID, ok := claims["sub"].(string)
	if !ok {
		return nil, ErrInvalidToken
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, ErrInvalidToken
	}

	accessToken, err := s.generateAccessToken(user)
	if err != nil {
		return nil, err
	}

	newRefreshToken, err := s.generateRefreshToken(user)
	if err != nil {
		return nil, err
	}

	if jti != "" {
		if err := s.invalidateToken(ctx, jti, s.cfg.Auth.JWT.RefreshTokenTTL); err != nil {
			return nil, fmt.Errorf("failed to invalidate old token: %w", err)
		}
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: newRefreshToken,
	}, nil
}

func (s *AuthService) invalidateToken(ctx context.Context, jti string, ttl time.Duration) error {
	if s.tokenInvalidator == nil {
		return nil
	}
	return s.tokenInvalidator.InvalidateToken(ctx, jti, ttl)
}

type TokenInvalidator interface {
	InvalidateToken(ctx context.Context, jti string, ttl time.Duration) error
	IsTokenInvalidated(ctx context.Context, jti string) (bool, error)
}

func (s *AuthService) generateAccessToken(user *model.User) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":  user.ID,
		"type": "access",
		"exp":  now.Add(s.cfg.Auth.JWT.AccessTokenTTL).Unix(),
		"iat":  now.Unix(),
		"iss":  s.cfg.Auth.JWT.Issuer,
		"aud":  s.cfg.Auth.JWT.Audience,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(s.rsaPrivateKey)
}

func (s *AuthService) generateRefreshToken(user *model.User) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":  user.ID,
		"type": "refresh",
		"jti":  uuid.New().String(),
		"exp":  now.Add(s.cfg.Auth.JWT.RefreshTokenTTL).Unix(),
		"iat":  now.Unix(),
		"iss":  s.cfg.Auth.JWT.Issuer,
		"aud":  s.cfg.Auth.JWT.Audience,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(s.rsaPrivateKey)
}
