package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSendDailyRemindersToAll_SkipsReminderOff(t *testing.T) {
	db := openPushReminderDB(t)
	users := repository.NewUserRepository(db)
	subs := repository.NewPushSubscriptionRepository(db)

	var mu sync.Mutex
	hit := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hit[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	unset := mustPushUser(t, users, "push-unset@test.com")
	on := mustPushUser(t, users, "push-on@test.com")
	off := mustPushUser(t, users, "push-off@test.com")
	enabled := true
	disabled := false
	if err := users.SetReminderSchedule(context.Background(), on.ID, &enabled, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := users.SetReminderSchedule(context.Background(), off.ID, &disabled, nil, nil); err != nil {
		t.Fatal(err)
	}
	p256, auth := mustPushKeys(t)
	for _, u := range []*domain.User{unset, on, off} {
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
			Subject:    "mailto:test@dadiary.vn",
		},
	}, subs)
	svc := NewService(subs, sender, nil, nil, nil)
	result, err := svc.SendDailyRemindersToAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || result.Sent != 2 || result.Failed != 0 {
		t.Fatalf("fan-out: %+v hits=%v", result, hit)
	}
	mu.Lock()
	defer mu.Unlock()
	if hit["/"+unset.ID.String()] != 1 || hit["/"+on.ID.String()] != 1 || hit["/"+off.ID.String()] != 0 {
		t.Fatalf("endpoints: %#v", hit)
	}
}

func TestGetUsersAtRiskWithPush_SkipsReminderOff(t *testing.T) {
	db := openPushReminderDB(t)
	users := repository.NewUserRepository(db)
	subs := repository.NewPushSubscriptionRepository(db)
	streaks := repository.NewStreakRepository(db)
	yesterday := streaktime.Today().AddDate(0, 0, -1)

	unset := mustPushUser(t, users, "risk-unset@test.com")
	on := mustPushUser(t, users, "risk-on@test.com")
	off := mustPushUser(t, users, "risk-off@test.com")
	enabled := true
	disabled := false
	if err := users.SetReminderSchedule(context.Background(), on.ID, &enabled, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := users.SetReminderSchedule(context.Background(), off.ID, &disabled, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, u := range []*domain.User{unset, on, off} {
		if err := subs.Create(context.Background(), &domain.PushSubscription{
			UserID:   u.ID,
			Endpoint: "https://push.example/" + u.ID.String(),
			P256dh:   "key",
			Auth:     "auth",
			IsActive: true,
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.Omit("User").Create(&domain.Streak{
			UserID:           u.ID,
			CurrentStreak:    4,
			LastCheckInDate:  &yesterday,
			FreezesAvailable: 1,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	svc := NewService(subs, nil, nil, streaks, nil)
	ids, err := svc.GetUsersAtRiskWithPush(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got[unset.ID] || !got[on.ID] || got[off.ID] || len(ids) != 2 {
		t.Fatalf("at-risk ids=%v unset=%s on=%s off=%s", ids, unset.ID, on.ID, off.ID)
	}
}

func openPushReminderDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:push_off_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.PushSubscription{}, &domain.Streak{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func mustPushUser(t *testing.T, users *repository.GormUserRepository, email string) *domain.User {
	t.Helper()
	u := &domain.User{Email: email, Username: email, IsActive: true}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func mustPushKeys(t *testing.T) (p256dh, auth string) {
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
