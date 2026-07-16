package streak

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
)

func TestApplyCheckIn_Branches(t *testing.T) {
	uid := uuid.New()
	day := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	yesterday := day.AddDate(0, 0, -1)
	twoDaysAgo := day.AddDate(0, 0, -2)
	threeDaysAgo := day.AddDate(0, 0, -3)

	ptr := func(t time.Time) *time.Time { return &t }

	tests := []struct {
		name             string
		before           domain.Streak
		wantCurrent      int
		wantLongest      int
		wantFreezes      int
		wantLast         time.Time
		wantProtectedSet bool
		wantAutoFreeze   bool
	}{
		{
			name: "first check-in",
			before: domain.Streak{
				UserID:           uid,
				FreezesAvailable: 1,
			},
			wantCurrent: 1,
			wantLongest: 1,
			wantFreezes: 1,
			wantLast:    day,
		},
		{
			name: "same day idempotent",
			before: domain.Streak{
				UserID:           uid,
				CurrentStreak:    3,
				LongestStreak:    5,
				LastCheckInDate:  ptr(day),
				FreezesAvailable: 1,
			},
			wantCurrent: 3,
			wantLongest: 5,
			wantFreezes: 1,
			wantLast:    day,
		},
		{
			name: "continue consecutive day",
			before: domain.Streak{
				UserID:           uid,
				CurrentStreak:    4,
				LongestStreak:    4,
				LastCheckInDate:  ptr(yesterday),
				FreezesAvailable: 1,
			},
			wantCurrent: 5,
			wantLongest: 5,
			wantFreezes: 1,
			wantLast:    day,
		},
		{
			name: "gap one day with freeze",
			before: domain.Streak{
				UserID:           uid,
				CurrentStreak:    4,
				LongestStreak:    6,
				LastCheckInDate:  ptr(twoDaysAgo),
				FreezesAvailable: 1,
			},
			wantCurrent:      5,
			wantLongest:      6,
			wantFreezes:      0,
			wantLast:         day,
			wantProtectedSet: false, // auto-freeze consumes yesterday then clears (not stale)
			wantAutoFreeze:   true,
		},
		{
			name: "gap one day without freeze resets",
			before: domain.Streak{
				UserID:           uid,
				CurrentStreak:    4,
				LongestStreak:    6,
				LastCheckInDate:  ptr(twoDaysAgo),
				FreezesAvailable: 0,
			},
			wantCurrent: 1,
			wantLongest: 6,
			wantFreezes: 0,
			wantLast:    day,
		},
		{
			name: "gap two plus days resets",
			before: domain.Streak{
				UserID:           uid,
				CurrentStreak:    10,
				LongestStreak:    10,
				LastCheckInDate:  ptr(threeDaysAgo),
				FreezesAvailable: 1,
			},
			wantCurrent: 1,
			wantLongest: 10,
			wantFreezes: 1,
			wantLast:    day,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := tt.before
			outcome := applyCheckIn(&row, day)

			if outcome.AutoFreezeApplied != tt.wantAutoFreeze {
				t.Fatalf("AutoFreezeApplied=%v want %v", outcome.AutoFreezeApplied, tt.wantAutoFreeze)
			}
			if row.CurrentStreak != tt.wantCurrent {
				t.Fatalf("CurrentStreak=%d want %d", row.CurrentStreak, tt.wantCurrent)
			}
			if row.LongestStreak != tt.wantLongest {
				t.Fatalf("LongestStreak=%d want %d", row.LongestStreak, tt.wantLongest)
			}
			if row.FreezesAvailable != tt.wantFreezes {
				t.Fatalf("FreezesAvailable=%d want %d", row.FreezesAvailable, tt.wantFreezes)
			}
			if row.LastCheckInDate == nil || !sameUTCDate(*row.LastCheckInDate, tt.wantLast) {
				t.Fatalf("LastCheckInDate=%v want %v", row.LastCheckInDate, tt.wantLast)
			}
			if tt.wantProtectedSet {
				if row.ProtectedUntil == nil || !sameUTCDate(*row.ProtectedUntil, yesterday) {
					t.Fatalf("ProtectedUntil=%v want missed day %v", row.ProtectedUntil, yesterday)
				}
			} else if row.ProtectedUntil != nil {
				t.Fatalf("ProtectedUntil should be nil for %q, got %v", tt.name, row.ProtectedUntil)
			}
		})
	}
}

