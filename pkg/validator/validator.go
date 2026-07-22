package validator

import (
	"errors"
	"regexp"
	"strings"
)

var (
	ErrInvalidEmail    = errors.New("invalid email format")
	ErrInvalidUsername = errors.New("invalid username format")
	ErrInvalidPassword = errors.New("invalid password")
	ErrTooShort        = errors.New("value too short")
	ErrTooLong         = errors.New("value too long")
)

var (
	emailRegex    = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,30}$`)
)

func ValidateEmail(email string) error {
	if !emailRegex.MatchString(email) {
		return ErrInvalidEmail
	}
	return nil
}

func ValidateUsername(username string) error {
	if len(username) < 3 {
		return ErrTooShort
	}
	if len(username) > 30 {
		return ErrTooLong
	}
	if !usernameRegex.MatchString(username) {
		return ErrInvalidUsername
	}
	return nil
}

func ValidatePassword(password string) error {
	if len(password) < 8 {
		return ErrTooShort
	}
	if len(password) > 128 {
		return ErrTooLong
	}
	return nil
}

func ValidateRoomName(name string) error {
	if len(name) < 1 {
		return ErrTooShort
	}
	if len(name) > 100 {
		return ErrTooLong
	}
	return nil
}

func ValidateMessageContent(content string) error {
	if len(content) > 4000 {
		return ErrTooLong
	}
	return nil
}

func SanitizeString(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}
