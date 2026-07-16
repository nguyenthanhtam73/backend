package dto

import (
	"encoding/json"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
)

// StreakResponse is returned by GET /api/v1/me/streak and POST /me/streak/freeze.
// Admin reconcile returns this nested under AdminStreakReconcileResponse.
//
// current_streak is the *effective* value after soft-expire on read: if the
// user has missed enough unprotected days, it is 0 even when the DB row still
// holds the old counter (until the next check-in resets it).
type StreakResponse struct {
	CurrentStreak        int     `json:"current_streak"`
	LongestStreak        int     `json:"longest_streak"`
	LastCheckInDate      *string `json:"last_check_in_date,omitempty"` // YYYY-MM-DD (VN calendar)
	// FirstCheckInDate is MIN(skin_checks.check_date) — the day the user started
	// using the app. Clients use this so mini-history does not mark earlier days
	// as "missed".
	FirstCheckInDate     *string `json:"first_check_in_date,omitempty"` // YYYY-MM-DD (VN calendar)
	ProtectedUntil       *string `json:"protected_until,omitempty"`    // YYYY-MM-DD active only
	// LastFreezeDate is the day most recently covered by a freeze (auto/manual).
	// Always returned when set — including past days — for mini-history.
	LastFreezeDate       *string  `json:"last_freeze_date,omitempty"` // YYYY-MM-DD
	// FreezeDates lists recent freeze-covered days for mini-history (oldest→newest).
	FreezeDates          []string `json:"freeze_dates,omitempty"`
	FreezesAvailable     int      `json:"freezes_available"`
	IsAtRisk             bool     `json:"is_at_risk"`
	IsProtected          bool     `json:"is_protected"`
	DaysSinceLastCheckIn *int     `json:"days_since_last_check_in,omitempty"`
}

// AdminStreakReconcileResponse is POST /api/v1/admin/users/:userId/streak/reconcile.
//
// Reconcile is an admin-only repair tool for streak counters that drifted from
// SkinCheck history. It must never be exposed as a self-serve user action
// (that would enable freeze refill abuse).
//
// Replay does not seed freezes: historical 1-day gaps are NOT auto-bridged, so
// reconstructed streaks stay honest for users who had already spent freezes.
type AdminStreakReconcileResponse struct {
	Message               string         `json:"message"`
	UserID                string         `json:"user_id"`
	DaysReplayed          int            `json:"days_replayed"`
	FreezesPreserved      bool           `json:"freezes_preserved"`
	FreezeBridgesInvented bool           `json:"freeze_bridges_invented"`
	Note                  string         `json:"note,omitempty"`
	Before                StreakResponse `json:"before"`
	After                 StreakResponse `json:"after"`
}

// NewStreakResponse maps a domain streak row using Vietnam calendar "today".
func NewStreakResponse(row *domain.Streak) StreakResponse {
	return NewStreakResponseAsOf(row, streaktime.Today())
}

// NewStreakResponseAsOf evaluates soft-expire / risk flags as of today
// (normalized to calendar midnight). See EvaluateStreakView for rules.
//
// Stale ProtectedUntil (protected day < today) is omitted from the JSON even if
// still present on the domain row — callers should also persist a cleanup via
// Service.Get / applyCheckIn.
func NewStreakResponseAsOf(row *domain.Streak, todayCal time.Time) StreakResponse {
	if row == nil {
		return StreakResponse{
			FreezesAvailable: domain.DefaultFreezesAvailable,
		}
	}
	today := utcDate(todayCal)
	view := EvaluateStreakView(row, today)
	return StreakResponse{
		CurrentStreak:        view.EffectiveStreak,
		LongestStreak:        row.LongestStreak,
		LastCheckInDate:      formatDatePtr(row.LastCheckInDate),
		ProtectedUntil:       formatActiveProtectedUntil(row.ProtectedUntil, today),
		LastFreezeDate:       formatDatePtr(row.LastFreezeDate),
		FreezeDates:          freezeDatesForResponse(row),
		FreezesAvailable:     row.FreezesAvailable,
		IsAtRisk:             view.IsAtRisk,
		IsProtected:          view.IsProtected,
		DaysSinceLastCheckIn: view.DaysSinceLastCheckIn,
	}
}

// freezeDatesForResponse returns persisted freeze days.
//
// LastFreezeDate is backfilled only when it is NOT an active reservation
// (ProtectedUntil still equals that day). Pending UseFreeze must not appear in
// freeze_dates until applyCheckIn consumes the bridge.
func freezeDatesForResponse(row *domain.Streak) []string {
	dates := decodeFreezeDates(row.FreezeDates)
	if row.LastFreezeDate == nil {
		return dates
	}
	last := row.LastFreezeDate.UTC().Format("2006-01-02")
	if row.ProtectedUntil != nil && utcDate(*row.ProtectedUntil).Format("2006-01-02") == last {
		return dates
	}
	for _, d := range dates {
		if d == last {
			return dates
		}
	}
	return append(dates, last)
}

