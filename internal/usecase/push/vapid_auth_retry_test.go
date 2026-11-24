package push

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/google/uuid"
)

// A 403 BadJwtToken must not count as a batch failure (that releases the
// once-per-day claim and retries every 30 minutes) and must not POST the
// same subscription again. A 500 still fails and is retried.
func TestSendDailyRemindersToAll_VAPIDAuthSkippedAndNotRetried(t *testing.T) {
	db := openPushReminderDB(t)
	users := repository.NewUserRepository(db)
	subs := repository.NewPushSubscriptionRepository(db)

	apple := mustPushUser(t, users, "apple-"+uuid.NewString()+"@test.com")
	fcm := mustPushUser(t, users, "fcm-"+uuid.NewString()+"@test.com")
	p256, auth := mustPushKeys(t)

	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		if r.URL.Path == "/"+apple.ID.String() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"reason":"BadJwtToken"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	for _, u := range []*domain.User{apple, fcm} {
		if err := subs.Create(context.Background(), &domain.PushSubscription{
			UserID:   u.ID,
			Endpoint: srv.URL + "/" + u.ID.String(),
			P256dh:   p256,
			Auth:     auth,
			IsActive: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	sender := pushsvc.NewPushSender(&config.Config{
		VAPID: config.VAPIDConfig{
			PublicKey:  pub,
			PrivateKey: priv,
			Subject:    "mailto:noreply@dadiary.app",
		},
	}, subs)
	svc := NewService(subs, sender, nil, nil, nil)
	ctx := context.Background()

	first, err := svc.SendDailyRemindersToAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.Failed != 1 || first.Sent != 0 || first.Skipped != 1 {
		t.Fatalf("first fan-out: %+v", first)
	}

	second, err := svc.SendDailyRemindersToAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second.Failed != 1 || second.Sent != 0 || second.Skipped != 1 {
		t.Fatalf("second fan-out: %+v", second)
	}

	mu.Lock()
	defer mu.Unlock()
	if hits["/"+apple.ID.String()] != 1 {
		t.Fatalf("apple subscription was retried: hits=%v", hits)
	}
	if hits["/"+fcm.ID.String()] != 2 {
		t.Fatalf("provider error was not retried: hits=%v", hits)
	}

	var n int64
	if err := db.Model(&domain.PushSubscription{}).Where("user_id IN ?", []uuid.UUID{apple.ID, fcm.ID}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("subscriptions left=%d want 2 (403 and 500 must keep the row)", n)
	}
}

func TestSendDailyRemindersToAll_OnlyVAPIDAuthDoesNotFailBatch(t *testing.T) {
	db := openPushReminderDB(t)
	users := repository.NewUserRepository(db)
	subs := repository.NewPushSubscriptionRepository(db)
	user := mustPushUser(t, users, "only-apple-"+uuid.NewString()+"@test.com")
	p256, auth := mustPushKeys(t)

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"reason":"BadJwtToken"}`))
	}))
	t.Cleanup(srv.Close)

	if err := subs.Create(context.Background(), &domain.PushSubscription{
		UserID:   user.ID,
		Endpoint: srv.URL + "/" + user.ID.String(),
		P256dh:   p256,
		Auth:     auth,
		IsActive: true,
	}); err != nil {
		t.Fatal(err)
	}
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	sender := pushsvc.NewPushSender(&config.Config{
		VAPID: config.VAPIDConfig{PublicKey: pub, PrivateKey: priv, Subject: "mailto:x@y"},
	}, subs)
	svc := NewService(subs, sender, nil, nil, nil)

	first, err := svc.SendDailyRemindersToAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Failed != 0 || first.Sent != 0 || first.Skipped != 1 || first.Total != 1 {
		t.Fatalf("first: %+v", first)
	}
	second, err := svc.SendDailyRemindersToAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Failed != 0 || hits != 1 {
		t.Fatalf("second=%+v hits=%d", second, hits)
	}
}
