package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestRegister_PersistsFirstTouchAttribution(t *testing.T) {
	repo := newMemAuthRepo()
	uc := NewUsecase(repo, &stubTokens{})
	uc.AttachSessions(newMemSessions())

	var req dto.RegisterRequest
	raw := `{
		"email":"ada@example.com",
		"password":"password1",
		"attribution":{
			"utm_source":"` + strings.Repeat("a", 101) + `",
			"utm_medium":"paid social",
			"utm_campaign":"launch",
			"utm_content":"person@example.com",
			"fbclid":"IwAR0abc_def-123",
			"ttclid":"tt.clid~1",
			"email":"hidden@example.com"
		}
	}`
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Register(context.Background(), req); err != nil {
		t.Fatal(err)
	}

	stored, err := repo.GetByEmail(context.Background(), "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Email != "ada@example.com" {
		t.Fatalf("email=%s", stored.Email)
	}
	if stored.UTMSource != nil {
		t.Fatalf("101-char utm_source stored: %s", *stored.UTMSource)
	}
	if stored.UTMMedium != nil {
		t.Fatalf("spaced utm_medium stored: %s", *stored.UTMMedium)
	}
	if stored.UTMCampaign == nil || *stored.UTMCampaign != "launch" {
		t.Fatalf("utm_campaign=%v", stored.UTMCampaign)
	}
	if stored.UTMContent != nil {
		t.Fatalf("email-like utm_content stored: %s", *stored.UTMContent)
	}
	if stored.FBCLID == nil || *stored.FBCLID != "IwAR0abc_def-123" {
		t.Fatalf("fbclid=%v", stored.FBCLID)
	}
	if stored.TTCLID != nil {
		t.Fatalf("ttclid with ~ stored: %s", *stored.TTCLID)
	}
	for _, field := range []*string{stored.UTMCampaign, stored.FBCLID} {
		if field != nil && strings.Contains(*field, "@") {
			t.Fatalf("attribution stored an email: %s", *field)
		}
	}

	var again dto.RegisterRequest
	if err := json.Unmarshal([]byte(`{"email":"ada@example.com","password":"password1","attribution":{"utm_source":"other"}}`), &again); err != nil {
		t.Fatal(err)
	}
	_, err = uc.Register(context.Background(), again)
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("second register err=%v", err)
	}
	still, err := repo.GetByEmail(context.Background(), "ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if still.UTMSource != nil {
		t.Fatalf("dropped utm_source written on retry: %v", still.UTMSource)
	}
	if still.UTMCampaign == nil || *still.UTMCampaign != "launch" {
		t.Fatalf("first touch overwritten: %v", still.UTMCampaign)
	}
	if still.FBCLID == nil || *still.FBCLID != "IwAR0abc_def-123" {
		t.Fatalf("fbclid overwritten: %v", still.FBCLID)
	}
}
