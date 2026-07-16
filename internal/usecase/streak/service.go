package streak

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

var (
	ErrUnavailable      = errors.New("streak service unavailable")
	ErrNoFreezes        = errors.New("no streak freezes available")
	ErrAlreadyProtected = errors.New("a freeze is already active for the target day")
	ErrNoStreak         = errors.New("no active streak to protect")
	// ErrSoftExpired is returned when manual freeze is attempted after the streak
	// is already broken for display (miss ≥ 2 days without bridging protection).
	ErrSoftExpired = errors.New("streak already broken; cannot use a freeze")
	// ErrCatchUpRequired: streak is still alive only via pending auto-freeze /
	// yesterday bridge — user must check in today, not spend a manual freeze.
	ErrCatchUpRequired = errors.New("check in today to continue your streak")
)

// Service owns streak read/update rules (Asia/Ho_Chi_Minh civil calendar days).
type Service struct {
	store Store
	dates CheckDateSource // optional; required for ReconcileForUser
}

// NewService constructs Service. dates may be nil when reconcile is unused.
func NewService(store Store, dates CheckDateSource) *Service {
	return &Service{store: store, dates: dates}
}

// Get returns the caller's streak, creating a default row when none exists yet.
//
// Soft-expire: the JSON current_streak is evaluated at read time (see
// dto.EvaluateStreakView). A long gap without check-in / protection returns 0
// so the client never shows a "live" streak that is already broken. The DB row
// is left unchanged until the next RecordCheckIn — except truly stale
// ProtectedUntil values, which may be cleared on read (see
// persistClearExpiredProtection). Yesterday's catch-up bridge is preserved.
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (dto.StreakResponse, error) {
	var zero dto.StreakResponse
	if s == nil || s.store == nil {
		return zero, ErrUnavailable
	}
	row, err := s.store.GetByUserID(ctx, userID)
	if err != nil {
		return zero, err
	}
	if row != nil {
		row, err = s.persistClearExpiredProtection(ctx, userID, row)
		if err != nil {
			return zero, err
		}
		return s.streakResponse(ctx, userID, row), nil
	}
	// Lazy-create so the client always sees freezes_available defaults.
	created, err := s.store.UpdateAtomic(ctx, userID, func(row *domain.Streak) error {
		_ = row
		return nil
	})
	if err != nil {
		return zero, err
	}
	return s.streakResponse(ctx, userID, created), nil
}

