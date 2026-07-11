// Package httpx holds shared HTTP helpers for outbound AI provider calls:
// a typed status error, a retry-aware classifier, a single-attempt JSON POST,
// and a generic retry wrapper. Keeping these here (rather than in each provider
// client) lets every AI call share one consistent retry policy.
package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dadiary/backend/pkg/retry"
)

// StatusError represents a non-2xx HTTP response from an AI provider. It keeps
// the status code and (trimmed) body so callers can log context and the retry
// classifier can decide whether the failure is transient.
type StatusError struct {
	Provider string // human label, e.g. "openai chat", "anthropic messages"
	Status   int    // HTTP status code
	Body     string // trimmed response body (for logs / classification)
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s http %d: %s", e.Provider, e.Status, e.Body)
}

// RetryableError reports whether this HTTP failure is worth retrying.
//
// Retryable:   429 (rate limit), 408 (request timeout), 5xx (server error).
// NOT retried: 400 (bad request), 401/403 (auth), other 4xx, and a 429 that
//
//	the provider tags as quota/billing exhaustion (retrying won't help).
func (e *StatusError) RetryableError() bool {
	switch {
	case e.Status == 429:
		low := strings.ToLower(e.Body)
		// OpenAI signals a permanently-out-of-credit account with
		// "insufficient_quota"; retrying that just burns time.
		if strings.Contains(low, "insufficient_quota") || strings.Contains(low, "billing_hard_limit") {
			return false
		}
		return true
	case e.Status == 408:
		return true
	case e.Status >= 500 && e.Status <= 599:
		return true
	default:
		return false
	}
}

// retryable is implemented by errors that can classify themselves.
type retryable interface{ RetryableError() bool }

// IsRetryable is the shared classifier for AI/HTTP calls.
//
// Order matters: a cancelled/expired *parent* context is never retryable — the
// caller is gone (or the deadline passed) so more attempts are pointless. Note
// this also treats an http.Client.Timeout (implemented via a context deadline)
// as non-retryable, which is intentional: a fresh attempt on the same client
// would race the same expired budget. A raw dial/connection timeout, by
// contrast, surfaces as a net.Error and IS retried.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Typed provider errors classify themselves (status-code aware).
	var re retryable
	if errors.As(err, &re) {
		return re.RetryableError()
	}

	// Transport-level failures: connection reset/refused, dial timeout, DNS,
	// EOF mid-response, etc. These are the classic transient network errors.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}
	return false
}

// PostJSON performs a single POST attempt with the given JSON payload and
// headers, returning the raw response body. A non-2xx response is returned as
// *StatusError (never a nil error), so IsRetryable can classify it. Transport
// errors are returned as-is (already implement net.Error).
func PostJSON(ctx context.Context, client *http.Client, provider, endpoint string, headers map[string]string, payload []byte) ([]byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{Provider: provider, Status: resp.StatusCode, Body: strings.TrimSpace(string(b))}
	}
	return b, nil
}

// WithRetry runs fn with exponential backoff + jitter using the shared
// HTTP-aware classifier. It is the single funnel through which all AI provider
// calls obtain retry behaviour.
func WithRetry[T any](ctx context.Context, cfg retry.Config, op string, fn func(context.Context) (T, error)) (T, error) {
	return retry.DoValue(ctx, cfg, op, IsRetryable, fn)
}
