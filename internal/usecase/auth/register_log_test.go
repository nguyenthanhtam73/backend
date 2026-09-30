package auth

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestRegisterLogOmitsRawEmail(t *testing.T) {
	const raw = "Thao.Nguyen+ads@gmail.com"
	const normalized = "thao.nguyen+ads@gmail.com"

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	uc := NewUsecase(newMemAuthRepo(), &stubTokens{})
	res, err := uc.Register(context.Background(), dto.RegisterRequest{
		Email:    raw,
		Password: "password1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.User.Email != normalized {
		t.Fatalf("stored email = %q, want the full normalized address", res.User.Email)
	}

	logged := buf.String()
	if !strings.Contains(logged, "auth: registered") {
		t.Fatalf("missing register log line:\n%s", logged)
	}
	for _, leak := range []string{raw, normalized, "thao.nguyen", "password1", "+ads"} {
		if strings.Contains(logged, leak) {
			t.Fatalf("register log contains %q:\n%s", leak, logged)
		}
	}
	if !strings.Contains(logged, "t***@gmail.com") {
		t.Fatalf("register log missing masked email:\n%s", logged)
	}
}