func TestApplyCheckIn_ContinuePreservesLongest(t *testing.T) {
	yesterday := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	day := yesterday.AddDate(0, 0, 1)
	row := domain.Streak{
		CurrentStreak:    2,
		LongestStreak:    10,
		LastCheckInDate:  &yesterday,
		FreezesAvailable: 1,
	}
	applyCheckIn(&row, day)
	if row.CurrentStreak != 3 {
		t.Fatalf("current=%d", row.CurrentStreak)
	}
	if row.LongestStreak != 10 {
		t.Fatalf("longest should stay 10, got %d", row.LongestStreak)
	}
}

func TestReconcileReplay_MatchesIncremental(t *testing.T) {
	// Sparse history without inventing freezes: d1, gap, d3 → reset at d3; then d10.
	d1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	d10 := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)

	inc := domain.Streak{FreezesAvailable: 0}
	for _, d := range []time.Time{d1, d3, d10} {
		applyCheckIn(&inc, d)
	}

	rec := domain.Streak{FreezesAvailable: 0}
	replayFromCheckDates(&rec, []time.Time{d1, d3, d10})

	if inc.CurrentStreak != rec.CurrentStreak ||
		inc.LongestStreak != rec.LongestStreak {
		t.Fatalf("replay mismatch: inc=%+v rec=%+v", inc, rec)
	}
	if inc.LastCheckInDate == nil || rec.LastCheckInDate == nil ||
		!sameUTCDate(*inc.LastCheckInDate, *rec.LastCheckInDate) {
		t.Fatalf("last date mismatch")
	}
	if inc.CurrentStreak != 1 || inc.LongestStreak != 1 {
		t.Fatalf("without freezes, gaps must reset: current=%d longest=%d", inc.CurrentStreak, inc.LongestStreak)
	}
}

func TestReplayFromCheckDates_PreservesFreezeInventory(t *testing.T) {
	// User already spent their freeze (0 left). Reconcile must NOT invent a
	// freeze bridge for d1→d3 (that would wrongly yield current=2).
	d1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	protected := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	lastFreeze := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

	row := domain.Streak{
		CurrentStreak:    99, // corrupted
		LongestStreak:    99,
		FreezesAvailable: 0,
		ProtectedUntil:   &protected,
		LastFreezeDate:   &lastFreeze,
	}
	replayFromCheckDates(&row, []time.Time{d1, d3})

	if row.FreezesAvailable != 0 {
		t.Fatalf("reconcile must not refill freezes, got %d", row.FreezesAvailable)
	}
	if row.ProtectedUntil == nil || !sameUTCDate(*row.ProtectedUntil, protected) {
		t.Fatalf("protected_until must be preserved, got %v", row.ProtectedUntil)
	}
	if row.LastFreezeDate == nil || !sameUTCDate(*row.LastFreezeDate, lastFreeze) {
		t.Fatalf("last_freeze_date must be preserved, got %v", row.LastFreezeDate)
	}
	// Gap d1→d3 with no seeded freeze → streak resets at d3.
	if row.CurrentStreak != 1 {
		t.Fatalf("current=%d want 1 (no invented freeze bridge)", row.CurrentStreak)
	}
	if row.LongestStreak != 1 {
		t.Fatalf("longest=%d want 1", row.LongestStreak)
	}
}

func TestReplayFromCheckDates_DoesNotInventBridgeWhenUserHadFreezes(t *testing.T) {
	// Even if the live inventory still shows 1 freeze, replay must not spend it
	// (or seed extras) to rewrite past gaps — only consecutive days continue.
	d1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	d3 := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)

	row := domain.Streak{
		CurrentStreak:    50,
		LongestStreak:    50,
		FreezesAvailable: 1,
	}
	replayFromCheckDates(&row, []time.Time{d1, d3})

	if row.FreezesAvailable != 1 {
		t.Fatalf("freezes must stay 1, got %d", row.FreezesAvailable)
	}
	if row.CurrentStreak != 1 {
		t.Fatalf("must not invent bridge: current=%d want 1", row.CurrentStreak)
	}
}

