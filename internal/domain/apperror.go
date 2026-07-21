package domain

import (
	"errors"
	"fmt"
	"net/http"
)

// AppError is the shared application error type for Clean Architecture layers.
// Usecase returns *AppError (or wraps sentinels); handlers map to HTTP once.
type AppError struct {
	Code       string // stable machine code (email_taken, invalid_credentials, …)
	Message    string // human-readable
	HTTPStatus int
	Err        error // optional wrapped cause (sentinels for errors.Is)
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *AppError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewAppError builds an AppError with an optional cause.
func NewAppError(httpStatus int, code, message string, cause error) *AppError {
	if httpStatus <= 0 {
		httpStatus = http.StatusInternalServerError
	}
	return &AppError{
		Code:       code,
		Message:    message,
		HTTPStatus: httpStatus,
		Err:        cause,
	}
}

func BadRequest(code, message string) *AppError {
	return NewAppError(http.StatusBadRequest, code, message, nil)
}

func Unauthorized(code, message string) *AppError {
	return NewAppError(http.StatusUnauthorized, code, message, nil)
}

func Forbidden(code, message string) *AppError {
	return NewAppError(http.StatusForbidden, code, message, nil)
}

func NotFound(code, message string) *AppError {
	return NewAppError(http.StatusNotFound, code, message, nil)
}

func Conflict(code, message string) *AppError {
	return NewAppError(http.StatusConflict, code, message, nil)
}

func Unavailable(code, message string) *AppError {
	return NewAppError(http.StatusServiceUnavailable, code, message, nil)
}

func Internal(code, message string, cause error) *AppError {
	return NewAppError(http.StatusInternalServerError, code, message, cause)
}

// Wrap attaches HTTP metadata around an existing sentinel / cause.
func Wrap(cause error, httpStatus int, code, message string) *AppError {
	if message == "" && cause != nil {
		message = cause.Error()
	}
	return NewAppError(httpStatus, code, message, cause)
}

// AsAppError extracts *AppError from an error chain.
func AsAppError(err error) (*AppError, bool) {
	var ae *AppError
	if errors.As(err, &ae) && ae != nil {
		return ae, true
	}
	return nil, false
}

// FormatDetail returns "code: message" for logs.
func (e *AppError) FormatDetail() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
