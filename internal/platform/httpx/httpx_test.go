package httpx

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestStatusError_RetryableError(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   bool
	}{
		{429, "rate limit reached", true},
		{429, `{"error":{"type":"insufficient_quota"}}`, false}, // out of credit — permanent
		{408, "request timeout", true},
		{500, "internal error", true},
		{503, "overloaded", true},
		{400, "bad request", false},
		{401, "unauthorized", false},
		{403, "forbidden", false},
		{404, "not found", false},
	}
	for _, c := range cases {
		e := &StatusError{Provider: "test", Status: c.status, Body: c.body}
		if got := e.RetryableError(); got != c.want {
			t.Errorf("status %d body %q: RetryableError()=%v, want %v", c.status, c.body, got, c.want)
		}
	}
}

func TestIsRetryable(t *testing.T) {
	if IsRetryable(nil) {
		t.Error("nil error should not be retryable")
	}
	if IsRetryable(context.Canceled) {
		t.Error("context.Canceled should not be retryable")
	}
	if IsRetryable(context.DeadlineExceeded) {
		t.Error("context.DeadlineExceeded should not be retryable")
	}
	if !IsRetryable(&StatusError{Status: 503}) {
		t.Error("5xx StatusError should be retryable")
	}
	if IsRetryable(&StatusError{Status: 401}) {
		t.Error("401 StatusError should not be retryable")
	}
	// A plain domain error (e.g. moderation rejection) is not retryable.
	if IsRetryable(errors.New("content failed moderation checks")) {
		t.Error("plain domain error should not be retryable")
	}
	// Wrapped StatusError is still classified via errors.As.
	wrapped := fmt.Errorf("call failed: %w", &StatusError{Status: 429, Body: "slow down"})
	if !IsRetryable(wrapped) {
		t.Error("wrapped 429 should be retryable")
	}
}