func TestApplyUseFreeze_ProtectTomorrowWhenCheckedToday(t *testing.T) {
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	row := domain.Streak{
		CurrentStreak:    3,
		LongestStreak:    3,
		LastCheckInDate:  &today,
		FreezesAvailable: 1,
	}
	if err := applyUseFreeze(&row, today); err != nil {
		t.Fatal(err)
	}
	if row.FreezesAvailable != 0 {
		t.Fatalf("freezes=%d", row.FreezesAvailable)
	}
	tomorrow := today.AddDate(0, 0, 1)
	if row.ProtectedUntil == nil || !sameUTCDate(*row.ProtectedUntil, tomorrow) {
		t.Fatalf("protected=%v want %v", row.ProtectedUntil, tomorrow)
	}
	if row.LastFreezeDate == nil || !sameUTCDate(*row.LastFreezeDate, tomorrow) {
		t.Fatalf("LastFreezeDate=%v want tomorrow (reserved)", row.LastFreezeDate)
	}
	if len(row.FreezeDates) > 0 {
		t.Fatalf("FreezeDates must stay empty until consume, got %s", string(row.FreezeDates))
	}
}

func TestApplyUseFreeze_RejectsSoftExpired(t *testing.T) {
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	twoDaysAgo := today.AddDate(0, 0, -2)
	row := domain.Streak{
		CurrentStreak:    12, // DB counter still high
		LongestStreak:    12,
		LastCheckInDate:  &twoDaysAgo,
		FreezesAvailable: 0, // no pending auto-freeze → soft-expired
	}
	err := applyUseFreeze(&row, today)
	if !errors.Is(err, ErrSoftExpired) {
		t.Fatalf("err=%v want ErrSoftExpired", err)
	}
	if row.FreezesAvailable != 0 {
		t.Fatalf("must not change freezes, got %d", row.FreezesAvailable)
	}
	if row.ProtectedUntil != nil {
		t.Fatalf("must not set ProtectedUntil, got %v", row.ProtectedUntil)
	}
}

func TestApplyUseFreeze_RejectsPendingAutoFreezeCatchUp(t *testing.T) {
	// Gap = 1 full day + freezes left: streak still alive (Option A) but manual
	// freeze is the wrong tool — user must check in for auto-freeze.
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	twoDaysAgo := today.AddDate(0, 0, -2)
	row := domain.Streak{
		CurrentStreak:    12,
		LastCheckInDate:  &twoDaysAgo,
		FreezesAvailable: 1,
	}
	err := applyUseFreeze(&row, today)
	if !errors.Is(err, ErrCatchUpRequired) {
		t.Fatalf("err=%v want ErrCatchUpRequired", err)
	}
	if row.FreezesAvailable != 1 || row.ProtectedUntil != nil {
		t.Fatalf("must not spend freeze: freezes=%d pu=%v", row.FreezesAvailable, row.ProtectedUntil)
	}
}

func TestApplyUseFreeze_RejectsLongerGapSoftExpired(t *testing.T) {
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	fiveDaysAgo := today.AddDate(0, 0, -5)
	row := domain.Streak{
		CurrentStreak:    40,
		LastCheckInDate:  &fiveDaysAgo,
		FreezesAvailable: 1,
	}
	if err := applyUseFreeze(&row, today); !errors.Is(err, ErrSoftExpired) {
		t.Fatalf("err=%v want ErrSoftExpired", err)
	}
}

func TestApplyUseFreeze_AllowsAtRiskYesterday(t *testing.T) {
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	row := domain.Streak{
		CurrentStreak:    5,
		LongestStreak:    5,
		LastCheckInDate:  &yesterday,
		FreezesAvailable: 1,
	}
	if err := applyUseFreeze(&row, today); err != nil {
		t.Fatal(err)
	}
	if row.ProtectedUntil == nil || !sameUTCDate(*row.ProtectedUntil, today) {
		t.Fatalf("at-risk freeze should protect today, got %v", row.ProtectedUntil)
	}
	if row.LastFreezeDate == nil || !sameUTCDate(*row.LastFreezeDate, today) {
		t.Fatalf("LastFreezeDate=%v want today (reserved)", row.LastFreezeDate)
	}
	if len(row.FreezeDates) > 0 {
		t.Fatalf("FreezeDates must stay empty until consume, got %s", string(row.FreezeDates))
	}
}