func decodeFreezeDates(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var dates []string
	if err := json.Unmarshal(raw, &dates); err != nil || len(dates) == 0 {
		return nil
	}
	return dates
}

// StreakView is the read-time interpretation of a streak row (soft-expire).
type StreakView struct {
	EffectiveStreak      int
	IsAtRisk             bool
	IsProtected          bool
	DaysSinceLastCheckIn *int
}

// EvaluateStreakView applies soft-expire rules for display.
//
// Soft-expire (read path only — DB row is unchanged until next check-in):
//
//  1. No last check-in / stored streak 0 → effective 0, not at risk.
//  2. days_since == 0 (checked today) → keep streak; at_risk false;
//     is_protected if ProtectedUntil covers today or later.
//  3. days_since == 1 → keep streak, at_risk true (unless already protected today).
//  4. days_since == 2 (missed exactly one calendar day):
//     - ProtectedUntil == yesterday → bridged miss; keep streak, at_risk.
//     - FreezesAvailable > 0 → pending auto-freeze on next check-in; keep
//       streak, at_risk (Option A — do NOT soft-expire; matches applyCheckIn).
//     - Else → soft-expire to effective 0.
//  5. days_since > 2:
//     - If ProtectedUntil covers today+, keep streak + is_protected.
//     - Otherwise soft-expire → effective 0.
func EvaluateStreakView(row *domain.Streak, todayUTC time.Time) StreakView {
	today := utcDate(todayUTC)
	if row == nil || row.LastCheckInDate == nil || row.CurrentStreak <= 0 {
		return StreakView{}
	}

	last := utcDate(*row.LastCheckInDate)
	daysSince := calendarDaysBetween(last, today)
	ds := daysSince
	view := StreakView{
		EffectiveStreak:      row.CurrentStreak,
		DaysSinceLastCheckIn: &ds,
	}

	yesterday := today.AddDate(0, 0, -1)
	// Active protection: ProtectedUntil >= today (compare to calendar day, never
	// treat a mere non-nil pointer as "protected").
	protectedUntilTodayOrLater := row.ProtectedUntil != nil &&
		!utcDate(*row.ProtectedUntil).Before(today)
	// Freeze that already covered the one missed day between last and today.
	protectedMissDay := row.ProtectedUntil != nil &&
		sameUTCDate(*row.ProtectedUntil, yesterday) &&
		daysSince == 2
	// One full day missed and a freeze remains — check-in will auto-freeze.
	pendingAutoFreeze := daysSince == 2 && row.FreezesAvailable > 0

	switch {
	case daysSince <= 0:
		view.IsProtected = protectedUntilTodayOrLater
		view.IsAtRisk = false

	case daysSince == 1:
		// Missed today so far — streak still alive until end of day.
		if protectedUntilTodayOrLater {
			view.IsProtected = true
			view.IsAtRisk = false
		} else {
			view.IsAtRisk = true
		}

	case protectedMissDay:
		// last = day-2, ProtectedUntil = yesterday → freeze bridged the gap;
		// today is the catch-up day (same as at_risk after a one-day miss).
		view.IsAtRisk = true
		view.IsProtected = false

	case pendingAutoFreeze:
		// Option A: align GET with applyCheckIn auto-freeze. Keep the streak
		// visible and mark at_risk so the user knows to check in today.
		view.IsAtRisk = true
		view.IsProtected = false

	case protectedUntilTodayOrLater:
		view.IsProtected = true
		view.IsAtRisk = false

	default:
		// Soft-expire: gap too large and no freeze can save this check-in.
		view.EffectiveStreak = 0
		view.IsAtRisk = false
		view.IsProtected = false
	}

	return view
}

func formatDatePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format("2006-01-02")
	return &s
}

// formatActiveProtectedUntil omits expired protection (pu < today) from the API
// so clients never see a stale ProtectedUntil. pu == today is still active.
func formatActiveProtectedUntil(pu *time.Time, today time.Time) *string {
	if pu == nil {
		return nil
	}
	if utcDate(*pu).Before(utcDate(today)) {
		return nil
	}
	return formatDatePtr(pu)
}

func utcDate(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func sameUTCDate(a, b time.Time) bool {
	return utcDate(a).Equal(utcDate(b))
}

func calendarDaysBetween(from, to time.Time) int {
	a := utcDate(from)
	b := utcDate(to)
	if b.Before(a) {
		return 0
	}
	return int(b.Sub(a) / (24 * time.Hour))
}