// persistClearExpiredProtection writes NULL ProtectedUntil when the protected
// day is strictly before today AND it is not a live catch-up bridge.
//
// Catch-up bridge (must NOT clear on GET):
//
//	last_check_in = today-2, ProtectedUntil = yesterday
//
// That state means a manual freeze covered yesterday; the user still needs to
// check in today for applyCheckIn to honor it. Clearing here would soft-expire
// the streak and waste the freeze. Stale PU is still omitted from JSON via
// formatActiveProtectedUntil.
func (s *Service) persistClearExpiredProtection(
	ctx context.Context,
	userID uuid.UUID,
	row *domain.Streak,
) (*domain.Streak, error) {
	today := streaktime.Today()
	if row == nil || row.ProtectedUntil == nil {
		return row, nil
	}
	if !utcDate(*row.ProtectedUntil).Before(today) {
		return row, nil // still active (today or later)
	}
	if isCatchUpFreezeBridge(row, today) {
		return row, nil // keep bridge until catch-up check-in
	}
	updated, err := s.store.UpdateAtomic(ctx, userID, func(locked *domain.Streak) error {
		if isCatchUpFreezeBridge(locked, today) {
			return nil
		}
		if locked.ProtectedUntil == nil {
			return nil
		}
		pu := utcDate(*locked.ProtectedUntil)
		if !pu.Before(today) {
			return nil
		}
		// Only persist when PU covered a real miss (last check-in before PU).
		// Leftover last==PU (already checked in that day) must NOT become a
		// false freeze-history day in the mini strip.
		if locked.LastCheckInDate != nil && utcDate(*locked.LastCheckInDate).Before(pu) {
			recordFreezeDay(locked, pu)
		}
		locked.ProtectedUntil = nil
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// isCatchUpFreezeBridge reports whether ProtectedUntil is yesterday and the
// last check-in was the day before that — the overnight manual-freeze bridge
// that applyCheckIn honors on today's catch-up.
func isCatchUpFreezeBridge(row *domain.Streak, today time.Time) bool {
	if row == nil || row.ProtectedUntil == nil || row.LastCheckInDate == nil {
		return false
	}
	today = utcDate(today)
	yesterday := today.AddDate(0, 0, -1)
	if !sameUTCDate(*row.ProtectedUntil, yesterday) {
		return false
	}
	return sameUTCDate(*row.LastCheckInDate, today.AddDate(0, 0, -2))
}

// RecordCheckIn applies streak rules after a successful SkinCheck on checkDate.
//
// When ctx carries a DB transaction (repository.WithTx), the streak write joins
// that transaction so SkinCheck + Analysis + Streak commit or roll back together.
//
// Auto-freeze: if the user missed exactly one day and still has a freeze, the
// system spends it here (see applyCheckIn). That is distinct from UseFreeze
// (manual / proactive protection).
func (s *Service) RecordCheckIn(ctx context.Context, userID uuid.UUID, checkDate time.Time) (CheckInResult, error) {
	var zero CheckInResult
	if s == nil || s.store == nil {
		return zero, ErrUnavailable
	}
	day := utcDate(checkDate)

	var outcome CheckInOutcome
	row, err := s.store.UpdateAtomic(ctx, userID, func(row *domain.Streak) error {
		outcome = applyCheckIn(row, day)
		return nil
	})
	if err != nil {
		return zero, fmt.Errorf("record check-in streak: %w", err)
	}
	result := CheckInResult{
		AutoFreezeApplied:   outcome.AutoFreezeApplied,
		CatchUpContinued:    outcome.CatchUpContinued,
		UnusedFreezeCleared: outcome.UnusedFreezeCleared,
		Streak:              s.streakResponse(ctx, userID, row),
	}
	if outcome.AutoFreezeApplied {
		slog.Info("streak auto-freeze: spent one freeze for missed day",
			"user_id", userID,
			"check_date", day.Format("2006-01-02"),
			"freezes_remaining", result.Streak.FreezesAvailable,
			"protected_until", result.Streak.ProtectedUntil,
		)
	}
	return result, nil
}

// CheckInResult is returned after RecordCheckIn so callers (e.g. skin-check
// create) can tell the client when an auto-freeze was spent.
type CheckInResult struct {
	AutoFreezeApplied   bool
	CatchUpContinued    bool
	UnusedFreezeCleared bool
	Streak              dto.StreakResponse
}

// CheckInOutcome is the internal result of applyCheckIn (auto vs continue/reset).
type CheckInOutcome struct {
	// AutoFreezeApplied means the system just spent one freeze to cover a
	// single missed day. Not set when honoring a pre-activated manual freeze.
	AutoFreezeApplied bool
	// CatchUpContinued means a manual freeze bridge was honored (no extra spend).
	CatchUpContinued bool
	// UnusedFreezeCleared means the user checked in on a day they had reserved
	// with UseFreeze — inventory stays spent, history not written as a miss.
	UnusedFreezeCleared bool
}

// UseFreeze consumes one freeze to protect a calendar day (VN calendar) — manual /
// proactive protection initiated by the user (button on Progress).
//
// Target day:
//   - last check-in is today  → protect tomorrow (skip tomorrow without breaking streak)
//   - otherwise (at risk)     → protect today
//
// Rejects when no freezes left, no active streak, soft-expired (miss ≥ 2 without
// bridging protection — same rules as dto.EvaluateStreakView), or already protected.
// Distinct from auto-freeze inside applyCheckIn / RecordCheckIn.
func (s *Service) UseFreeze(ctx context.Context, userID uuid.UUID) (dto.StreakResponse, error) {
	var zero dto.StreakResponse
	if s == nil || s.store == nil {
		return zero, ErrUnavailable
	}
	today := streaktime.Today()

	row, err := s.store.UpdateAtomic(ctx, userID, func(row *domain.Streak) error {
		return applyUseFreeze(row, today)
	})
	if err != nil {
		return zero, err
	}
	res := s.streakResponse(ctx, userID, row)
	slog.Info("streak manual-freeze: user spent one freeze",
		"user_id", userID,
		"protected_until", res.ProtectedUntil,
		"freezes_remaining", res.FreezesAvailable,
	)
	return res, nil
}

// ReconcileResult is the outcome of an admin streak repair.
type ReconcileResult struct {
	TargetUserID     uuid.UUID
	DaysReplayed     int
	FreezesPreserved bool
	// FreezeBridgesInvented is true only if replay temporarily seeded freezes
	// (legacy behaviour). Current reconcile always keeps this false.
	FreezeBridgesInvented bool
	Before                dto.StreakResponse
	After                 dto.StreakResponse
}

// ReconcileForUser rebuilds current_streak / longest_streak / last_check_in_date
// by replaying distinct SkinCheck calendar days.
//
// Purpose: admin repair when counters drifted due to a system bug — NOT a
// user-facing feature and NOT a way to refill freezes.
//
// Freeze inventory (FreezesAvailable), ProtectedUntil, and LastFreezeDate are
// preserved exactly. Replay runs with FreezesAvailable=0 so applyCheckIn cannot
// invent auto-freeze bridges for historical 1-day gaps (that would inflate
// streak length for users who had already spent their freeze).
func (s *Service) ReconcileForUser(ctx context.Context, userID uuid.UUID) (ReconcileResult, error) {
	var zero ReconcileResult
	if s == nil || s.store == nil {
		return zero, ErrUnavailable
	}
	if s.dates == nil {
		return zero, fmt.Errorf("%w: check-date source not configured", ErrUnavailable)
	}
	dates, err := s.dates.ListDistinctCheckDates(ctx, userID)
	if err != nil {
		return zero, fmt.Errorf("list check dates: %w", err)
	}

	var before dto.StreakResponse
	var freezesBefore int
	row, err := s.store.UpdateAtomic(ctx, userID, func(row *domain.Streak) error {
		before = dto.NewStreakResponse(row)
		freezesBefore = row.FreezesAvailable
		replayFromCheckDates(row, dates)
		return nil
	})
	if err != nil {
		return zero, fmt.Errorf("reconcile streak: %w", err)
	}
	after := s.streakResponse(ctx, userID, row)
	before = s.attachFirstCheckIn(ctx, userID, before)
	result := ReconcileResult{
		TargetUserID:          userID,
		DaysReplayed:          len(dates),
		FreezesPreserved:      after.FreezesAvailable == freezesBefore,
		FreezeBridgesInvented: false,
		Before:                before,
		After:                 after,
	}
	slog.Info("streak reconcile: repaired counters from skin_checks",
		"target_user_id", userID,
		"days_replayed", result.DaysReplayed,
		"before_current", before.CurrentStreak,
		"after_current", after.CurrentStreak,
		"before_longest", before.LongestStreak,
		"after_longest", after.LongestStreak,
		"freezes_available", after.FreezesAvailable,
		"freezes_preserved", result.FreezesPreserved,
		"freeze_bridges_invented", result.FreezeBridgesInvented,
	)
	return result, nil
}

// streakResponse maps a domain row and attaches first_check_in_date when available.
func (s *Service) streakResponse(ctx context.Context, userID uuid.UUID, row *domain.Streak) dto.StreakResponse {
	return s.attachFirstCheckIn(ctx, userID, dto.NewStreakResponse(row))
}

// attachFirstCheckIn sets FirstCheckInDate from MIN(skin_checks.check_date).
// Failures are ignored — the field is optional for clients.
func (s *Service) attachFirstCheckIn(ctx context.Context, userID uuid.UUID, res dto.StreakResponse) dto.StreakResponse {
	if s == nil || s.dates == nil {
		return res
	}
	first, err := s.dates.FirstCheckDate(ctx, userID)
	if err != nil || first == nil {
		return res
	}
	day := first.UTC().Format("2006-01-02")
	res.FirstCheckInDate = &day
	return res
}

// replayFromCheckDates rebuilds CurrentStreak / LongestStreak / LastCheckInDate
// from SkinCheck days for admin reconcile.
//
// Important — do NOT seed DefaultFreezesAvailable during replay:
// seeding would let applyCheckIn auto-bridge every historical 1-day gap and
// invent a longer streak than users who had already spent their freeze.
// Replay therefore forces FreezesAvailable=0 (consecutive days only); live
// freeze inventory + ProtectedUntil + LastFreezeDate are restored afterward.
func replayFromCheckDates(row *domain.Streak, dates []time.Time) {
	if row == nil {
		return
	}
	preservedFreezes := row.FreezesAvailable
	preservedProtected := cloneTimePtr(row.ProtectedUntil)
	preservedLastFreeze := cloneTimePtr(row.LastFreezeDate)
	preservedFreezeDates := append(json.RawMessage(nil), row.FreezeDates...)

	row.CurrentStreak = 0
	row.LongestStreak = 0
	row.LastCheckInDate = nil
	row.ProtectedUntil = nil
	row.LastFreezeDate = nil
	row.FreezeDates = nil
	// Zero freezes for simulation — never invent freeze-bridged history.
	row.FreezesAvailable = 0

	for _, d := range dates {
		_ = applyCheckIn(row, utcDate(d))
	}

	row.FreezesAvailable = preservedFreezes
	row.ProtectedUntil = preservedProtected
	row.LastFreezeDate = preservedLastFreeze
	row.FreezeDates = preservedFreezeDates
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

// applyUseFreeze spends one freeze proactively (manual).
//
// Soft-expire guard: uses the same read rules as EvaluateStreakView. If the
// streak is already broken for display (effective 0), reject — callers must
// not be able to spend a freeze to temporarily "revive" a dead streak via API.
//
// Evaluate before clearing expired ProtectedUntil: a bridge of yesterday
// (protectedMissDay) is still a live streak until catch-up check-in.
func applyUseFreeze(row *domain.Streak, today time.Time) error {
	today = utcDate(today)

	if row.CurrentStreak <= 0 || row.LastCheckInDate == nil {
		return ErrNoStreak
	}

	// Soft-expire / catch-up before inventory — clearer errors than "no freezes"
	// when the streak is already dead or only check-in can save it.
	view := dto.EvaluateStreakView(row, today)
	if view.EffectiveStreak <= 0 {
		return ErrSoftExpired
	}
	// Alive only because of pending auto-freeze or a yesterday bridge: manual
	// freeze cannot rewrite that miss day — check-in is the correct action.
	if view.DaysSinceLastCheckIn != nil && *view.DaysSinceLastCheckIn >= 2 {
		return ErrCatchUpRequired
	}

	if row.FreezesAvailable <= 0 {
		return ErrNoFreezes
	}

	// Drop pu < today only after the soft-expire check (yesterday bridge matters).
	_ = clearExpiredProtectedUntil(row, today)

	target := freezeTargetDay(row, today)
	if row.ProtectedUntil != nil && sameUTCDate(*row.ProtectedUntil, target) {
		return ErrAlreadyProtected
	}
	// Block if an active protection already covers today or later.
	if row.ProtectedUntil != nil {
		pu := utcDate(*row.ProtectedUntil)
		if !pu.Before(today) {
			return ErrAlreadyProtected
		}
	}

	row.FreezesAvailable--
	row.ProtectedUntil = &target
	// Do NOT recordFreezeDay here: the protected day may still get a real
	// check-in. History is written when applyCheckIn honors/consumes the bridge
	// (or auto-freezes). Mark LastFreezeDate so clients can tell a manual bridge
	// from a pending auto-save before catch-up.
	row.LastFreezeDate = &target
	return nil
}

// freezeTargetDay picks which calendar day the freeze covers.
func freezeTargetDay(row *domain.Streak, today time.Time) time.Time {
	last := utcDate(*row.LastCheckInDate)
	if sameUTCDate(last, today) {
		return today.AddDate(0, 0, 1) // already checked in — protect tomorrow
	}
	return today // at risk — protect today so a miss does not break the streak
}

// applyCheckIn mutates row in place for a check-in on day (date-only,
// Vietnam calendar Y-M-D stored as UTC midnight).
//
// Auto-freeze (system): when last check-in was day-2 and a freeze remains, one
// freeze is spent to cover yesterday so the streak continues. That is separate
// from UseFreeze / applyUseFreeze (manual / proactive).
//
// ProtectedUntil cleanup: after a successful check-in (and on same-day no-op for
// expired values), clearConsumedOrExpiredProtectedUntil removes protection whose
// calendar day is on or before the check-in day — the freeze has been used or
// the day has passed. Future protection (e.g. tomorrow) is kept.
func applyCheckIn(row *domain.Streak, day time.Time) CheckInOutcome {
	if row == nil {
		return CheckInOutcome{}
	}
	day = utcDate(day)

	// Same calendar day: already counted — still scrub expired protection.
	if row.LastCheckInDate != nil && sameUTCDate(*row.LastCheckInDate, day) {
		_ = clearExpiredProtectedUntil(row, day)
		revertUnusedFreezeReservation(row, day)
		return CheckInOutcome{}
	}

	// First ever check-in.
	if row.LastCheckInDate == nil {
		row.CurrentStreak = 1
		if row.LongestStreak < 1 {
			row.LongestStreak = 1
		}
		row.LastCheckInDate = &day
		clearConsumedOrExpiredProtectedUntil(row, day)
		revertUnusedFreezeReservation(row, day)
		return CheckInOutcome{}
	}

	last := utcDate(*row.LastCheckInDate)
	yesterday := day.AddDate(0, 0, -1)
	dayBeforeYesterday := day.AddDate(0, 0, -2)

	var outcome CheckInOutcome
	switch {
	// Consecutive day: last check-in was yesterday → continue streak.
	case sameUTCDate(last, yesterday):
		unusedFreeze := isUnusedFreezeReservation(row, day)
		row.CurrentStreak++
		bumpLongest(row)
		row.LastCheckInDate = &day
		if unusedFreeze {
			outcome = CheckInOutcome{UnusedFreezeCleared: true}
		}

	// Missed exactly one day, already covered by a proactive (manual) freeze.
	case sameUTCDate(last, dayBeforeYesterday) &&
		row.ProtectedUntil != nil &&
		sameUTCDate(*row.ProtectedUntil, yesterday):
		row.CurrentStreak++
		bumpLongest(row)
		row.LastCheckInDate = &day
		recordFreezeDay(row, yesterday)
		outcome = CheckInOutcome{CatchUpContinued: true}

	// Auto-freeze: missed exactly one day, and a freeze is available.
	case sameUTCDate(last, dayBeforeYesterday) && row.FreezesAvailable > 0:
		row.FreezesAvailable--
		missed := yesterday
		row.ProtectedUntil = &missed
		recordFreezeDay(row, missed)
		row.CurrentStreak++
		bumpLongest(row)
		row.LastCheckInDate = &day
		outcome = CheckInOutcome{AutoFreezeApplied: true}

	// Gap too large, or no freezes left → streak breaks; today starts at 1.
	default:
		// Persist a past freeze reservation that covered a real miss (never
		// honored via catch-up) before wiping counters / PU.
		commitExpiredFreezeHistory(row, day)
		row.CurrentStreak = 1
		bumpLongest(row)
		row.ProtectedUntil = nil
		row.LastCheckInDate = &day
	}

	// Drop protection for this check-in day or earlier (consumed / past).
	// Auto-freeze briefly sets ProtectedUntil=yesterday then this clears it —
	// the miss was already applied; keeping yesterday would be stale.
	clearConsumedOrExpiredProtectedUntil(row, day)
	// If the user checked in on a day they had reserved with UseFreeze (no miss),
	// drop the misleading LastFreezeDate reservation (inventory stays spent).
	revertUnusedFreezeReservation(row, day)
	return outcome
}

// clearExpiredProtectedUntil clears ProtectedUntil when the protected day is
// strictly before today. ProtectedUntil == today remains active (covers a miss
// today). Returns true when the field was cleared.
func clearExpiredProtectedUntil(row *domain.Streak, today time.Time) bool {
	if row == nil || row.ProtectedUntil == nil {
		return false
	}
	if utcDate(*row.ProtectedUntil).Before(utcDate(today)) {
		row.ProtectedUntil = nil
		return true
	}
	return false
}

// clearConsumedOrExpiredProtectedUntil clears ProtectedUntil when pu <= asOf.
// Used after check-in: the protected day has been bridged or the user checked
// in on that day, so the value must not linger. Keeps pu > asOf (future day).
func clearConsumedOrExpiredProtectedUntil(row *domain.Streak, asOf time.Time) {
	if row == nil || row.ProtectedUntil == nil {
		return
	}
	pu := utcDate(*row.ProtectedUntil)
	asOf = utcDate(asOf)
	if !pu.After(asOf) {
		row.ProtectedUntil = nil
	}
}

// revertUnusedFreezeReservation clears LastFreezeDate when it was only a
// UseFreeze reservation (not yet written to FreezeDates) and that day has
// passed or been checked in — otherwise mini-history paints a false freeze.
func revertUnusedFreezeReservation(row *domain.Streak, day time.Time) {
	if row == nil || row.LastFreezeDate == nil {
		return
	}
	day = utcDate(day)
	lfd := utcDate(*row.LastFreezeDate)
	if lfd.After(day) {
		// Still protecting a future day — keep the reservation marker.
		return
	}
	key := lfd.Format("2006-01-02")
	dates := parseFreezeDatesOrNil(row.FreezeDates)
	if dates != nil {
		for _, d := range dates {
			if d == key {
				return // actually consumed / recorded
			}
		}
	}
	// Restore prior history tip if jsonb has entries; else clear.
	if dates != nil && len(dates) > 0 {
		prev := dates[len(dates)-1]
		if t, err := time.ParseInLocation("2006-01-02", prev, time.UTC); err == nil {
			row.LastFreezeDate = &t
			return
		}
	}
	row.LastFreezeDate = nil
}

// isUnusedFreezeReservation reports a UseFreeze day that will be cleared by
// today's check-in without ever being a rescued miss (not in FreezeDates yet).
func isUnusedFreezeReservation(row *domain.Streak, day time.Time) bool {
	if row == nil || row.LastFreezeDate == nil {
		return false
	}
	day = utcDate(day)
	lfd := utcDate(*row.LastFreezeDate)
	if lfd.After(day) {
		return false
	}
	if freezeDateRecorded(row, lfd) {
		return false
	}
	// Active PU covering today or earlier, or LFD == today (reserved this day).
	if row.ProtectedUntil != nil && !utcDate(*row.ProtectedUntil).After(day) {
		return true
	}
	return sameUTCDate(lfd, day)
}

// commitExpiredFreezeHistory writes a past freeze reservation into FreezeDates
// when the streak is about to reset (gap too large). Prevents losing the day
// that a manual freeze actually covered as a miss.
func commitExpiredFreezeHistory(row *domain.Streak, day time.Time) {
	if row == nil {
		return
	}
	day = utcDate(day)
	if row.ProtectedUntil != nil {
		pu := utcDate(*row.ProtectedUntil)
		if pu.Before(day) {
			// Same rule as persistClearExpiredProtection: only a real miss
			// (last < pu). leftover last==PU must not become freeze history.
			if row.LastCheckInDate != nil && utcDate(*row.LastCheckInDate).Before(pu) {
				recordFreezeDay(row, pu)
			}
			return
		}
	}
	if row.LastFreezeDate == nil || row.LastCheckInDate == nil {
		return
	}
	lfd := utcDate(*row.LastFreezeDate)
	last := utcDate(*row.LastCheckInDate)
	// Reserved day after last check-in = a miss that was (or would be) covered.
	if last.Before(lfd) && !lfd.After(day) && !freezeDateRecorded(row, lfd) {
		recordFreezeDay(row, lfd)
	}
}

func freezeDateRecorded(row *domain.Streak, day time.Time) bool {
	key := utcDate(day).Format("2006-01-02")
	dates := parseFreezeDatesOrNil(row.FreezeDates)
	if dates == nil {
		return false
	}
	for _, d := range dates {
		if d == key {
			return true
		}
	}
	return false
}

func bumpLongest(row *domain.Streak) {
	if row.CurrentStreak > row.LongestStreak {
		row.LongestStreak = row.CurrentStreak
	}
}

// recordFreezeDay appends day to FreezeDates history and sets LastFreezeDate.
// Idempotent for the same day; keeps at most MaxFreezeDatesKept entries.
//
// Backfill: if FreezeDates is empty/corrupt but LastFreezeDate is set to a
// different day, seed that day first so the next freeze does not erase legacy
// single-field history.
func recordFreezeDay(row *domain.Streak, day time.Time) {
	if row == nil {
		return
	}
	day = utcDate(day)
	key := day.Format("2006-01-02")

	dates := parseFreezeDatesOrNil(row.FreezeDates)
	if dates == nil {
		// Corrupt JSON — keep raw bytes until we can rewrite a valid list;
		// seed from LastFreezeDate so we do not drop the only known day.
		dates = seedFreezeDatesFromLast(row, key)
	} else if len(dates) == 0 {
		dates = seedFreezeDatesFromLast(row, key)
	}

	for _, d := range dates {
		if d == key {
			row.LastFreezeDate = &day
			raw, err := json.Marshal(dates)
			if err == nil {
				row.FreezeDates = raw
			}
			return
		}
	}
	dates = append(dates, key)
	if len(dates) > domain.MaxFreezeDatesKept {
		dates = dates[len(dates)-domain.MaxFreezeDatesKept:]
	}
	raw, err := json.Marshal(dates)
	if err != nil {
		return
	}
	row.FreezeDates = raw
	row.LastFreezeDate = &day
}

func parseFreezeDatesOrNil(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var dates []string
	if err := json.Unmarshal(raw, &dates); err != nil {
		return nil // signal corrupt
	}
	return dates
}

func seedFreezeDatesFromLast(row *domain.Streak, excludeKey string) []string {
	if row == nil || row.LastFreezeDate == nil {
		return []string{}
	}
	old := utcDate(*row.LastFreezeDate).Format("2006-01-02")
	if old == "" || old == excludeKey {
		return []string{}
	}
	return []string{old}
}

func utcDate(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func sameUTCDate(a, b time.Time) bool {
	return utcDate(a).Equal(utcDate(b))
}
