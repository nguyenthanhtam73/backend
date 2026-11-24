package push

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSendToSubscription_VAPIDJWTSubIsSingleMailto(t *testing.T) {
	p256, auth := testPushKeys(t)
	cases := []struct {
		subject string
		want    string
	}{
		{subject: "mailto:x@y", want: "mailto:x@y"},
		{subject: "x@y", want: "mailto:x@y"},
		{subject: "https://dadiary.vn", want: "https://dadiary.vn"},
		{subject: "noreply@dadiary.app", want: "mailto:noreply@dadiary.app"},
	}
	for _, tc := range cases {
		t.Run(tc.subject, func(t *testing.T) {
			var authz string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authz = r.Header.Get("Authorization")
				w.WriteHeader(http.StatusCreated)
			}))
			t.Cleanup(srv.Close)

			sender := testSender(t, tc.subject, repository.NewPushSubscriptionRepository(nil))
			err := sender.SendToSubscription(context.Background(), &domain.PushSubscription{
				ID:       uuid.New(),
				UserID:   uuid.New(),
				Endpoint: srv.URL + "/device",
				P256dh:   p256,
				Auth:     auth,
				IsActive: true,
			}, NotificationPayload{Title: "Nhắc", Body: "Check-in"})
			if err != nil {
				t.Fatal(err)
			}
			got := vapidJWTSub(t, authz)
			if got != tc.want {
				t.Fatalf("sub=%q want %q", got, tc.want)
			}
			if strings.HasPrefix(tc.want, "mailto:") && strings.Count(got, "mailto:") != 1 {
				t.Fatalf("sub=%q want exactly one mailto:", got)
			}
			if strings.HasPrefix(tc.want, "https:") && strings.Contains(got, "mailto:") {
				t.Fatalf("https subject signed as %q", got)
			}
		})
	}
}

func TestSendToSubscription_RejectLogIsEndpointDomainOnly(t *testing.T) {
	const token = "apple-device-token-do-not-log"
	p256, auth := testPushKeys(t)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"reason":"BadJwtToken","echo":"eyJhbGciOiJFUzI1NiJ9.aaaa.bbbb"}`)
	}))
	t.Cleanup(srv.Close)

	sender := testSender(t, "mailto:noreply@dadiary.app", repository.NewPushSubscriptionRepository(nil))
	endpoint := srv.URL + "/" + token
	err := sender.SendToSubscription(context.Background(), &domain.PushSubscription{
		ID:       uuid.New(),
		UserID:   uuid.New(),
		Endpoint: endpoint,
		P256dh:   p256,
		Auth:     auth,
		IsActive: true,
	}, NotificationPayload{Title: "Nhắc", Body: "Check-in"})
	if !errors.Is(err, ErrVAPIDAuthFailed) {
		t.Fatalf("err=%v", err)
	}

	logged := buf.String()
	host := endpointDomain(endpoint)
	if !strings.Contains(logged, host) {
		t.Fatalf("log missing domain %s: %s", host, scrubPushLog(logged, token, auth, p256, endpoint))
	}
	if !strings.Contains(logged, "BadJwtToken") {
		t.Fatalf("log missing provider reason: %s", scrubPushLog(logged, token, auth, p256, endpoint))
	}
	for _, secret := range []string{token, auth, p256, endpoint, srv.URL, "eyJhbGciOiJFUzI1NiJ9"} {
		if secret != "" && strings.Contains(logged, secret) {
			t.Fatalf("reject log contains %q: %s", secret, scrubPushLog(logged, token, auth, p256, endpoint))
		}
	}
	if endpointDomain("https://web.push.apple.com/"+token) != "web.push.apple.com" {
		t.Fatal("apple host")
	}
}

func TestSendToSubscription_VAPIDAuthNotRetriedSameDay(t *testing.T) {
	p256, auth := testPushKeys(t)
	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/apple"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"reason":"BadJwtToken"}`))
		case strings.HasSuffix(r.URL.Path, "/ok"):
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)

	sender := testSender(t, "mailto:x@y", repository.NewPushSubscriptionRepository(nil))
	ctx := context.Background()
	payload := NotificationPayload{Title: "Nhắc", Body: "Check-in"}
	sub := func(path string) *domain.PushSubscription {
		return &domain.PushSubscription{
			ID:       uuid.New(),
			UserID:   uuid.New(),
			Endpoint: srv.URL + path,
			P256dh:   p256,
			Auth:     auth,
			IsActive: true,
		}
	}
	apple := sub("/apple")
	other := sub("/other")
	okSub := sub("/ok")

	if err := sender.SendToSubscription(ctx, apple, payload); !errors.Is(err, ErrVAPIDAuthFailed) {
		t.Fatalf("apple: %v", err)
	}
	if err := sender.SendToSubscription(ctx, apple, payload); !errors.Is(err, ErrVAPIDAuthFailed) {
		t.Fatalf("apple retry: %v", err)
	}
	if err := sender.SendToSubscription(ctx, other, payload); err == nil || errors.Is(err, ErrVAPIDAuthFailed) || errors.Is(err, ErrSubscriptionExpired) {
		t.Fatalf("other: %v", err)
	}
	if err := sender.SendToSubscription(ctx, other, payload); err == nil || errors.Is(err, ErrVAPIDAuthFailed) {
		t.Fatalf("other retry: %v", err)
	}
	if err := sender.SendToSubscription(ctx, okSub, payload); err != nil {
		t.Fatal(err)
	}
	if err := sender.SendToSubscription(ctx, okSub, payload); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if hits["/apple"] != 1 || hits["/other"] != 2 || hits["/ok"] != 2 {
		t.Fatalf("hits=%v", hits)
	}
}

