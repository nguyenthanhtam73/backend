package push

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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

// sameDayEnv is a user who registered and checked in on Vietnam day 1 and
// skipped day 2 (today). The streak is at risk, and push is subscribed.
type sameDayEnv struct {
	db   *gorm.DB
	svc  *Service
	user *domain.User

	mu   sync.Mutex
	hits int
	srv  *httptest.Server
}

func (e *sameDayEnv) pushes() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hits
}

func (e *sameDayEnv) checkInToday(t *testing.T) {
	t.Helper()
	if err := e.db.Create(&domain.SkinCheck{
		UserID:    e.user.ID,
		ImageURLs: json.RawMessage(`[]`),
		CheckDate: streaktime.Today(),
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func (e *sameDayEnv) assertReceipts(t *testing.T, want ...string) {
	t.Helper()
	var rows []domain.PushSendReceipt
	if err := e.db.Where("user_id = ?", e.user.ID).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(want) {
		t.Fatalf("receipts=%d want %d rows=%+v", len(rows), len(want), rows)
	}
	got := map[string]bool{}
	today := streaktime.TodayString()
	for _, row := range rows {
		if row.RunDate != today {
			t.Fatalf("receipt %s run_date %s, want VN today %s", row.NotificationType, row.RunDate, today)
		}
		got[row.NotificationType] = true
	}
	for _, typ := range want {
		if !got[typ] {
			t.Fatalf("missing receipt %s in %v", typ, got)
		}
	}
}

// newDayTwoAtRisk builds the reported case: register + check in on VN day 1,
// no check-in on day 2, streak at risk, active push subscription.
func newDayTwoAtRisk(t *testing.T) *sameDayEnv {
	t.Helper()
	if streaktime.Location.String() != "Asia/Ho_Chi_Minh" {
		t.Fatalf("calendar zone %s", streaktime.Location.String())
	}

	db, err := gorm.Open(sqlite.Open("file:same_vn_day_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.PushSubscription{},
		&domain.Streak{},
		&domain.SkinCheck{},
		&domain.SkinAnalysis{},
		&domain.PushSendReceipt{},
	); err != nil {
		t.Fatal(err)
	}

	users := repository.NewUserRepository(db)
	subs := repository.NewPushSubscriptionRepository(db)
	checks := repository.NewSkinCheckRepository(db)
	streaks := repository.NewStreakRepository(db)
	receipts := repository.NewPushSendReceiptRepository(db)

	yesterday := streaktime.Today().AddDate(0, 0, -1)
	user := mustPushUser(t, users, "day2-"+uuid.NewString()+"@test.com")
	if err := users.SetCreatedAtForTest(context.Background(), user.ID, yesterday.Add(10*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.SkinCheck{
		UserID:    user.ID,
		ImageURLs: json.RawMessage(`[]`),
		CheckDate: yesterday,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Omit("User").Create(&domain.Streak{
		UserID:           user.ID,
		CurrentStreak:    1,
		LongestStreak:    1,
		LastCheckInDate:  &yesterday,
		FreezesAvailable: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}

	env := &sameDayEnv{db: db, user: user}
	env.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.mu.Lock()
		env.hits++
		env.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(env.srv.Close)

	p256, auth := mustPushKeys(t)
	if err := subs.Create(context.Background(), &domain.PushSubscription{
		UserID:   user.ID,
		Endpoint: env.srv.URL + "/" + user.ID.String(),
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
		VAPID: config.VAPIDConfig{
			PublicKey:  pub,
			PrivateKey: priv,
			Subject:    "mailto:test@dadiary.vn",
		},
	}, subs)
	env.svc = NewService(subs, sender, checks, streaks, receipts)
	return env
}

// TestSameVNDay_NewUserReminderThenStreakAtRisk is the reported case.
// Day 1: register and check in. Day 2: skip. Noon D1 (new-user) reminder
// lands; the evening streak_at_risk fan-out must not send a second nudge.
func TestSameVNDay_NewUserReminderThenStreakAtRisk(t *testing.T) {
	env := newDayTwoAtRisk(t)
	ctx := context.Background()

	if err := env.svc.SendD0D1ReminderToUser(ctx, env.user.ID, "d1"); err != nil {
		t.Fatal(err)
	}
	if env.pushes() != 1 {
		t.Fatalf("noon d1 pushes=%d", env.pushes())
	}

	result, err := env.svc.SendStreakAtRiskNotifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Sent != 0 || result.Skipped != 1 ||
		result.SkippedAlreadyNotified != 1 || result.Failed != 0 {
		t.Fatalf("evening streak fan-out: %+v", result)
	}
	if err := env.svc.SendStreakAtRiskToUser(ctx, env.user.ID); !errors.Is(err, ErrAlreadyNotifiedToday) {
		t.Fatalf("streak send err=%v", err)
	}
	if env.pushes() != 1 {
		t.Fatalf("pushes=%d, want 1", env.pushes())
	}
	env.assertReceipts(t, string(pushsvc.NotificationTypeD1Reminder))
}

// TestSameVNDay_StreakAtRiskThenNewUserReminder is the reverse order.
// The 20:00 streak job starts before the hourly new-user nudge, so streak
// can land first; the later D1 push must not send.
func TestSameVNDay_StreakAtRiskThenNewUserReminder(t *testing.T) {
	env := newDayTwoAtRisk(t)
	ctx := context.Background()

	result, err := env.svc.SendStreakAtRiskNotifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Sent != 1 || result.Failed != 0 || env.pushes() != 1 {
		t.Fatalf("streak first: %+v pushes=%d", result, env.pushes())
	}
	if err := env.svc.SendStreakAtRiskToUser(ctx, env.user.ID); !errors.Is(err, ErrAlreadyNotifiedToday) {
		t.Fatalf("second streak err=%v", err)
	}
	if err := env.svc.SendD0D1ReminderToUser(ctx, env.user.ID, "d1"); !errors.Is(err, ErrAlreadyNotifiedToday) {
		t.Fatalf("later d1 err=%v", err)
	}
	if env.pushes() != 1 {
		t.Fatalf("pushes=%d, want 1", env.pushes())
	}
	env.assertReceipts(t, string(pushsvc.NotificationTypeStreakAtRisk))
}

// TestSameVNDay_EveryReminderTypeIsExclusive locks the cap for the other
// types on the same Vietnam-day list, in both orders.
func TestSameVNDay_EveryReminderTypeIsExclusive(t *testing.T) {
	ctx := context.Background()
	others := []struct {
		name string
		send func(*Service, uuid.UUID) error
		typ  string
	}{
		{
			name: "d0_reminder",
			send: func(s *Service, id uuid.UUID) error {
				return s.SendD0D1ReminderToUser(ctx, id, "d0")
			},
			typ: string(pushsvc.NotificationTypeD0Reminder),
		},
		{
			name: "daily_reminder",
			send: func(s *Service, id uuid.UUID) error {
				return s.SendDailyReminderToUser(ctx, id)
			},
			typ: string(pushsvc.NotificationTypeDailyReminder),
		},
	}
	for _, other := range others {
		t.Run(other.name+"_then_streak", func(t *testing.T) {
			env := newDayTwoAtRisk(t)
			if err := other.send(env.svc, env.user.ID); err != nil {
				t.Fatal(err)
			}
			if err := env.svc.SendStreakAtRiskToUser(ctx, env.user.ID); !errors.Is(err, ErrAlreadyNotifiedToday) {
				t.Fatalf("streak err=%v", err)
			}
			if env.pushes() != 1 {
				t.Fatalf("pushes=%d, want 1", env.pushes())
			}
			env.assertReceipts(t, other.typ)
		})
		t.Run("streak_then_"+other.name, func(t *testing.T) {
			env := newDayTwoAtRisk(t)
			if err := env.svc.SendStreakAtRiskToUser(ctx, env.user.ID); err != nil {
				t.Fatal(err)
			}
			if err := other.send(env.svc, env.user.ID); !errors.Is(err, ErrAlreadyNotifiedToday) {
				t.Fatalf("%s err=%v", other.name, err)
			}
			if env.pushes() != 1 {
				t.Fatalf("pushes=%d, want 1", env.pushes())
			}
			env.assertReceipts(t, string(pushsvc.NotificationTypeStreakAtRisk))
		})
	}
}

// TestSameVNDay_CheckedInTodayGetsNoReminder keeps the rule that a check-in
// today suppresses both the new-user nudge and streak_at_risk.
func TestSameVNDay_CheckedInTodayGetsNoReminder(t *testing.T) {
	env := newDayTwoAtRisk(t)
	env.checkInToday(t)
	ctx := context.Background()

	if err := env.svc.SendD0D1ReminderToUser(ctx, env.user.ID, "d1"); !errors.Is(err, ErrAlreadyCheckedIn) {
		t.Fatalf("d1 err=%v", err)
	}
	if err := env.svc.SendStreakAtRiskToUser(ctx, env.user.ID); !errors.Is(err, ErrAlreadyCheckedIn) {
		t.Fatalf("streak err=%v", err)
	}
	result, err := env.svc.SendStreakAtRiskNotifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sent != 0 || result.SkippedAlreadyCheckedIn != 1 || result.Failed != 0 || env.pushes() != 0 {
		t.Fatalf("fan-out %+v pushes=%d", result, env.pushes())
	}
	env.assertReceipts(t)
}
