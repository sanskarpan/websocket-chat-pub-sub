package errors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewAPIError(t *testing.T) {
	err := NewAPIError("TEST_ERROR", "test message", errors.New("original"))

	assert.Equal(t, "TEST_ERROR", err.Code)
	assert.Equal(t, "test message", err.Message)
	assert.Contains(t, err.Error(), "test message")
}

func TestNotFound(t *testing.T) {
	err := NotFound("user")

	assert.Equal(t, "NOT_FOUND", err.Code)
	assert.Contains(t, err.Message, "user not found")
}

func TestUnauthorized(t *testing.T) {
	err := Unauthorized("invalid token")

	assert.Equal(t, "UNAUTHORIZED", err.Code)
	assert.Equal(t, "invalid token", err.Message)
}

func TestBadRequest(t *testing.T) {
	err := BadRequest("invalid input")

	assert.Equal(t, "BAD_REQUEST", err.Code)
	assert.Equal(t, "invalid input", err.Message)
}

func TestConflict(t *testing.T) {
	err := Conflict("username")

	assert.Equal(t, "CONFLICT", err.Code)
	assert.Contains(t, err.Message, "username already exists")
}
