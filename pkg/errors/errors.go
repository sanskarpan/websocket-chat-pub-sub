package errors

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound           = errors.New("resource not found")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
	ErrBadRequest         = errors.New("bad request")
	ErrConflict           = errors.New("conflict")
	ErrInternalServer     = errors.New("internal server error")
	ErrServiceUnavailable = errors.New("service unavailable")
)

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *APIError) Unwrap() error {
	return e.Err
}

func NewAPIError(code, message string, err error) *APIError {
	return &APIError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

func NotFound(resource string) *APIError {
	return NewAPIError("NOT_FOUND", fmt.Sprintf("%s not found", resource), ErrNotFound)
}

func Unauthorized(message string) *APIError {
	return NewAPIError("UNAUTHORIZED", message, ErrUnauthorized)
}

func Forbidden(message string) *APIError {
	return NewAPIError("FORBIDDEN", message, ErrForbidden)
}

func BadRequest(message string) *APIError {
	return NewAPIError("BAD_REQUEST", message, ErrBadRequest)
}

func Conflict(resource string) *APIError {
	return NewAPIError("CONFLICT", fmt.Sprintf("%s already exists", resource), ErrConflict)
}

func InternalError(message string) *APIError {
	return NewAPIError("INTERNAL_ERROR", message, ErrInternalServer)
}
