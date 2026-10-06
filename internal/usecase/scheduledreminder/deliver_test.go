package scheduledreminder

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/reminder"
	"github.com/dadiary/backend/internal/repository"
	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/dadiary/backend/internal/streaktime"
	pushuc "github.com/dadiary/backend/internal/usecase/push"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type schedEnv struct {
	t      *testing.T
	db     *gorm.DB
	users  *repository.GormUserRepository
	checks *repository.GormSkinCheckRepository
	subs   *repository.GormPushSubscriptionRepository
	push   *pushuc.Service
	svc    *Service
	hits   map[string]int
	mu     sync.Mutex
	srv    *httptest.Server
	p256   string
	auth   string
}

func newSchedEnv(t *testing.T, now time.Time) *schedEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:sched_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("PRAGMA busy_timeout = 5000").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.PushSubscription{},
		&domain.SkinCheck{},
		&domain.SkinAnalysis{},
		&domain.CaptureReminderClaim{},
		&domain.CheckInReminderFlag{},
		&domain.Streak{},
	); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	checks := repository.NewSkinCheckRepository(db)
	subs := repository.NewPushSubscriptionRepository(db)
	env := &schedEnv{
		t:      t,
		db:     db,
		users:  users,
		checks: checks,
		subs:   subs,
		hits:   map[string]int{},
	}
	env.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.mu.Lock()
		env.hits[r.URL.Path]++
		env.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(env.srv.Close)
	env.p256, env.auth = schedPushKeys(t)
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	sender := pushsvc.NewPushSender(&config.Config{
		VAPID: config.VAPIDConfig{PublicKey: pub, PrivateKey: priv, Subject: "mailto:test@dadiary.vn"},
	}, subs)
	env.push = pushuc.NewService(subs, sender, checks, nil, nil)
	env.svc = NewService(users, checks, repository.NewCaptureReminderClaimRepository(db), env.push, nil, nil)
	env.setClock(now)
	return env
}

func (e *schedEnv) setClock(now time.Time) {
	e.svc.SetNow(func() time.Time { return now })
}

func (e *schedEnv) addUser(email string, enabled *bool, hhmm, tz string, created time.Time) *domain.User {
	e.t.Helper()
	u := &domain.User{Email: email, Username: email, IsActive: true}
	if err := e.users.Create(context.Background(), u); err != nil {
		e.t.Fatal(err)
	}
	if !created.IsZero() {
		if err := e.users.SetCreatedAtForTest(context.Background(), u.ID, created); err != nil {
			e.t.Fatal(err)
		}
	}
	if enabled != nil || hhmm != "" || tz != "" {
		var timePtr, tzPtr *string
		if hhmm != "" {
			timePtr = &hhmm
		}
		if tz != "" {
			tzPtr = &tz
		}
		if err := e.users.SetReminderSchedule(context.Background(), u.ID, enabled, timePtr, tzPtr); err != nil {
			e.t.Fatal(err)
		}
	}
	if err := e.db.Create(&domain.PushSubscription{
		UserID:   u.ID,
		Endpoint: e.srv.URL + "/" + u.ID.String(),
		P256dh:   e.p256,
		Auth:     e.auth,
		IsActive: true,
	}).Error; err != nil {
		e.t.Fatal(err)
	}
	got, err := e.users.GetByID(context.Background(), u.ID)
	if err != nil || got == nil {
		e.t.Fatalf("reload user: %v", err)
	}
	return got
}

