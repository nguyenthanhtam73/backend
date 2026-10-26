package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/token"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const sessionTestSecret = "test-secret-for-android-session-ttl"

func newSessionUsecase(t *testing.T) (*Service, *memSessions, *token.Service) {
	t.Helper()
	tok, err := token.NewService(config.JWTConfig{
		Secret:        sessionTestSecret,
		AccessTTL:     24 * time.Hour,
		RefreshTTL:    7 * 24 * time.Hour,
		AppRefreshTTL: 90 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessions := newMemSessions()
	uc := NewUsecase(newMemAuthRepo(), tok)
	uc.AttachSessions(sessions)
	return uc, sessions, tok
}

func registerClient(t *testing.T, uc *Service, email, client string) Result {
	t.Helper()
	res, err := uc.Register(context.Background(), dto.RegisterRequest{
		Email:    email,
		Password: "password1",
		Client:   client,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func refreshExpiry(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := jwt.ParseWithClaims(raw, &jwt.RegisteredClaims{}, func(tok *jwt.Token) (any, error) {
		if tok.Method != jwt.SigningMethodHS256 {
			t.Fatalf("alg %v", tok.Header["alg"])
		}
		return []byte(sessionTestSecret), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || claims.ExpiresAt == nil {
		t.Fatal("missing exp")
	}
	return claims.ExpiresAt.Time.UTC()
}

func assertExpiryNear(t *testing.T, got, want time.Time) {
	t.Helper()
	delta := got.Sub(want)
	if delta < 0 {
		delta = -delta
	}
	if delta > 2*time.Second {
		t.Fatalf("exp %s, want about %s (delta %s)", got, want, delta)
	}
}

func sessionByRefresh(t *testing.T, sessions *memSessions, tok *token.Service, raw string) *domain.RefreshSession {
	t.Helper()
	_, jti, err := tok.ParseRefreshToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.GetByID(context.Background(), jti)
	if err != nil || sess == nil {
		t.Fatalf("session: %v", err)
	}
	return sess
}

func TestAndroidLoginRefreshTTL(t *testing.T) {
	uc, sessions, tok := newSessionUsecase(t)
	before := time.Now().UTC()
	res := registerClient(t, uc, "android@example.com", domain.RefreshClientAndroid)
	if res.Tokens.ExpiresIn != int64((24 * time.Hour).Seconds()) {
		t.Fatalf("access expires_in=%d", res.Tokens.ExpiresIn)
	}
	exp := refreshExpiry(t, res.Tokens.RefreshToken)
	assertExpiryNear(t, exp, before.Add(90*24*time.Hour))
	sess := sessionByRefresh(t, sessions, tok, res.Tokens.RefreshToken)
	if sess.ClientKind != domain.RefreshClientAndroid {
		t.Fatalf("client=%q", sess.ClientKind)
	}
	if !sess.ExpiresAt.Equal(exp) {
		t.Fatalf("session expires %s, jwt exp %s", sess.ExpiresAt, exp)
	}

	web := registerClient(t, uc, "web@example.com", "")
	webExp := refreshExpiry(t, web.Tokens.RefreshToken)
	assertExpiryNear(t, webExp, before.Add(7*24*time.Hour))
	webSess := sessionByRefresh(t, sessions, tok, web.Tokens.RefreshToken)
	if webSess.ClientKind != domain.RefreshClientWeb {
		t.Fatalf("web client=%q", webSess.ClientKind)
	}
	if web.Tokens.ExpiresIn != res.Tokens.ExpiresIn {
		t.Fatalf("access ttl changed: android %d web %d", res.Tokens.ExpiresIn, web.Tokens.ExpiresIn)
	}

	for i, client := range []string{"ios", "Android", "ANDROID", "web"} {
		other := registerClient(t, uc, "other"+string(rune('a'+i))+"@example.com", client)
		got := refreshExpiry(t, other.Tokens.RefreshToken)
		assertExpiryNear(t, got, before.Add(7*24*time.Hour))
		sess := sessionByRefresh(t, sessions, tok, other.Tokens.RefreshToken)
		if sess.ClientKind != domain.RefreshClientWeb {
			t.Fatalf("client %q stored as %q", client, sess.ClientKind)
		}
	}
}

func TestRefreshKeepsAndroidClientKind(t *testing.T) {
	uc, sessions, tok := newSessionUsecase(t)
	res := registerClient(t, uc, "slide@example.com", domain.RefreshClientAndroid)
	before := time.Now().UTC()
	// Hint says web. The stored android kind still wins, and the 90-day window slides.
	next, err := uc.Refresh(context.Background(), res.Tokens.RefreshToken, domain.RefreshClientWeb)
	if err != nil {
		t.Fatal(err)
	}
	if next.Tokens.RefreshToken == res.Tokens.RefreshToken {
		t.Fatal("expected a rotated refresh token")
	}
	exp := refreshExpiry(t, next.Tokens.RefreshToken)
	assertExpiryNear(t, exp, before.Add(90*24*time.Hour))
	sess := sessionByRefresh(t, sessions, tok, next.Tokens.RefreshToken)
	if sess.ClientKind != domain.RefreshClientAndroid {
		t.Fatalf("rotated client=%q", sess.ClientKind)
	}
	if !sess.ExpiresAt.Equal(exp) {
		t.Fatalf("session expires %s, jwt exp %s", sess.ExpiresAt, exp)
	}
	old := sessionByRefresh(t, sessions, tok, res.Tokens.RefreshToken)
	if old.RevokedAt == nil {
		t.Fatal("presented session should be revoked after rotation")
	}
}

func TestLogoutWithRefreshTokenRevokesOnlyThatSession(t *testing.T) {
	uc, _, _ := newSessionUsecase(t)
	web := registerClient(t, uc, "both@example.com", "")
	phone, err := uc.Login(context.Background(), dto.LoginRequest{
		Email:    "both@example.com",
		Password: "password1",
		Client:   domain.RefreshClientAndroid,
	})
	if err != nil {
		t.Fatal(err)
	}
	uid := uuid.MustParse(web.User.ID)
	if err := uc.Logout(context.Background(), uid, phone.Tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Refresh(context.Background(), phone.Tokens.RefreshToken, ""); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("phone session should be revoked, got %v", err)
	}
	if _, err := uc.Refresh(context.Background(), web.Tokens.RefreshToken, ""); err != nil {
		t.Fatalf("web session should still refresh, got %v", err)
	}
}

func TestLogoutWithoutRefreshTokenRevokesAll(t *testing.T) {
	uc, _, _ := newSessionUsecase(t)
	web := registerClient(t, uc, "all@example.com", "")
	phone, err := uc.Login(context.Background(), dto.LoginRequest{
		Email:    "all@example.com",
		Password: "password1",
		Client:   domain.RefreshClientAndroid,
	})
	if err != nil {
		t.Fatal(err)
	}
	uid := uuid.MustParse(web.User.ID)
	if err := uc.Logout(context.Background(), uid, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Refresh(context.Background(), web.Tokens.RefreshToken, ""); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("web session should be revoked, got %v", err)
	}
	if _, err := uc.Refresh(context.Background(), phone.Tokens.RefreshToken, ""); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("phone session should be revoked, got %v", err)
	}
}

func TestLogoutAllRevokesEverySession(t *testing.T) {
	uc, _, _ := newSessionUsecase(t)
	web := registerClient(t, uc, "everywhere@example.com", "")
	phone, err := uc.Login(context.Background(), dto.LoginRequest{
		Email:    "everywhere@example.com",
		Password: "password1",
		Client:   domain.RefreshClientAndroid,
	})
	if err != nil {
		t.Fatal(err)
	}
	uid := uuid.MustParse(web.User.ID)
	if err := uc.LogoutAll(context.Background(), uid); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Refresh(context.Background(), web.Tokens.RefreshToken, ""); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("web session should be revoked, got %v", err)
	}
	if _, err := uc.Refresh(context.Background(), phone.Tokens.RefreshToken, ""); !errors.Is(err, ErrInvalidRefresh) {
		t.Fatalf("phone session should be revoked, got %v", err)
	}
}

func TestLogoutIgnoresRefreshTokenFromAnotherUser(t *testing.T) {
	uc, _, _ := newSessionUsecase(t)
	alice := registerClient(t, uc, "alice-device@example.com", domain.RefreshClientAndroid)
	bob := registerClient(t, uc, "bob-device@example.com", "")
	aliceID := uuid.MustParse(alice.User.ID)
	if err := uc.Logout(context.Background(), aliceID, bob.Tokens.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if err := uc.Logout(context.Background(), aliceID, "not-a-refresh-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.Refresh(context.Background(), bob.Tokens.RefreshToken, ""); err != nil {
		t.Fatalf("bob session should still refresh, got %v", err)
	}
	if _, err := uc.Refresh(context.Background(), alice.Tokens.RefreshToken, ""); err != nil {
		t.Fatalf("alice session should still refresh, got %v", err)
	}
}
