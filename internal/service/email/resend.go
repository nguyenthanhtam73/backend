// Package email sends transactional mail via Resend (or no-ops when unconfigured).
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

var (
	// ErrNotConfigured means the ESP key or From address is missing.
	ErrNotConfigured = errors.New("email sender not configured")
	// ErrSendFailed means Resend rejected or failed the delivery.
	ErrSendFailed = errors.New("email send failed")
)

const resendAPIURL = "https://api.resend.com/emails"

// Message is one outbound email.
type Message struct {
	To          string
	Subject     string
	Text        string
	HTML        string
	Headers     map[string]string
	Tags        []string
	Unsubscribe string // List-Unsubscribe URL
}

// Sender delivers a single transactional email.
type Sender interface {
	Configured() bool
	Send(ctx context.Context, msg Message) error
}

// ResendClient POSTs to api.resend.com. Safe to construct with empty credentials
// — Send then returns ErrNotConfigured (API can boot without an ESP).
type ResendClient struct {
	apiKey     string
	from       string
	endpoint   string
	httpClient *http.Client
}

// NewResendClient builds a client. Empty apiKey/from → Configured() false.
func NewResendClient(apiKey, from string) *ResendClient {
	return &ResendClient{
		apiKey:   strings.TrimSpace(apiKey),
		from:     strings.TrimSpace(from),
		endpoint: resendAPIURL,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// Configured reports whether both API key and From are set.
func (c *ResendClient) Configured() bool {
	return c != nil && c.apiKey != "" && c.from != ""
}

type resendRequest struct {
	From    string            `json:"from"`
	To      []string          `json:"to"`
	Subject string            `json:"subject"`
	Text    string            `json:"text,omitempty"`
	HTML    string            `json:"html,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// Send delivers one message. No-ops with ErrNotConfigured when ESP is missing.
func (c *ResendClient) Send(ctx context.Context, msg Message) error {
	if !c.Configured() {
		slog.Info("email: skipped — ESP not configured (set RESEND_API_KEY and EMAIL_FROM)")
		return ErrNotConfigured
	}
	to := strings.TrimSpace(msg.To)
	if to == "" {
		return fmt.Errorf("%w: missing to", ErrSendFailed)
	}
	if strings.TrimSpace(msg.Subject) == "" || strings.TrimSpace(msg.Text) == "" {
		return fmt.Errorf("%w: subject and text required", ErrSendFailed)
	}

	headers := map[string]string{}
	for k, v := range msg.Headers {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			headers[k] = v
		}
	}
	if u := strings.TrimSpace(msg.Unsubscribe); u != "" {
		headers["List-Unsubscribe"] = "<" + u + ">"
		headers["List-Unsubscribe-Post"] = "List-Unsubscribe=One-Click"
	}

	body := resendRequest{
		From:    c.from,
		To:      []string{to},
		Subject: msg.Subject,
		Text:    msg.Text,
		HTML:    msg.HTML,
	}
	if len(headers) > 0 {
		body.Headers = headers
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal resend body: %w", err)
	}

	endpoint := c.endpoint
	if endpoint == "" {
		endpoint = resendAPIURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSendFailed, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("email: resend transport failed", "to", maskEmail(to), "err", err)
		return fmt.Errorf("%w: %v", ErrSendFailed, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		slog.Info("email: sent", "to", maskEmail(to), "subject", msg.Subject, "status", resp.StatusCode)
		return nil
	}
	slog.Error("email: resend rejected",
		"to", maskEmail(to),
		"status", resp.StatusCode,
		"body", string(respBody),
	)
	return fmt.Errorf("%w: HTTP %d", ErrSendFailed, resp.StatusCode)
}

func maskEmail(addr string) string {
	at := strings.Index(addr, "@")
	if at <= 1 {
		return "***"
	}
	return addr[:1] + "***" + addr[at:]
}
