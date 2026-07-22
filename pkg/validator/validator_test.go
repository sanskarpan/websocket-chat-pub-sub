package validator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr error
	}{
		{"valid email", "test@example.com", nil},
		{"invalid email", "invalid", ErrInvalidEmail},
		{"empty email", "", ErrInvalidEmail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmail(tt.email)
			assert.Equal(t, tt.wantErr, err)
		})
	}
}

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		wantErr  error
	}{
		{"valid username", "john_ddoe", nil},
		{"too short", "ab", ErrTooShort},
		{"too long", "thisisaverylongusernamethatexceedslimit", ErrTooLong},
		{"invalid chars", "user@name", ErrInvalidUsername},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUsername(tt.username)
			assert.Equal(t, tt.wantErr, err)
		})
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  error
	}{
		{"valid password", "secure123", nil},
		{"too short", "short", ErrTooShort},
		{"too long", string(make([]byte, 200)), ErrTooLong},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			assert.Equal(t, tt.wantErr, err)
		})
	}
}
