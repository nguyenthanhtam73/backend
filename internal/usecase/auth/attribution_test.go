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

	long := strings.Repeat("a", 250)
	var req dto.RegisterRequest
	raw := `{
		"email":"ada@example.com",
		"password":"password1",
		"attribution":{
			"utm_source":"` + long + `",
			"utm_medium":"ig<script>",
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
	if stored.UTMSource == nil || *stored.UTMSource != strings.Repeat("a", 200) {
		t.Fatalf("utm_source=%v", stored.UTMSource)
	}
	if stored.UTMMedium == nil || *stored.UTMMedium != "igscript" {
		t.Fatalf("utm_medium=%v", stored.UTMMedium)
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
	if stored.TTCLID == nil || *stored.TTCLID != "tt.clid~1" {
		t.Fatalf("ttclid=%v", stored.TTCLID)
	}
	for _, field := range []*string{stored.UTMSource, stored.UTMMedium, stored.UTMCampaign, stored.FBCLID, stored.TTCLID} {
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
	if still.UTMSource == nil || *still.UTMSource != strings.Repeat("a", 200) {
		t.Fatalf("first touch overwritten: %v", still.UTMSource)
	}
}