func TestApplyUseFreeze_RejectsBridgedMissCatchUp(t *testing.T) {
	// days_since == 2 with ProtectedUntil == yesterday → still alive; must check in.
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	twoDaysAgo := today.AddDate(0, 0, -2)
	yesterday := today.AddDate(0, 0, -1)
	row := domain.Streak{
		CurrentStreak:    8,
		LastCheckInDate:  &twoDaysAgo,
		ProtectedUntil:   &yesterday,
		FreezesAvailable: 1,
	}
	if err := applyUseFreeze(&row, today); !errors.Is(err, ErrCatchUpRequired) {
		t.Fatalf("err=%v want ErrCatchUpRequired", err)
	}
}

func TestApplyCheckIn_AutoFreezeSetsLastFreezeDate(t *testing.T) {
	day := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	twoDaysAgo := day.AddDate(0, 0, -2)
	yesterday := day.AddDate(0, 0, -1)
	row := domain.Streak{
		CurrentStreak:    4,
		LongestStreak:    4,
		LastCheckInDate:  &twoDaysAgo,
		FreezesAvailable: 1,
	}
	outcome := applyCheckIn(&row, day)
	if !outcome.AutoFreezeApplied {
		t.Fatal("expected auto-freeze")
	}
	if row.ProtectedUntil != nil {
		t.Fatalf("ProtectedUntil should be cleared after catch-up, got %v", row.ProtectedUntil)
	}
	if row.LastFreezeDate == nil || !sameUTCDate(*row.LastFreezeDate, yesterday) {
		t.Fatalf("LastFreezeDate=%v want yesterday %v", row.LastFreezeDate, yesterday)
	}
}

func TestApplyCheckIn_HonorsPreActivatedFreeze(t *testing.T) {
	day := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	twoDaysAgo := day.AddDate(0, 0, -2)
	yesterday := day.AddDate(0, 0, -1)
	row := domain.Streak{
		CurrentStreak:    4,
		LongestStreak:    4,
		LastCheckInDate:  &twoDaysAgo,
		ProtectedUntil:   &yesterday,
		FreezesAvailable: 0, // already spent on proactive freeze
	}
	applyCheckIn(&row, day)
	if row.CurrentStreak != 5 {
		t.Fatalf("current=%d want 5", row.CurrentStreak)
	}
	if row.FreezesAvailable != 0 {
		t.Fatalf("should not consume another freeze")
	}
	if row.ProtectedUntil != nil {
		t.Fatalf("ProtectedUntil should be cleared after catch-up check-in, got %v", row.ProtectedUntil)
	}
	if row.LastFreezeDate == nil || !sameUTCDate(*row.LastFreezeDate, yesterday) {
		t.Fatalf("LastFreezeDate should be set from honored freeze, got %v", row.LastFreezeDate)
	}
}

func TestApplyCheckIn_ClearsStaleProtectedUntilOnContinue(t *testing.T) {
	day := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	yesterday := day.AddDate(0, 0, -1)
	stale := yesterday // leftover from a prior auto/manual freeze
	row := domain.Streak{
		CurrentStreak:    3,
		LongestStreak:    3,
		LastCheckInDate:  &yesterday,
		ProtectedUntil:   &stale,
		FreezesAvailable: 0,
	}
	applyCheckIn(&row, day)
	if row.CurrentStreak != 4 {
		t.Fatalf("current=%d", row.CurrentStreak)
	}
	if row.ProtectedUntil != nil {
		t.Fatalf("stale ProtectedUntil should be cleared, got %v", row.ProtectedUntil)
	}
}