func TestVAPIDAuthSuppressionResetsNextDay(t *testing.T) {
	s := &PushSender{}
	sub := &domain.PushSubscription{ID: uuid.New()}
	s.rememberVAPIDAuthFailure(sub, "2026-11-24")
	if !s.vapidAuthBlocked(sub, "2026-11-24") {
		t.Fatal("same day should skip the subscription")
	}
	if s.vapidAuthBlocked(sub, "2026-11-25") {
		t.Fatal("next civil day should allow another send")
	}
	if s.vapidAuthBlocked(&domain.PushSubscription{ID: uuid.New()}, "2026-11-24") {
		t.Fatal("a different subscription should still send")
	}
}

func TestSendToSubscription_403KeepsSubscription404And410Remove(t *testing.T) {
	cases := []struct {
		status int
		keep   bool
	}{
		{status: http.StatusForbidden, keep: true},
		{status: http.StatusNotFound, keep: false},
		{status: http.StatusGone, keep: false},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			p256, auth := testPushKeys(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status == http.StatusForbidden {
					w.Header().Set("Content-Type", "application/json")
				}
				w.WriteHeader(tc.status)
				if tc.status == http.StatusForbidden {
					_, _ = w.Write([]byte(`{"reason":"BadJwtToken"}`))
				}
			}))
			t.Cleanup(srv.Close)

			db := openPushSenderDB(t)
			repo := repository.NewPushSubscriptionRepository(db)
			user := &domain.User{
				Email:    uuid.NewString() + "@test.com",
				Username: uuid.NewString(),
				IsActive: true,
			}
			if err := repository.NewUserRepository(db).Create(context.Background(), user); err != nil {
				t.Fatal(err)
			}
			endpoint := srv.URL + "/device-" + uuid.NewString()
			row := &domain.PushSubscription{
				UserID:   user.ID,
				Endpoint: endpoint,
				P256dh:   p256,
				Auth:     auth,
				IsActive: true,
			}
			if err := repo.Create(context.Background(), row); err != nil {
				t.Fatal(err)
			}

			sender := testSender(t, "mailto:x@y", repo)
			err := sender.SendToSubscription(context.Background(), row, NotificationPayload{Title: "Nhắc", Body: "Check-in"})

			var left domain.PushSubscription
			findErr := db.First(&left, "id = ?", row.ID).Error
			if tc.keep {
				if !errors.Is(err, ErrVAPIDAuthFailed) || errors.Is(err, ErrSubscriptionExpired) {
					t.Fatalf("err=%v", err)
				}
				if findErr != nil {
					t.Fatalf("403 removed the subscription: %v", findErr)
				}
				if !left.IsActive || left.Endpoint != endpoint {
					t.Fatalf("403 changed the subscription: active=%v endpoint changed=%v", left.IsActive, left.Endpoint != endpoint)
				}
				return
			}
			if !errors.Is(err, ErrSubscriptionExpired) || errors.Is(err, ErrVAPIDAuthFailed) {
				t.Fatalf("err=%v", err)
			}
			if !errors.Is(findErr, gorm.ErrRecordNotFound) {
				t.Fatalf("status %d left the subscription: find=%v row=%+v", tc.status, findErr, left)
			}
		})
	}
}

func testSender(t *testing.T, subject string, repo *repository.GormPushSubscriptionRepository) *PushSender {
	t.Helper()
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	return NewPushSender(&config.Config{
		VAPID: config.VAPIDConfig{
			PublicKey:  pub,
			PrivateKey: priv,
			Subject:    subject,
		},
	}, repo)
}

func testPushKeys(t *testing.T) (p256dh, auth string) {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		base64.RawURLEncoding.EncodeToString(secret)
}

func openPushSenderDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:push_sender_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.PushSubscription{}); err != nil {
		t.Fatal(err)
	}
	return db
}

// vapidJWTSub reads the sub claim. It does not log the token or the public key.
func vapidJWTSub(t *testing.T, authz string) string {
	t.Helper()
	const prefix = "vapid t="
	if !strings.HasPrefix(authz, prefix) {
		t.Fatal("authorization is not a vapid token")
	}
	tokenStr, _, ok := strings.Cut(strings.TrimPrefix(authz, prefix), ",")
	if !ok || tokenStr == "" {
		t.Fatal("authorization is missing the vapid token")
	}
	parsed, _, err := jwt.NewParser().ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		t.Fatal("could not decode vapid jwt")
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("vapid jwt claims")
	}
	sub, _ := claims["sub"].(string)
	return sub
}

func scrubPushLog(logged, token, auth, p256, endpoint string) string {
	replacer := strings.NewReplacer(token, "[token]", auth, "[auth]", p256, "[p256]", endpoint, "[endpoint]")
	return replacer.Replace(logged)
}
