package dto_test

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
)

func TestEvaluateStreakView_SoftExpire(t *testing.T) {
	today := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	ptr := func(d time.Time) *time.Time { return &d }

	tests := []struct {
		name      string
		row       domain.Streak
		wantEff   int
		wantRisk  bool
		wantProt  bool
		wantDays  int
	}{
		{
			name: "checked today maintaining",
			row: domain.Streak{
				UserID:          uuid.New(),
				CurrentStreak:   5,
				LastCheckInDate: ptr(today),
			},
			wantEff:  5,
			wantRisk: false,
			wantDays: 0,
		},
		{
			name: "yesterday at risk",
			row: domain.Streak{
				CurrentStreak:   5,
				LastCheckInDate: ptr(today.AddDate(0, 0, -1)),
			},
			wantEff:  5,
			wantRisk: true,
			wantDays: 1,
		},
		{
			name: "gap 2 unprotected no freezes expires",
			row: domain.Streak{
				CurrentStreak:    12,
				LastCheckInDate:  ptr(today.AddDate(0, 0, -2)),
				FreezesAvailable: 0,
			},
			wantEff:  0,
			wantRisk: false,
			wantDays: 2,
		},
		{
			name: "gap 2 with freezes pending auto-freeze stays at risk",
			row: domain.Streak{
				CurrentStreak:    12,
				LastCheckInDate:  ptr(today.AddDate(0, 0, -2)),
				FreezesAvailable: 1,
			},
			wantEff:  12,
			wantRisk: true,
			wantDays: 2,
		},
		{
			name: "gap 2 with protected yesterday stays at risk",
			row: domain.Streak{
				CurrentStreak:   12,
				LastCheckInDate: ptr(today.AddDate(0, 0, -2)),
				ProtectedUntil:  ptr(today.AddDate(0, 0, -1)),
			},
			wantEff:  12,
			wantRisk: true,
			wantDays: 2,
		},
		{
			name: "gap 5 unprotected expires",
			row: domain.Streak{
				CurrentStreak:   40,
				LastCheckInDate: ptr(today.AddDate(0, 0, -5)),
			},
			wantEff:  0,
			wantRisk: false,
			wantDays: 5,
		},
		{
			name: "gap 5 with freezes still expires",
			row: domain.Streak{
				CurrentStreak:    40,
				LastCheckInDate:  ptr(today.AddDate(0, 0, -5)),
				FreezesAvailable: 1,
			},
			wantEff:  0,
			wantRisk: false,
			wantDays: 5,
		},
		{
			name: "today protected proactive",
			row: domain.Streak{
				CurrentStreak:   4,
				LastCheckInDate: ptr(today.AddDate(0, 0, -1)),
				ProtectedUntil:  ptr(today),
			},
			wantEff:  4,
			wantRisk: false,
			wantProt: true,
			wantDays: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := dto.EvaluateStreakView(&tt.row, today)
			if v.EffectiveStreak != tt.wantEff {
				t.Fatalf("EffectiveStreak=%d want %d", v.EffectiveStreak, tt.wantEff)
			}
			if v.IsAtRisk != tt.wantRisk {
				t.Fatalf("IsAtRisk=%v want %v", v.IsAtRisk, tt.wantRisk)
			}
			if v.IsProtected != tt.wantProt {
				t.Fatalf("IsProtected=%v want %v", v.IsProtected, tt.wantProt)
			}
			if v.DaysSinceLastCheckIn == nil || *v.DaysSinceLastCheckIn != tt.wantDays {
				t.Fatalf("DaysSince=%v want %d", v.DaysSinceLastCheckIn, tt.wantDays)
			}
		})
	}
}

func TestNewStreakResponseAsOf_OmitsStaleProtectedUntil(t *testing.T) {
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	row := &domain.Streak{
		CurrentStreak:    5,
		LongestStreak:    5,
		LastCheckInDate:  &today,
		ProtectedUntil:   &yesterday, // stale leftover
		FreezesAvailable: 0,
	}
	res := dto.NewStreakResponseAsOf(row, today)
	if res.ProtectedUntil != nil {
		t.Fatalf("stale protected_until must be omitted, got %v", *res.ProtectedUntil)
	}
	if res.IsProtected {
		t.Fatal("stale protection must not set is_protected")
	}

	row.ProtectedUntil = &today
	res = dto.NewStreakResponseAsOf(row, today)
	if res.ProtectedUntil == nil || *res.ProtectedUntil != "2026-07-16" {
		t.Fatalf("active protected_until should be returned, got %v", res.ProtectedUntil)
	}
}

func TestNewStreakResponseAsOf_BackfillsFreezeDatesFromLast(t *testing.T) {
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	lastFreeze := today.AddDate(0, 0, -2)
	row := &domain.Streak{
		CurrentStreak:    3,
		LongestStreak:    3,
		LastCheckInDate:  &today,
		LastFreezeDate:   &lastFreeze,
		FreezesAvailable: 1,
	}
	res := dto.NewStreakResponseAsOf(row, today)
	if len(res.FreezeDates) != 1 || res.FreezeDates[0] != "2026-07-14" {
		t.Fatalf("FreezeDates=%v want [2026-07-14]", res.FreezeDates)
	}
}

func TestNewStreakResponseAsOf_SkipsPendingFreezeReservation(t *testing.T) {
	today := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	row := &domain.Streak{
		CurrentStreak:    3,
		LongestStreak:    3,
		LastCheckInDate:  &today,
		ProtectedUntil:   &today,
		LastFreezeDate:   &today, // UseFreeze reservation — not consumed yet
		FreezesAvailable: 0,
	}
	res := dto.NewStreakResponseAsOf(row, today)
	if len(res.FreezeDates) != 0 {
		t.Fatalf("pending reservation must not appear in freeze_dates, got %v", res.FreezeDates)
	}
	if res.LastFreezeDate == nil || *res.LastFreezeDate != "2026-07-16" {
		t.Fatalf("last_freeze_date still returned for badge logic, got %v", res.LastFreezeDate)
	}
}
