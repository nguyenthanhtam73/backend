package checkinreminder

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
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

// jobRunner exercises one outbound reminder. NULL and true must be contacted.
// reminder_enabled = false must not.
type jobRunner func(t *testing.T, ctx context.Context)

// TestEveryReminderJobSkipsMutedUsers is the registry check for reminder.All.
// A new outbound reminder has to be listed there and given a runner here.
// Each runner must call reminder.ExcludeMuted with its own JobID, skip
// reminder_enabled = false, and still include a user who never set the column.
func TestEveryReminderJobSkipsMutedUsers(t *testing.T) {
	runners := map[reminder.JobID]jobRunner{
		reminder.JobDailyPush:    runDailyPushMute,
		reminder.JobStreakAtRisk: runStreakAtRiskMute,
		reminder.JobEveningEmail: runEveningEmailMute,
		reminder.JobD0Email:      runD0EmailMute,
		reminder.JobD0D1Push:     runD0D1PushMute,
	}

	jobs := reminder.All()
	if len(jobs) != len(runners) {
		t.Fatalf("reminder.All has %d jobs, runners has %d — register the new job", len(jobs), len(runners))
	}
	seen := make(map[reminder.JobID]bool, len(jobs))
	for _, job := range jobs {
		if job.ID == "" {
			t.Fatal("reminder.All contains an empty job id")
		}
		if seen[job.ID] {
			t.Fatalf("reminder.All lists %s twice", job.ID)
		}
		seen[job.ID] = true
		runner, ok := runners[job.ID]
		if !ok {
			t.Fatalf("reminder job %s (%s) has no mute runner", job.ID, job.Name)
		}
		t.Run(string(job.ID), func(t *testing.T) {
			ctx := reminder.ObserveContext(context.Background())
			runner(t, ctx)
			if !reminder.Observed(ctx, job.ID) {
				t.Fatalf("%s did not call reminder.ExcludeMuted with its own job id", job.ID)
			}
		})
	}
	for id := range runners {
		if !seen[id] {
			t.Fatalf("runner %s is not in reminder.All", id)
		}
	}
}

type muteCohort struct {
	unset *domain.User
	on    *domain.User
	off   *domain.User
}