func (e *schedEnv) addUserNoPush(email string, enabled *bool, hhmm, tz string, created time.Time) *domain.User {
	e.t.Helper()
	u := &domain.User{Email: email, Username: email, IsActive: true}
	if err := e.users.Create(context.Background(), u); err != nil {
		e.t.Fatal(err)
	}
	if !created.IsZero() {
		if err := e.users.SetCreatedAtForTest(context.Background(), u.ID, created); err != nil {
			e.t.Fatal(err)
		}
	}
	var timePtr, tzPtr *string
	if hhmm != "" {
		timePtr = &hhmm
	}
	if tz != "" {
		tzPtr = &tz
	}
	if err := e.users.SetReminderSchedule(context.Background(), u.ID, enabled, timePtr, tzPtr); err != nil {
		e.t.Fatal(err)
	}
	got, err := e.users.GetByID(context.Background(), u.ID)
	if err != nil || got == nil {
		e.t.Fatalf("reload user: %v", err)
	}
	return got
}

func (e *schedEnv) checkIn(u *domain.User, at time.Time) {
	e.t.Helper()
	row := &domain.SkinCheck{
		UserID:    u.ID,
		ImageURLs: json.RawMessage(`[]`),
		CheckDate: streaktime.DateOf(at),
		CreatedAt: at,
	}
	if err := e.checks.CreateWithAnalysis(context.Background(), row, &domain.SkinAnalysis{}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *schedEnv) hit(id uuid.UUID) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.hits["/"+id.String()]
}

