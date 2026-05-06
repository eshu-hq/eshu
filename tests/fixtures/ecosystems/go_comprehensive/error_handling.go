package comprehensive

import (
	"errors"
	"fmt"
)

// AppError is a custom error type.
type AppError struct {
	Code    int
	Message string
	Cause   error
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Cause
}

// Sentinel errors.
var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
)

// NewNotFoundError wraps ErrNotFound.
func NewNotFoundError(resource string) error {
	return &AppError{
		Code:    404,
		Message: fmt.Sprintf("%s not found", resource),
		Cause:   ErrNotFound,
	}
}

// ValidateInput demonstrates error wrapping.
func ValidateInput(input string) error {
	if input == "" {
		return fmt.Errorf("validation failed: %w", ErrNotFound)
	}
	return nil
}
