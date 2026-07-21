package payment

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// SePay sandbox / production checkout hosts (form POST target).
const (
	sepayCheckoutSandbox    = "https://pay-sandbox.sepay.vn/v1/checkout/init"
	sepayCheckoutProduction = "https://pay.sepay.vn/v1/checkout/init"
)

// CheckoutInitURL returns the SePay form action URL for the given env.
func CheckoutInitURL(env string) string {
	if strings.EqualFold(strings.TrimSpace(env), "production") {
		return sepayCheckoutProduction
	}
	return sepayCheckoutSandbox
}

// signedFieldOrder matches SePay Payment Gateway form docs (PHP sample).
// Do not reorder — signature verification on SePay side depends on this order.
var signedFieldOrder = []string{
	"order_amount",
	"merchant",
	"currency",
	"operation",
	"order_description",
	"order_invoice_number",
	"customer_id",
	"payment_method",
	"success_url",
	"error_url",
	"cancel_url",
}

// SignFormFields builds HMAC-SHA256 (base64) over present fields in signedFieldOrder.
//
// Algorithm (SePay docs):
//  1. For each allowed field that exists: append "field=value"
//  2. Join with commas
//  3. HMAC-SHA256(secret, string) → raw bytes → base64
func SignFormFields(fields map[string]string, secretKey string) string {
	parts := make([]string, 0, len(signedFieldOrder))
	for _, key := range signedFieldOrder {
		val, ok := fields[key]
		if !ok {
			continue
		}
		parts = append(parts, key+"="+val)
	}
	mac := hmac.New(sha256.New, []byte(secretKey))
	_, _ = mac.Write([]byte(strings.Join(parts, ",")))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// BuildCheckoutFormFields creates the hidden form map for POST → checkout/init.
// order_amount is always a plain integer string (VND has no decimals in practice).
func BuildCheckoutFormFields(in CheckoutFormInput, merchantID, secretKey string) map[string]string {
	fields := map[string]string{
		"merchant":             merchantID,
		"currency":             "VND",
		"operation":            "PURCHASE",
		"order_amount":         strconv.FormatInt(in.AmountVND, 10),
		"order_invoice_number": in.InvoiceNumber,
		"order_description":    in.Description,
		"customer_id":          in.CustomerID,
	}
	if in.PaymentMethod != "" {
		fields["payment_method"] = in.PaymentMethod
	}
	if in.SuccessURL != "" {
		fields["success_url"] = in.SuccessURL
	}
	if in.ErrorURL != "" {
		fields["error_url"] = in.ErrorURL
	}
	if in.CancelURL != "" {
		fields["cancel_url"] = in.CancelURL
	}
	if in.CustomData != "" {
		// custom_data is NOT part of the signed field list — safe to attach.
		fields["custom_data"] = in.CustomData
	}
	fields["signature"] = SignFormFields(fields, secretKey)
	return fields
}

// CheckoutFormInput is the merchant-side payload before signing.
type CheckoutFormInput struct {
	AmountVND      int64
	InvoiceNumber  string
	Description    string
	CustomerID     string
	PaymentMethod  string // optional: CARD | BANK_TRANSFER | NAPAS_BANK_TRANSFER
	SuccessURL     string
	ErrorURL       string
	CancelURL      string
	CustomData     string
}

// VerifyIPNSecretKey compares X-Secret-Key header with merchant secret (constant-time).
func VerifyIPNSecretKey(headerValue, secretKey string) bool {
	got := strings.TrimSpace(headerValue)
	want := strings.TrimSpace(secretKey)
	if got == "" || want == "" {
		return false
	}
	return hmac.Equal([]byte(got), []byte(want))
}

// VerifyWebhookHMAC verifies SePay bank-style webhooks:
//   X-SePay-Signature: sha256={hex}
//   X-SePay-Timestamp: {unix}
// over "{timestamp}.{raw_body}".
// Optional — Payment Gateway IPN typically uses X-Secret-Key instead.
func VerifyWebhookHMAC(rawBody []byte, timestamp, signatureHeader, secretKey string) bool {
	sig := strings.TrimSpace(signatureHeader)
	sig = strings.TrimPrefix(sig, "sha256=")
	if timestamp == "" || sig == "" || secretKey == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secretKey))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(rawBody)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(sig)), []byte(strings.ToLower(expected)))
}

// ParseAmountVND parses SePay amount strings like "50000" or "50000.00".
func ParseAmountVND(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty amount")
	}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return n, nil
}