func TestApplyCheckIn_KeepsFutureProtectedUntil(t *testing.T) {
	// Edge: last check-in somehow still "today" path isn't consecutive; simulate
	// continue from yesterday while a freeze already covers tomorrow — rare, but
	// clearConsumed must not wipe future protection.
	day := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	yesterday := day.AddDate(0, 0, -1)
	tomorrow := day.AddDate(0, 0, 1)
	row := domain.Streak{
		CurrentStreak:    2,
		LongestStreak:    2,
		LastCheckInDate:  &yesterday,
		ProtectedUntil:   &tomorrow,
		FreezesAvailable: 0,
	}
	applyCheckIn(&row, day)
	if row.ProtectedUntil == nil || !sameUTCDate(*row.ProtectedUntil, tomorrow) {
		t.Fatalf("future ProtectedUntil must be kept, got %v", row.ProtectedUntil)
	}
}

func TestClearExpiredProtectedUntil(t *testing.T) {
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	row := domain.Streak{ProtectedUntil: &yesterday}
	if !clearExpiredProtectedUntil(&row, today) || row.ProtectedUntil != nil {
		t.Fatal("expected expired ProtectedUntil cleared")
	}
	row.ProtectedUntil = &today
	if clearExpiredProtectedUntil(&row, today) || row.ProtectedUntil == nil {
		t.Fatal("ProtectedUntil == today must stay active")
	}
}

func TestIsCatchUpFreezeBridge(t *testing.T) {
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	twoDaysAgo := today.AddDate(0, 0, -2)
	threeDaysAgo := today.AddDate(0, 0, -3)

	bridge := domain.Streak{
		LastCheckInDate: &twoDaysAgo,
		ProtectedUntil:  &yesterday,
	}
	if !isCatchUpFreezeBridge(&bridge, today) {
		t.Fatal("last=day-2 + PU=yesterday must be preserved as catch-up bridge")
	}

	// Already checked yesterday — PU=yesterday is stale leftover, not a bridge.
	stale := domain.Streak{
		LastCheckInDate: &yesterday,
		ProtectedUntil:  &yesterday,
	}
	if isCatchUpFreezeBridge(&stale, today) {
		t.Fatal("should not treat consecutive leftover as catch-up bridge")
	}

	// Gap too large — freeze day is not yesterday relative to last.
	old := domain.Streak{
		LastCheckInDate: &threeDaysAgo,
		ProtectedUntil:  &yesterday,
	}
	if isCatchUpFreezeBridge(&old, today) {
		t.Fatal("PU=yesterday with last=day-3 is not the honor-freeze bridge")
	}
}

func TestCatchUpBridgeSurvivesGetStyleClear(t *testing.T) {
	// Mirrors persistClearExpiredProtection decision: bridge must not be wiped
	// so EvaluateStreakView still sees protectedMissDay and applyCheckIn can honor it.
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	twoDaysAgo := today.AddDate(0, 0, -2)
	row := domain.Streak{
		CurrentStreak:    8,
		LastCheckInDate:  &twoDaysAgo,
		ProtectedUntil:   &yesterday,
		FreezesAvailable: 0,
	}

	if isCatchUpFreezeBridge(&row, today) {
		// Would skip clear on GET — leave PU intact.
	} else {
		t.Fatal("expected catch-up bridge")
	}

	view := dto.EvaluateStreakView(&row, today)
	if view.EffectiveStreak != 8 || !view.IsAtRisk {
		t.Fatalf("bridge must keep streak alive: eff=%d at_risk=%v", view.EffectiveStreak, view.IsAtRisk)
	}

	// Catch-up check-in still honors the freeze.
	applyCheckIn(&row, today)
	if row.CurrentStreak != 9 {
		t.Fatalf("current=%d want 9 after honor", row.CurrentStreak)
	}
	if row.ProtectedUntil != nil {
		t.Fatalf("PU should clear after catch-up, got %v", row.ProtectedUntil)
	}
}

func TestStaleProtectedUntilClearedWhenNotBridge(t *testing.T) {
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	// Checked in yesterday; leftover PU=yesterday is safe to clear on GET.
	row := domain.Streak{
		CurrentStreak:   3,
		LastCheckInDate: &yesterday,
		ProtectedUntil:  &yesterday,
	}
	if isCatchUpFreezeBridge(&row, today) {
		t.Fatal("not a bridge")
	}
	if !clearExpiredProtectedUntil(&row, today) || row.ProtectedUntil != nil {
		t.Fatal("stale non-bridge PU should clear")
	}
}

