package adminfunnel

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

func TestCountRetentionProxies_D0D1AndEligibility(t *testing.T) {
	// Noon VN on 2026-09-05 — well clear of the UTC/VN midnight boundary.
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, streaktime.Location)
	today := streaktime.DateOf(now)
	yesterday := today.AddDate(0, 0, -1)
	twoDaysAgo := today.AddDate(0, 0, -2)
	since7d := now.Add(-7 * 24 * time.Hour)

	d0Only := uuid.New()
	d1Only := uuid.New()
	both := uuid.New()
	neither := uuid.New()
	signedToday := uuid.New() // D0 possible; D1 not eligible yet
	oldCohort := uuid.New()   // signed up 10d ago — all-time only
	dupSignup := uuid.New()

	signups := []SignupRow{
		{UserID: d0Only, CreatedAt: yesterday.Add(3 * time.Hour)},
		{UserID: d1Only, CreatedAt: yesterday.Add(4 * time.Hour)},
		{UserID: both, CreatedAt: twoDaysAgo.Add(2 * time.Hour)},
		{UserID: neither, CreatedAt: twoDaysAgo.Add(5 * time.Hour)},
		{UserID: signedToday, CreatedAt: today.Add(5 * time.Hour)},
		{UserID: oldCohort, CreatedAt: now.Add(-10 * 24 * time.Hour)},
		{UserID: dupSignup, CreatedAt: yesterday.Add(6 * time.Hour)},
		{UserID: dupSignup, CreatedAt: yesterday.Add(6 * time.Hour)}, // duplicate row ignored
	}
	checks := []CheckDateRow{
		{UserID: d0Only, CheckDate: yesterday},
		{UserID: d1Only, CheckDate: today}, // D1 of yesterday signup
		{UserID: both, CheckDate: twoDaysAgo},
		{UserID: both, CheckDate: yesterday}, // D1
		{UserID: signedToday, CheckDate: today},
		{UserID: oldCohort, CheckDate: streaktime.DateOf(now.Add(-10 * 24 * time.Hour))},
		{UserID: oldCohort, CheckDate: streaktime.DateOf(now.Add(-10*24*time.Hour)).AddDate(0, 0, 1)},
		{UserID: d0Only, CheckDate: yesterday}, // duplicate check day ignored
	}

	got := CountRetentionProxies(signups, checks, now, since7d)
	if got.D0Users != 4 { // d0Only, both, signedToday, oldCohort
		t.Fatalf("D0Users=%d want 4", got.D0Users)
	}
	if got.D0Users7d != 3 { // excludes oldCohort
		t.Fatalf("D0Users7d=%d want 3", got.D0Users7d)
	}
	if got.D1Eligible != 6 { // all unique signups except signedToday (includes dupSignup)
		t.Fatalf("D1Eligible=%d want 6", got.D1Eligible)
	}
	if got.D1Eligible7d != 5 { // excludes oldCohort
		t.Fatalf("D1Eligible7d=%d want 5", got.D1Eligible7d)
	}
	if got.D1Users != 3 { // d1Only, both, oldCohort
		t.Fatalf("D1Users=%d want 3", got.D1Users)
	}
	if got.D1Users7d != 2 { // d1Only, both
		t.Fatalf("D1Users7d=%d want 2", got.D1Users7d)
	}
}

func TestCountRetentionProxies_VietnamMidnightBoundary(t *testing.T) {
	// 2026-07-15 17:30 UTC == 2026-07-16 00:30 VN — signup is VN 16th.
	signup := time.Date(2026, 7, 15, 17, 30, 0, 0, time.UTC)
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, streaktime.Location)
	uid := uuid.New()

	got := CountRetentionProxies(
		[]SignupRow{{UserID: uid, CreatedAt: signup}},
		[]CheckDateRow{{UserID: uid, CheckDate: streaktime.DateOf(signup)}},
		now,
		now.Add(-7*24*time.Hour),
	)
	if got.D0Users != 1 {
		t.Fatalf("D0 across UTC/VN midnight: D0Users=%d want 1", got.D0Users)
	}
	if got.D1Eligible != 0 {
		t.Fatalf("same VN day is not D1-eligible: eligible=%d", got.D1Eligible)
	}

	// Evening UTC still previous VN day would be wrong if we truncated UTC.
	earlyUTC := time.Date(2026, 7, 15, 16, 59, 0, 0, time.UTC) // still 15th VN
	got = CountRetentionProxies(
		[]SignupRow{{UserID: uid, CreatedAt: earlyUTC}},
		[]CheckDateRow{{UserID: uid, CheckDate: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)}},
		now,
		now.Add(-7*24*time.Hour),
	)
	if got.D0Users != 1 {
		t.Fatalf("D0 on VN 15th: D0Users=%d want 1", got.D0Users)
	}
	if got.D1Eligible != 1 {
		t.Fatalf("signup VN 15th, now VN 16th: eligible=%d want 1", got.D1Eligible)
	}
}

func TestCountRetentionProxies_EmptyAndNil(t *testing.T) {
	got := CountRetentionProxies(nil, nil, time.Time{}, time.Time{})
	if got != (RetentionProxies{}) {
		t.Fatalf("empty input: %#v", got)
	}
	got = CountRetentionProxies(
		[]SignupRow{{UserID: uuid.Nil, CreatedAt: time.Now()}},
		[]CheckDateRow{{UserID: uuid.Nil, CheckDate: time.Now()}},
		time.Now(),
		time.Now().Add(-24*time.Hour),
	)
	if got != (RetentionProxies{}) {
		t.Fatalf("nil ids: %#v", got)
	}
}
