package payment

import (
	"testing"
)

func TestSignFormFields_MatchesDocExampleShape(t *testing.T) {
	fields := map[string]string{
		"order_amount":         "100000",
		"merchant":             "MERCHANT_123",
		"currency":             "VND",
		"operation":            "PURCHASE",
		"order_description":    "Payment for order #12345",
		"order_invoice_number": "INV_20231201_001",
		"success_url":          "https://yoursite.com/payment/success",
		"error_url":            "https://yoursite.com/payment/error",
		"cancel_url":           "https://yoursite.com/payment/cancel",
	}
	sig := SignFormFields(fields, "test-secret")
	if sig == "" {
		t.Fatal("expected non-empty signature")
	}
	// Stable: same inputs → same signature.
	sig2 := SignFormFields(fields, "test-secret")
	if sig != sig2 {
		t.Fatalf("signature not stable: %q vs %q", sig, sig2)
	}
	// Different secret → different signature.
	if SignFormFields(fields, "other") == sig {
		t.Fatal("expected different signature for different secret")
	}
}

func TestBuildCheckoutFormFields_IncludesSignature(t *testing.T) {
	out := BuildCheckoutFormFields(CheckoutFormInput{
		AmountVND:     99000,
		InvoiceNumber: "DD-TEST-1",
		Description:   "DaDiary premium (monthly)",
		CustomerID:    "user-1",
		SuccessURL:    "https://dadiary.vn/pricing?paid=1",
		CustomData:    `{"plan_tier":"premium"}`,
	}, "SP-TEST", "spsk_test")

	if out["signature"] == "" {
		t.Fatal("missing signature")
	}
	if out["merchant"] != "SP-TEST" {
		t.Fatalf("merchant: %q", out["merchant"])
	}
	if out["order_amount"] != "99000" {
		t.Fatalf("order_amount: %q", out["order_amount"])
	}
	if out["custom_data"] == "" {
		t.Fatal("expected custom_data passthrough")
	}
	// custom_data must not break re-sign of signed fields.
	resign := SignFormFields(out, "spsk_test")
	if resign != out["signature"] {
		t.Fatal("signature should ignore custom_data (not in signed list)")
	}
}

func TestCheckoutInitURL(t *testing.T) {
	if got := CheckoutInitURL("sandbox"); got != sepayCheckoutSandbox {
		t.Fatalf("sandbox: %s", got)
	}
	if got := CheckoutInitURL("production"); got != sepayCheckoutProduction {
		t.Fatalf("production: %s", got)
	}
}

func TestParseAmountVND(t *testing.T) {
	n, err := ParseAmountVND("50000.00")
	if err != nil || n != 50000 {
		t.Fatalf("got %d %v", n, err)
	}
}

func TestVerifyIPNSecretKey(t *testing.T) {
	if !VerifyIPNSecretKey("abc", "abc") {
		t.Fatal("expected match")
	}
	if VerifyIPNSecretKey("abc", "xyz") {
		t.Fatal("expected mismatch")
	}
}

func TestAmountForPlan(t *testing.T) {
	n, err := AmountForPlan("premium", "monthly")
	if err != nil || n != 99_000 {
		t.Fatalf("premium monthly: %d %v", n, err)
	}
	n, err = AmountForPlan("premium_plus", "yearly")
	if err != nil || n != 1_369_000 {
		t.Fatalf("plus yearly: %d %v", n, err)
	}
	if _, err := AmountForPlan("free", "monthly"); err == nil {
		t.Fatal("expected error for free")
	}
}