func TestRecordFreezeDay_BackfillsLastFreezeDate(t *testing.T) {
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	oldFreeze := today.AddDate(0, 0, -5)
	newFreeze := today.AddDate(0, 0, -1)
	row := domain.Streak{LastFreezeDate: &oldFreeze}
	recordFreezeDay(&row, newFreeze)

	if row.LastFreezeDate == nil || !sameUTCDate(*row.LastFreezeDate, newFreeze) {
		t.Fatalf("LastFreezeDate=%v", row.LastFreezeDate)
	}
	var dates []string
	if err := json.Unmarshal(row.FreezeDates, &dates); err != nil {
		t.Fatal(err)
	}
	if len(dates) != 2 || dates[0] != "2026-07-11" || dates[1] != "2026-07-15" {
		t.Fatalf("dates=%v want [2026-07-11 2026-07-15]", dates)
	}
}

func TestRecordFreezeDay_CorruptJSONStillSeedsLast(t *testing.T) {
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	oldFreeze := today.AddDate(0, 0, -3)
	newFreeze := today.AddDate(0, 0, -1)
	row := domain.Streak{
		LastFreezeDate: &oldFreeze,
		FreezeDates:    json.RawMessage(`{not-json`),
	}
	recordFreezeDay(&row, newFreeze)
	var dates []string
	if err := json.Unmarshal(row.FreezeDates, &dates); err != nil {
		t.Fatal(err)
	}
	if len(dates) != 2 || dates[0] != "2026-07-13" || dates[1] != "2026-07-15" {
		t.Fatalf("dates=%v", dates)
	}
}

func TestRecordFreezeDay_Idempotent(t *testing.T) {
	day := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	row := domain.Streak{}
	recordFreezeDay(&row, day)
	recordFreezeDay(&row, day)
	var dates []string
	_ = json.Unmarshal(row.FreezeDates, &dates)
	if len(dates) != 1 || dates[0] != "2026-07-15" {
		t.Fatalf("dates=%v", dates)
	}
}

func TestApplyCheckIn_AutoFreezeWritesFreezeDates(t *testing.T) {
	day := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	twoDaysAgo := day.AddDate(0, 0, -2)
	yesterday := day.AddDate(0, 0, -1)
	oldFreeze := day.AddDate(0, 0, -10)
	row := domain.Streak{
		CurrentStreak:    4,
		LongestStreak:    4,
		LastCheckInDate:  &twoDaysAgo,
		FreezesAvailable: 1,
		LastFreezeDate:   &oldFreeze,
	}
	applyCheckIn(&row, day)
	var dates []string
	if err := json.Unmarshal(row.FreezeDates, &dates); err != nil {
		t.Fatal(err)
	}
	if len(dates) != 2 || dates[0] != "2026-07-06" || dates[1] != yesterday.Format("2006-01-02") {
		t.Fatalf("dates=%v", dates)
	}
}

func TestApplyUseFreeze_ThenHonorRecordsFreezeDates(t *testing.T) {
	// Spend freeze for today while at risk; next day catch-up should append history.
	day1 := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	yesterdayOfDay1 := day1.AddDate(0, 0, -1)
	row := domain.Streak{
		CurrentStreak:    3,
		LongestStreak:    3,
		LastCheckInDate:  &yesterdayOfDay1,
		FreezesAvailable: 1,
	}
	if err := applyUseFreeze(&row, day1); err != nil {
		t.Fatal(err)
	}
	if len(row.FreezeDates) > 0 {
		t.Fatalf("no FreezeDates until consume")
	}
	day2 := day1.AddDate(0, 0, 1)
	applyCheckIn(&row, day2)
	var dates []string
	_ = json.Unmarshal(row.FreezeDates, &dates)
	if len(dates) != 1 || dates[0] != day1.Format("2006-01-02") {
		t.Fatalf("dates=%v want [%s]", dates, day1.Format("2006-01-02"))
	}
}

