package email

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Failure is a Resend or transport error from Send.
// Status is the HTTP code, or 0 when the request never got a response.
type Failure struct {
	Status int
	cause  error
}

func (f *Failure) Error() string {
	if f == nil {
		return ErrSendFailed.Error()
	}
	switch {
	case f.Status > 0 && f.cause != nil:
		return fmt.Sprintf("%s: HTTP %d: %v", ErrSendFailed, f.Status, f.cause)
	case f.Status > 0:
		return fmt.Sprintf("%s: HTTP %d", ErrSendFailed, f.Status)
	case f.cause != nil:
		return fmt.Sprintf("%s: %v", ErrSendFailed, f.cause)
	default:
		return ErrSendFailed.Error()
	}
}

func (f *Failure) Unwrap() error { return ErrSendFailed }

// StatusOf returns the HTTP status attached to err, or 0.
func StatusOf(err error) int {
	var f *Failure
	if errors.As(err, &f) && f != nil {
		return f.Status
	}
	return 0
}

// FailureClass says whether reminder delivery should retry an address.
type FailureClass int

const (
	// FailureTransient is a network error, HTTP 5xx, 408, 429, or an auth/config
	// error (401/403). The same address is retried on a later run.
	FailureTransient FailureClass = iota
	// FailureRejection is some other HTTP 4xx. Several of these suppress the address.
	FailureRejection
	// FailurePermanent is a validation rejection (HTTP 400 or 422). Retrying
	// the same address will not succeed.
	FailurePermanent
)

// Classify reports how reminder delivery should treat err.
// Errors that are not a Failure stay transient, so an unexpected failure
// still retries.
func Classify(err error) FailureClass {
	if err == nil || errors.Is(err, ErrNotConfigured) {
		return FailureTransient
	}
	var f *Failure
	if !errors.As(err, &f) || f == nil {
		return FailureTransient
	}
	switch f.Status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return FailurePermanent
	case http.StatusUnauthorized, http.StatusForbidden,
		http.StatusRequestTimeout, http.StatusTooManyRequests:
		return FailureTransient
	default:
		if f.Status >= 400 && f.Status < 500 {
			return FailureRejection
		}
		return FailureTransient
	}
}

// AddressHash is a stable key for a mailbox. It is not reversible.
func AddressHash(addr string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(addr))))
	return hex.EncodeToString(sum[:])
}