func seedMuteCohort(t *testing.T, users *repository.GormUserRepository, created time.Time) muteCohort {
	t.Helper()
	out := muteCohort{
		unset: createUser(t, users, "unset@test.com", created),
		on:    createUser(t, users, "on@test.com", created),
		off:   createUser(t, users, "off@test.com", created),
	}
	enabled := true
	disabled := false
	if err := users.SetReminderSchedule(context.Background(), out.on.ID, &enabled, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := users.SetReminderSchedule(context.Background(), out.off.ID, &disabled, nil, nil); err != nil {
		t.Fatal(err)
	}
	return out
}

func assertMutedSkipped(t *testing.T, label string, got map[string]int, c muteCohort) {
	t.Helper()
	if got[c.unset.Email] != 1 || got[c.on.Email] != 1 || got[c.off.Email] != 0 {
		t.Fatalf("%s: NULL and true should be sent once, false skipped: %#v", label, got)
	}
}

func runDailyPushMute(t *testing.T, ctx context.Context) {
	t.Helper()
	db := openMutePushDB(t)
	users := repository.NewUserRepository(db)
	subs := repository.NewPushSubscriptionRepository(db)
	c := seedMuteCohort(t, users, time.Now().Add(-time.Hour))
	hits, srv := subscribeMutePush(t, subs, c)
	defer srv.Close()

	svc := pushuc.NewService(subs, newMutePushSender(t, subs), nil, nil, nil)
	result, err := svc.SendDailyRemindersToAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || result.Sent != 2 || result.Failed != 0 {
		t.Fatalf("daily fan-out: %+v hits=%v", result, hits.snapshot())
	}
	assertMutedSkipped(t, "daily_reminder", hits.byEmail(c), c)
}

func runStreakAtRiskMute(t *testing.T, ctx context.Context) {
	t.Helper()
	db := openMutePushDB(t)
	if err := db.AutoMigrate(&domain.Streak{}); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	subs := repository.NewPushSubscriptionRepository(db)
	streaks := repository.NewStreakRepository(db)
	c := seedMuteCohort(t, users, time.Now().Add(-48*time.Hour))
	hits, srv := subscribeMutePush(t, subs, c)
	defer srv.Close()
	yesterday := streaktime.Today().AddDate(0, 0, -1)
	for _, u := range []*domain.User{c.unset, c.on, c.off} {
		if err := db.Omit("User").Create(&domain.Streak{
			UserID:           u.ID,
			CurrentStreak:    4,
			LastCheckInDate:  &yesterday,
			FreezesAvailable: 1,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	svc := pushuc.NewService(subs, newMutePushSender(t, subs), nil, streaks, nil)
	result, err := svc.SendStreakAtRiskNotifications(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 || result.Sent != 2 || result.Failed != 0 {
		t.Fatalf("streak fan-out: %+v hits=%v", result, hits.snapshot())
	}
	assertMutedSkipped(t, "streak_at_risk", hits.byEmail(c), c)
}

func runEveningEmailMute(t *testing.T, ctx context.Context) {
	t.Helper()
	now := vnAt(2026, 10, 5, 19, 30)
	svc, users, checks, db := setupReminderSvc(t, now)
	c := seedMuteCohort(t, users, vnAt(2026, 9, 1, 9, 0))
	insertCheck(t, checks, c.unset, vnAt(2026, 10, 4, 20, 0))
	insertCheck(t, checks, c.on, vnAt(2026, 10, 4, 20, 0))
	insertCheck(t, checks, c.off, vnAt(2026, 10, 4, 20, 0))

	mailer := &stubMailer{ready: true}
	svc.AttachOutbound(mailer, repository.NewEmailSendReceiptRepository(db), nil, nil, "https://dadiary.vn/check-in")
	res, err := svc.DeliverEveningEmails(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.EmailSent != 2 {
		t.Fatalf("evening send: %+v", res)
	}
	assertMutedSkipped(t, "evening email", mailer.counts(), c)
}

func runD0EmailMute(t *testing.T, ctx context.Context) {
	t.Helper()
	now := vnAt(2026, 10, 5, 11, 0)
	svc, users, _, db := setupReminderSvc(t, now)
	c := seedMuteCohort(t, users, vnAt(2026, 10, 5, 9, 0))
	if _, err := svc.RefreshWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	mailer := &stubMailer{ready: true}
	svc.AttachOutbound(mailer, repository.NewEmailSendReceiptRepository(db), nil, nil, "https://dadiary.vn/check-in")
	res, err := svc.DeliverDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.EmailSent != 2 {
		t.Fatalf("hourly D0 email: %+v sent=%v", res, mailer.sent)
	}
	assertMutedSkipped(t, "hourly D0 email", mailer.counts(), c)
}

func runD0D1PushMute(t *testing.T, ctx context.Context) {
	t.Helper()
	now := vnAt(2026, 10, 5, 11, 0)
	svc, users, _, db := setupReminderSvc(t, now)
	svc.vapidConfigured = true
	if err := db.AutoMigrate(&domain.PushSubscription{}); err != nil {
		t.Fatal(err)
	}
	c := seedMuteCohort(t, users, vnAt(2026, 10, 5, 9, 0))
	subs := repository.NewPushSubscriptionRepository(db)
	hits, srv := subscribeMutePush(t, subs, c)
	defer srv.Close()
	if _, err := svc.RefreshWindow(context.Background()); err != nil {
		t.Fatal(err)
	}
	pushSvc := pushuc.NewService(subs, newMutePushSender(t, subs), nil, nil, nil)
	svc.AttachOutbound(nil, nil, pushSvc, nil, "https://dadiary.vn/check-in")
	res, err := svc.DeliverDue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.PushSent != 2 || res.PushFailed != 0 {
		t.Fatalf("hourly D0/D1 push: %+v hits=%v", res, hits.snapshot())
	}
	assertMutedSkipped(t, "hourly D0/D1 push", hits.byEmail(c), c)
}

func (m *stubMailer) counts() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]int{}
	for _, msg := range m.sent {
		out[msg.To]++
	}
	return out
}

type mutePushHits struct {
	mu   sync.Mutex
	byID map[string]int
}

func (h *mutePushHits) snapshot() map[string]int {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]int, len(h.byID))
	for k, v := range h.byID {
		out[k] = v
	}
	return out
}

func (h *mutePushHits) byEmail(c muteCohort) map[string]int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return map[string]int{
		c.unset.Email: h.byID["/"+c.unset.ID.String()],
		c.on.Email:    h.byID["/"+c.on.ID.String()],
		c.off.Email:   h.byID["/"+c.off.ID.String()],
	}
}

func subscribeMutePush(t *testing.T, subs *repository.GormPushSubscriptionRepository, c muteCohort) (*mutePushHits, *httptest.Server) {
	t.Helper()
	hits := &mutePushHits{byID: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.mu.Lock()
		hits.byID[r.URL.Path]++
		hits.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	p256, auth := mustMutePushKeys(t)
	for _, u := range []*domain.User{c.unset, c.on, c.off} {
		if err := subs.Create(context.Background(), &domain.PushSubscription{
			UserID:   u.ID,
			Endpoint: srv.URL + "/" + u.ID.String(),
			P256dh:   p256,
			Auth:     auth,
			IsActive: true,
		}); err != nil {
			srv.Close()
			t.Fatal(err)
		}
	}
	return hits, srv
}

func openMutePushDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:all_jobs_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
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

func newMutePushSender(t *testing.T, subs *repository.GormPushSubscriptionRepository) *pushsvc.PushSender {
	t.Helper()
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	return pushsvc.NewPushSender(&config.Config{
		VAPID: config.VAPIDConfig{
			PublicKey:  pub,
			PrivateKey: priv,
			Subject:    "mailto:test@dadiary.vn",
		},
	}, subs)
}

func mustMutePushKeys(t *testing.T) (p256dh, auth string) {
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