func TestApplyCheckIn_RevertsUnusedFreezeWhenCheckedIn(t *testing.T) {
	// Protect today while at risk, then check in same day — freeze spent but
	// not a miss; LastFreezeDate reservation must clear (not in FreezeDates).
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	row := domain.Streak{
		CurrentStreak:    3,
		LongestStreak:    3,
		LastCheckInDate:  &yesterday,
		FreezesAvailable: 1,
	}
	if err := applyUseFreeze(&row, today); err != nil {
		t.Fatal(err)
	}
	if row.LastFreezeDate == nil {
		t.Fatal("expected reservation LastFreezeDate")
	}
	out := applyCheckIn(&row, today)
	if !out.UnusedFreezeCleared {
		t.Fatal("expected UnusedFreezeCleared")
	}
	if row.LastFreezeDate != nil {
		t.Fatalf("unused reservation should clear, got %v", row.LastFreezeDate)
	}
	if len(row.FreezeDates) > 0 {
		t.Fatalf("FreezeDates must stay empty, got %s", string(row.FreezeDates))
	}
	if row.FreezesAvailable != 0 {
		t.Fatalf("inventory stays spent, got %d", row.FreezesAvailable)
	}
}

func TestCommitExpiredFreezeHistory_PreservesManualMiss(t *testing.T) {
	day := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	last := day.AddDate(0, 0, -5)
	freezeDay := day.AddDate(0, 0, -3)
	row := domain.Streak{
		CurrentStreak:    10,
		LastCheckInDate:  &last,
		LastFreezeDate:   &freezeDay,
		FreezesAvailable: 0,
	}
	commitExpiredFreezeHistory(&row, day)
	if !freezeDateRecorded(&row, freezeDay) {
		t.Fatalf("expected freeze day recorded, dates=%s", string(row.FreezeDates))
	}
}

func TestApplyCheckIn_SoftExpireKeepsManualFreezeHistory(t *testing.T) {
	day := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	last := day.AddDate(0, 0, -5)      // Jul 15
	freezeDay := day.AddDate(0, 0, -3) // Jul 17 (reserved miss)
	row := domain.Streak{
		CurrentStreak:    10,
		LongestStreak:    10,
		LastCheckInDate:  &last,
		LastFreezeDate:   &freezeDay,
		FreezesAvailable: 0,
	}
	applyCheckIn(&row, day)
	var dates []string
	if err := json.Unmarshal(row.FreezeDates, &dates); err != nil {
		t.Fatal(err)
	}
	if len(dates) != 1 || dates[0] != "2026-07-17" {
		t.Fatalf("dates=%v want [2026-07-17]", dates)
	}
}

func TestShouldNotRecordFreezeWhenLastEqualsPU(t *testing.T) {
	// leftover last==PU must not become freeze history (checked-in day).
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	row := domain.Streak{
		CurrentStreak:   3,
		LastCheckInDate: &yesterday,
		ProtectedUntil:  &yesterday,
	}
	commitExpiredFreezeHistory(&row, today)
	if len(row.FreezeDates) > 0 {
		t.Fatalf("must not record freeze for last==PU leftover, got %s", string(row.FreezeDates))
	}
}

func TestCommitExpiredFreezeHistory_RecordsWhenLastBeforePU(t *testing.T) {
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	twoDaysAgo := today.AddDate(0, 0, -2)
	pu := today.AddDate(0, 0, -1) // miss day after last check-in
	row := domain.Streak{
		CurrentStreak:   3,
		LastCheckInDate: &twoDaysAgo,
		ProtectedUntil:  &pu,
	}
	commitExpiredFreezeHistory(&row, today)
	if !freezeDateRecorded(&row, pu) {
		t.Fatalf("expected miss day recorded, dates=%s", string(row.FreezeDates))
	}
}


func TestApplyCheckIn_HonorSetsCatchUpContinued(t *testing.T) {
	day := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	twoDaysAgo := day.AddDate(0, 0, -2)
	yesterday := day.AddDate(0, 0, -1)
	row := domain.Streak{
		CurrentStreak:    4,
		LongestStreak:    4,
		LastCheckInDate:  &twoDaysAgo,
		ProtectedUntil:   &yesterday,
		FreezesAvailable: 0,
	}
	out := applyCheckIn(&row, day)
	if !out.CatchUpContinued || out.AutoFreezeApplied {
		t.Fatalf("outcome=%+v", out)
	}
}

