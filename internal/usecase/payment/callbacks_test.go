package payment

import (
	"testing"

	"github.com/dadiary/backend/internal/config"
)

func TestLocalizeCallbackURLs_VI(t *testing.T) {
	cfg := config.SePayConfig{PublicWebURL: "https://dadiary.vn"}
	s, e, c := localizeCallbackURLs(cfg, "vi")
	if s != "https://dadiary.vn/payment/success" {
		t.Fatalf("success: %s", s)
	}
	if e != "https://dadiary.vn/payment/error" {
		t.Fatalf("error: %s", e)
	}
	if c != "https://dadiary.vn/payment/cancel" {
		t.Fatalf("cancel: %s", c)
	}
}

func TestLocalizeCallbackURLs_EN(t *testing.T) {
	cfg := config.SePayConfig{PublicWebURL: "https://dadiary.vn"}
	s, e, c := localizeCallbackURLs(cfg, "en")
	if s != "https://dadiary.vn/en/payment/success" {
		t.Fatalf("success: %s", s)
	}
	if e != "https://dadiary.vn/en/payment/error" {
		t.Fatalf("error: %s", e)
	}
	if c != "https://dadiary.vn/en/payment/cancel" {
		t.Fatalf("cancel: %s", c)
	}
}

func TestWithLocalePrefix_Rewrite(t *testing.T) {
	got := withLocalePrefix("https://dadiary.vn/payment/success", "en")
	if got != "https://dadiary.vn/en/payment/success" {
		t.Fatalf("got %s", got)
	}
	got = withLocalePrefix("https://dadiary.vn/en/payment/success", "vi")
	if got != "https://dadiary.vn/payment/success" {
		t.Fatalf("got %s", got)
	}
}
