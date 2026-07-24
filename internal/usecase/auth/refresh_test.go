package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/token"
	"github.com/google/uuid"
)

func TestRefreshRotatesAndLogoutRevokes(t *testing.T) {
	tok, err := token.NewService(config.JWTConfig{
		Secret:     "test-secret-for-refresh-rotation-32b",
		AccessTTL:  time.Minute,
		RefreshTTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}

	repo := newMemAuthRepo()
	sessions := newMemSessions()
	uc := NewUsecase(repo, tok)
	uc.AttachSessions(sessions)

	reg, err := uc.Register(context.Background(), dto.RegisterRequest{
		Email:    "refresh@example.com",
		Password: "password1",
	})
	if err != nil {
		t.Fatal(err)
	}
	oldRefresh := reg.Tokens.RefreshToken
	if oldRefresh == "" {
		t.Fatal("missing refresh token")
	}

	next, err := uc.Refresh(context.Background(), oldRefresh)
	if err != nil {
		t.Fatal(err)
	}
	if next.Tokens.AccessToken == "" || next.Tokens.RefreshToken == "" {
		t.Fatalf("bad refresh result: %+v", next.Tokens)
	}
	if next.Tokens.RefreshToken == oldRefresh {
		t.Fatal("expected rotated refresh token")
	}

	// Old refresh must be rejected after rotation.
	if _, err := uc.Refresh(context.Background(), oldRefresh); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("want ErrInvalidRefresh for reused token, got %v", err)
	}

	uid := uuid.MustParse(next.User.ID)
	if err := uc.Logout(context.Background(), uid, next.Tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Refresh(context.Background(), next.Tokens.RefreshToken); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("want ErrInvalidRefresh after logout, got %v", err)
	}
}
