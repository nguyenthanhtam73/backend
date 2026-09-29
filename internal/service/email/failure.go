package email

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Failure is a Resend or transport error from Send.
// Status is the HTTP code, or 0 when the request never got a response.
// Body is the raw Resend JSON, when there was a response. Error() omits it
// so a mailbox echoed by the provider is not copied into logs via the error text.
type Failure struct {
	Status int
	Body   string
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
	// FailureRejection is an HTTP 4xx that is not a confirmed bad recipient.
	// Several of these suppress the address. HTTP 400/422 count here unless
	// the body says the recipient address itself is invalid.
	FailureRejection
	// FailurePermanent means Resend reported the recipient (`to`) address as
	// invalid. Retrying that address will not succeed.
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
		if recipientAddressRejected(f.Body) {
			return FailurePermanent
		}
		return FailureRejection
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

// Resend reports a bad recipient as validation_error whose message contains
// "Invalid `to` field". invalid_from_address and any message that blames
// `from` are our payload, not the mailbox.
var (
	invalidRecipientField = regexp.MustCompile("invalid\\s+`to`|invalid\\s+to\\s+field")
	invalidSenderField    = regexp.MustCompile("invalid\\s+`from`|invalid\\s+from\\s+field")
	mailboxInMessage      = regexp.MustCompile(`[^\s<>"']+@[^\s<>"']+`)
)

type resendErrorBody struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func parseResendBody(body string) (name, message string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", ""
	}
	var parsed resendErrorBody
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return "", ""
	}
	return strings.TrimSpace(parsed.Name), strings.TrimSpace(parsed.Message)
}

// recipientAddressRejected is true only when the body clearly names the
// recipient field. A bare 400/422, a bad `from`, or a missing field is not enough.
func recipientAddressRejected(body string) bool {
	name, message := parseResendBody(body)
	name = strings.ToLower(name)
	msg := strings.ToLower(message)
	if name == "invalid_from_address" || invalidSenderField.MatchString(msg) {
		return false
	}
	if name == "invalid_to_address" {
		return true
	}
	return invalidRecipientField.MatchString(msg)
}

// ResendName is the `name` field from a Resend error body, or empty.
func ResendName(err error) string {
	var f *Failure
	if !errors.As(err, &f) || f == nil {
		return ""
	}
	name, _ := parseResendBody(f.Body)
	return name
}

// FailureKey groups equivalent rejections. Mailboxes are stripped so one
// sender error shared by many recipients stays a single key.
func FailureKey(err error) string {
	var f *Failure
	if !errors.As(err, &f) || f == nil {
		return "0"
	}
	name, message := parseResendBody(f.Body)
	message = strings.ToLower(message)
	message = mailboxInMessage.ReplaceAllString(message, "")
	message = strings.Join(strings.Fields(message), " ")
	return fmt.Sprintf("%d\n%s\n%s", f.Status, strings.ToLower(name), message)
}

// AddressHash is a stable key for a mailbox. It is not reversible.
func AddressHash(addr string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(addr))))
	return hex.EncodeToString(sum[:])
}
