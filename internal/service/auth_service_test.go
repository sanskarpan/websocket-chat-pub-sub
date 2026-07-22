package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
)

func TestAuthService_Register(t *testing.T) {
	t.Skip("Requires database connection")
}

func TestAuthService_Login(t *testing.T) {
	t.Skip("Requires database connection")
}

func TestPasswordHashing(t *testing.T) {
	password := "testpassword123"

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	assert.NoError(t, err)
	assert.NotEmpty(t, hash)

	err = bcrypt.CompareHashAndPassword(hash, []byte(password))
	assert.NoError(t, err)

	err = bcrypt.CompareHashAndPassword(hash, []byte("wrongpassword"))
	assert.Error(t, err)
}

func TestTokenGeneration(t *testing.T) {
	t.Skip("Requires config setup")
}
