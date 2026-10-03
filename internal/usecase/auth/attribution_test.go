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
	longSource := strings.Repeat("a", 150)
	raw := `{
		"email":"ada@example.com",
		"password":"password1",
		"attribution":{
			"utm_source":"` + longSource + `",
			"utm_medium":"paid social",
			"utm_campaign":"launch",
			"utm_content":"person@example.com",
			"fbclid":"` + strings.Repeat("c", 256) + `",
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
	if stored.UTMSource == nil || *stored.UTMSource != strings.Repeat("a", 100) {
		t.Fatalf("utm_source=%v", stored.UTMSource)
	}
	if stored.UTMMedium == nil || *stored.UTMMedium != "paid social" {
		t.Fatalf("utm_medium=%v", stored.UTMMedium)
	}
	if stored.UTMCampaign == nil || *stored.UTMCampaign != "launch" {
		t.Fatalf("utm_campaign=%v", stored.UTMCampaign)
	}
	if stored.UTMContent != nil {
		t.Fatalf("email-like utm_content stored: %s", *stored.UTMContent)
	}
	if stored.FBCLID == nil || *stored.FBCLID != strings.Repeat("c", 256) {
		t.Fatalf("fbclid=%v", stored.FBCLID)
	}
	// '~' is not in the click-id alphabet, so it is stripped and the rest is kept.
	if stored.TTCLID == nil || *stored.TTCLID != "tt.clid1" {
		t.Fatalf("ttclid=%v", stored.TTCLID)
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
	if still.UTMSource == nil || *still.UTMSource != strings.Repeat("a", 100) {
		t.Fatalf("first touch utm_source overwritten: %v", still.UTMSource)
	}
	if still.UTMCampaign == nil || *still.UTMCampaign != "launch" {
		t.Fatalf("first touch overwritten: %v", still.UTMCampaign)
	}
	if still.FBCLID == nil || *still.FBCLID != strings.Repeat("c", 256) {
		t.Fatalf("fbclid overwritten: %v", still.FBCLID)
	}
}