func (e *schedEnv) claims(userID uuid.UUID) int64 {
	e.t.Helper()
	var n int64
	if err := e.db.Model(&domain.CaptureReminderClaim{}).Where("user_id = ?", userID).Count(&n).Error; err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *schedEnv) deliver(t *testing.T) Result {
	t.Helper()
	res, err := e.svc.Deliver(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func schedPushKeys(t *testing.T) (string, string) {
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

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestScheduledUserSendsOnceInNonVNZone(t *testing.T) {
	// 08:04 in Tokyo is 06:04 in Vietnam — not the 20:00 fixed slot.
	now := time.Date(2026, 6, 15, 8, 4, 0, 0, mustLoc(t, "Asia/Tokyo"))
	env := newSchedEnv(t, now)
	old := now.Add(-40 * 24 * time.Hour)
	enabled := true
	tokyo := env.addUser("tokyo@test.com", &enabled, "08:00", "Asia/Tokyo", old)
	unset := env.addUser("unset@test.com", nil, "", "", old)
	noTime := env.addUser("notime@test.com", &enabled, "", "", old)

	res := env.deliver(t)
	if res.Sent != 1 || res.PushSent != 1 || res.Failed != 0 {
		t.Fatalf("deliver: %+v", res)
	}
	if env.hit(tokyo.ID) != 1 || env.hit(unset.ID) != 0 || env.hit(noTime.ID) != 0 {
		t.Fatalf("hits tokyo=%d unset=%d notime=%d", env.hit(tokyo.ID), env.hit(unset.ID), env.hit(noTime.ID))
	}
	if env.claims(tokyo.ID) != 1 {
		t.Fatalf("claims=%d", env.claims(tokyo.ID))
	}
}

func TestNULLScheduleUnchangedOnFixedClock(t *testing.T) {
	now := time.Date(2026, 6, 15, 8, 4, 0, 0, mustLoc(t, "Asia/Tokyo"))
	env := newSchedEnv(t, now)
	old := now.Add(-40 * 24 * time.Hour)
	enabled := true
	tokyo := env.addUser("tokyo-fixed@test.com", &enabled, "08:00", "Asia/Tokyo", old)
	unset := env.addUser("unset-fixed@test.com", nil, "", "", old)
	noTime := env.addUser("notime-fixed@test.com", &enabled, "", "", old)

	daily, err := env.push.SendDailyRemindersToAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if daily.Sent != 2 || daily.Failed != 0 {
		t.Fatalf("daily fan-out: %+v", daily)
	}
	if env.hit(unset.ID) != 1 || env.hit(noTime.ID) != 1 || env.hit(tokyo.ID) != 0 {
		t.Fatalf("fixed clock hits tokyo=%d unset=%d notime=%d", env.hit(tokyo.ID), env.hit(unset.ID), env.hit(noTime.ID))
	}
}

func TestDefaultTimezoneIsVietnam(t *testing.T) {
	// 06:04 in Vietnam. A missing or invalid zone must use that clock, not Tokyo.
	now := time.Date(2026, 6, 15, 6, 4, 0, 0, streaktime.Location)
	env := newSchedEnv(t, now)
	old := now.Add(-40 * 24 * time.Hour)
	enabled := true
	missing := env.addUser("missing-tz@test.com", &enabled, "06:00", "", old)
	bad := env.addUser("bad-tz@test.com", &enabled, "06:00", "Not/AZone", old)
	early := env.addUser("early-vn@test.com", &enabled, "08:00", "", old)

	res := env.deliver(t)
	if res.Sent != 2 || res.PushSent != 2 || res.Failed != 0 {
		t.Fatalf("deliver: %+v", res)
	}
	if env.hit(missing.ID) != 1 || env.hit(bad.ID) != 1 || env.hit(early.ID) != 0 {
		t.Fatalf("hits missing=%d bad=%d early=%d", env.hit(missing.ID), env.hit(bad.ID), env.hit(early.ID))
	}
}

func TestNothingToSendKeepsClaimForTheLocalDay(t *testing.T) {
	now := time.Date(2026, 6, 15, 8, 4, 0, 0, mustLoc(t, "Asia/Tokyo"))
	env := newSchedEnv(t, now)
	enabled := true
	// Old account: not D0/D1, and no first check, so no Day-3 email is due.
	// No push subscription, so the push is skipped rather than sent.
	user := env.addUserNoPush("quiet@test.com", &enabled, "08:00", "Asia/Tokyo", now.Add(-40*24*time.Hour))

	first := env.deliver(t)
	if first.Sent != 0 || first.PushSent != 0 || first.EmailSent != 0 || first.Failed != 0 {
		t.Fatalf("first tick should send nothing: %+v", first)
	}
	if env.hit(user.ID) != 0 || env.claims(user.ID) != 1 {
		t.Fatalf("hits=%d claims=%d", env.hit(user.ID), env.claims(user.ID))
	}

	later := now.Add(5 * time.Minute)
	env.setClock(later)
	second := env.deliver(t)
	if second.Sent != 0 || second.PushSent != 0 || second.Failed != 0 {
		t.Fatalf("later tick should not send: %+v", second)
	}
	if env.hit(user.ID) != 0 || env.claims(user.ID) != 1 {
		t.Fatalf("later tick hits=%d claims=%d", env.hit(user.ID), env.claims(user.ID))
	}
}

func TestAlreadyCheckedInLocalDaySkips(t *testing.T) {
	loc := mustLoc(t, "Asia/Tokyo")
	now := time.Date(2026, 6, 15, 8, 4, 0, 0, loc)
	env := newSchedEnv(t, now)
	old := now.Add(-40 * 24 * time.Hour)
	enabled := true
	done := env.addUser("checked@test.com", &enabled, "08:00", "Asia/Tokyo", old)
	open := env.addUser("open@test.com", &enabled, "08:00", "Asia/Tokyo", old)
	env.checkIn(done, time.Date(2026, 6, 15, 7, 30, 0, 0, loc))

	res := env.deliver(t)
	if res.Sent != 1 || res.PushSent != 1 {
		t.Fatalf("deliver: %+v", res)
	}
	if env.hit(done.ID) != 0 || env.hit(open.ID) != 1 {
		t.Fatalf("hits done=%d open=%d", env.hit(done.ID), env.hit(open.ID))
	}
	if env.claims(done.ID) != 0 {
		t.Fatalf("checked-in user was claimed: %d", env.claims(done.ID))
	}
}

func TestOffSkipsScheduledCapture(t *testing.T) {
	now := time.Date(2026, 6, 15, 8, 4, 0, 0, mustLoc(t, "Asia/Tokyo"))
	env := newSchedEnv(t, now)
	old := now.Add(-40 * 24 * time.Hour)
	disabled := false
	enabled := true
	off := env.addUser("off@test.com", &disabled, "08:00", "Asia/Tokyo", old)
	on := env.addUser("on@test.com", &enabled, "08:00", "Asia/Tokyo", old)

	res := env.deliver(t)
	if res.Sent != 1 || env.hit(off.ID) != 0 || env.hit(on.ID) != 1 {
		t.Fatalf("deliver=%+v off=%d on=%d", res, env.hit(off.ID), env.hit(on.ID))
	}
}

func TestTwoTicksAndTwoReplicasDoNotDoubleSend(t *testing.T) {
	now := time.Date(2026, 6, 15, 8, 4, 0, 0, mustLoc(t, "Asia/Tokyo"))
	env := newSchedEnv(t, now)
	enabled := true
	user := env.addUser("once@test.com", &enabled, "08:00", "Asia/Tokyo", now.Add(-40*24*time.Hour))

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, err := env.svc.Deliver(context.Background())
			errCh <- err
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	again := env.deliver(t)
	if again.Sent != 0 {
		t.Fatalf("third tick sent again: %+v", again)
	}
	if env.hit(user.ID) != 1 || env.claims(user.ID) != 1 {
		t.Fatalf("hits=%d claims=%d", env.hit(user.ID), env.claims(user.ID))
	}
}

func TestNewYorkDSTSendsOnTheRightOffset(t *testing.T) {
	env := newSchedEnv(t, time.Time{})
	enabled := true
	user := env.addUser(
		"ny@test.com",
		&enabled,
		"09:00",
		"America/New_York",
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	)

	// July 15:30 UTC is 11:30 EDT, past the 2h grace. A fixed EST offset would send.
	env.setClock(time.Date(2026, 7, 15, 15, 30, 0, 0, time.UTC))
	july := env.deliver(t)
	if july.Sent != 0 || env.hit(user.ID) != 0 {
		t.Fatalf("july should be closed: %+v hits=%d", july, env.hit(user.ID))
	}

	// January 15:30 UTC is 10:30 EST, inside 09:00–11:00. A fixed EDT offset would not send.
	env.setClock(time.Date(2026, 1, 15, 15, 30, 0, 0, time.UTC))
	january := env.deliver(t)
	if january.Sent != 1 || january.PushSent != 1 || env.hit(user.ID) != 1 {
		t.Fatalf("january should send: %+v hits=%d", january, env.hit(user.ID))
	}

	// Fall-back day: both 01:35 wall times are one local date. One send.
	fold := env.addUser(
		"fold@test.com",
		&enabled,
		"01:30",
		"America/New_York",
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	)
	env.setClock(time.Date(2026, 11, 1, 5, 35, 0, 0, time.UTC))
	if res := env.deliver(t); res.PushSent != 1 || env.hit(fold.ID) != 1 {
		t.Fatalf("first fall-back instant: %+v hits=%d", res, env.hit(fold.ID))
	}
	env.setClock(time.Date(2026, 11, 1, 6, 35, 0, 0, time.UTC))
	if res := env.deliver(t); res.Sent != 0 || env.hit(fold.ID) != 1 || env.claims(fold.ID) != 1 {
		t.Fatalf("second fall-back instant double-sent: %+v hits=%d claims=%d", res, env.hit(fold.ID), env.claims(fold.ID))
	}
}

func TestAtRiskSavedScheduleSendsStreakNotDaily(t *testing.T) {
	now := time.Date(2026, 6, 15, 8, 4, 0, 0, mustLoc(t, "Asia/Tokyo"))
	env := newSchedEnv(t, now)
	if err := env.db.AutoMigrate(&domain.PushSendReceipt{}); err != nil {
		t.Fatal(err)
	}
	enabled := true
	user := env.addUser("risk-sched@test.com", &enabled, "08:00", "Asia/Tokyo", now.Add(-40*24*time.Hour))
	yesterday := streaktime.Today().AddDate(0, 0, -1)
	if err := env.db.Omit("User").Create(&domain.Streak{
		UserID:           user.ID,
		CurrentStreak:    4,
		LastCheckInDate:  &yesterday,
		FreezesAvailable: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	receipts := repository.NewPushSendReceiptRepository(env.db)
	streaks := repository.NewStreakRepository(env.db)
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	sender := pushsvc.NewPushSender(&config.Config{
		VAPID: config.VAPIDConfig{PublicKey: pub, PrivateKey: priv, Subject: "mailto:test@dadiary.vn"},
	}, env.subs)
	pushSvc := pushuc.NewService(env.subs, sender, env.checks, streaks, receipts)
	env.svc = NewService(env.users, env.checks, repository.NewCaptureReminderClaimRepository(env.db), pushSvc, streaks, nil)
	env.setClock(now)

	res := env.deliver(t)
	if res.Sent != 1 || res.PushSent != 1 || env.hit(user.ID) != 1 {
		t.Fatalf("deliver=%+v hits=%d", res, env.hit(user.ID))
	}
	var row domain.PushSendReceipt
	if err := env.db.Where("user_id = ?", user.ID).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.NotificationType != "streak_at_risk" {
		t.Fatalf("push type %s", row.NotificationType)
	}
	// The 20:00 streak list must not also contain this saved schedule.
	ids, err := env.subs.ListActiveUserIDs(context.Background(), reminder.JobStreakAtRisk)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id == user.ID {
			t.Fatal("streak_at_risk 20:00 list still contains the saved schedule")
		}
	}
}

func TestFixedJobsSkipSavedSchedule(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, streaktime.Location)
	env := newSchedEnv(t, now)
	old := now.Add(-40 * 24 * time.Hour)
	enabled := true
	saved := env.addUser("saved-fixed@test.com", &enabled, "21:00", "Asia/Ho_Chi_Minh", old)
	unset := env.addUser("unset-list@test.com", nil, "", "", old)

	ctx := reminder.ObserveContext(context.Background())
	ids, err := env.subs.ListActiveUserIDs(ctx, reminder.JobDailyPush)
	if err != nil {
		t.Fatal(err)
	}
	if !reminder.Observed(ctx, reminder.JobDailyPush) {
		t.Fatal("daily list did not call ExcludeMuted")
	}
	got := map[uuid.UUID]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if got[saved.ID] || !got[unset.ID] {
		t.Fatalf("daily candidates saved=%v unset=%v ids=%v", got[saved.ID], got[unset.ID], ids)
	}

	day := streaktime.DateOf(now)
	for _, u := range []*domain.User{saved, unset} {
		if err := env.db.Create(&domain.CheckInReminderFlag{
			UserID:     u.ID,
			Kind:       "d0",
			Due:        true,
			SignupDate: day,
			ComputedOn: day,
			ComputedAt: now,
		}).Error; err != nil {
			t.Fatal(err)
		}
		env.checkIn(u, day.Add(-24*time.Hour))
	}
	flags := repository.NewCheckInReminderRepository(env.db)
	rows, err := flags.ListDue(ctx, 100, reminder.JobD0Email, reminder.JobD0D1Push)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].UserID != unset.ID {
		t.Fatalf("hourly due rows: %+v", rows)
	}
	if !reminder.Observed(ctx, reminder.JobD0Email) || !reminder.Observed(ctx, reminder.JobD0D1Push) {
		t.Fatal("hourly list did not call ExcludeMuted for both jobs")
	}

	cohorts, err := env.checks.ListUsersByFirstCheckDates(ctx, []time.Time{day.Add(-24 * time.Hour)}, 100, reminder.JobEveningEmail)
	if err != nil {
		t.Fatal(err)
	}
	if len(cohorts) != 1 || cohorts[0].UserID != unset.ID {
		t.Fatalf("evening cohorts: %+v", cohorts)
	}
	if !reminder.Observed(ctx, reminder.JobEveningEmail) {
		t.Fatal("evening list did not call ExcludeMuted")
	}
}
