// Package turnstile verifies Cloudflare Turnstile tokens (siteverify API).
package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const siteVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// ErrMissingToken means the client did not send a response token while verification is required.
var ErrMissingToken = errors.New("turnstile response token missing")

// ErrVerificationFailed means Cloudflare rejected the token.
var ErrVerificationFailed = errors.New("turnstile verification failed")

type siteVerifyPayload struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

// Verify calls Cloudflare Turnstile siteverify. Secret and responseToken must be non-empty.
func Verify(ctx context.Context, secret, responseToken, remoteIP string) error {
	secret = strings.TrimSpace(secret)
	responseToken = strings.TrimSpace(responseToken)
	if secret == "" {
		return fmt.Errorf("turnstile: %w", errors.New("secret not configured"))
	}
	if responseToken == "" {
		return ErrMissingToken
	}

	form := url.Values{}
	form.Set("secret", secret)
	form.Set("response", responseToken)
	if ip := strings.TrimSpace(remoteIP); ip != "" {
		form.Set("remoteip", ip)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, siteVerifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("turnstile siteverify request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("turnstile siteverify read body: %w", err)
	}

	var out siteVerifyPayload
	if err := json.Unmarshal(body, &out); err != nil {
		return fmt.Errorf("turnstile siteverify decode: %w", err)
	}
	if !out.Success {
		return fmt.Errorf("%w: %v", ErrVerificationFailed, out.ErrorCodes)
	}
	return nil
}
