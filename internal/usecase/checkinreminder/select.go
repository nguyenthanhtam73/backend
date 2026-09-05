// Package checkinreminder owns D0/D1 first-check-in reminder selection.
//
// Retention target: new accounts that have not logged a skin_check on the
// Vietnam civil day they signed up (D0) or the next day (D1). After D1 the
// existing evening daily_reminder push covers ongoing "didn't check in today".
package checkinreminder

import (
	"strings"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
)

// Kind is the reminder cohort for a Vietnam civil day.
type Kind string

const (
	KindNone Kind = "none"
	KindD0   Kind = "d0"
	KindD1   Kind = "d1"
)

// NormalizeKind maps unknown values to KindNone.
func NormalizeKind(raw string) Kind {
	switch Kind(strings.ToLower(strings.TrimSpace(raw))) {
	case KindD0:
		return KindD0
	case KindD1:
		return KindD1
	default:
		return KindNone
	}
}

// Input is the data needed to decide D0/D1 without hitting the database.
type Input struct {
	SignupAt       time.Time
	Now            time.Time
	CheckedInToday bool
	AccountActive  bool
}

// State is the reminder decision for one user on one Vietnam civil day.
type State struct {
	Kind            Kind
	Due             bool
	SignupDate      string // YYYY-MM-DD in Asia/Ho_Chi_Minh
	DaysSinceSignup int    // 0 = signup day; negative if Now is before SignupAt's day
	CheckedInToday  bool
}

// Select decides whether a user needs a D0 or D1 check-in reminder.
//
// Calendar is streaktime (Asia/Ho_Chi_Minh), same as SkinCheck.check_date.
// Due is true only when the account is active, they have not checked in today,
// and today is their signup day (D0) or the next civil day (D1).
func Select(in Input) State {
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	out := State{
		Kind:           KindNone,
		CheckedInToday: in.CheckedInToday,
	}
	if in.SignupAt.IsZero() {
		return out
	}

	signupDay := streaktime.DateOf(in.SignupAt)
	today := streaktime.DateOf(now)
	out.SignupDate = signupDay.Format("2006-01-02")
	out.DaysSinceSignup = int(today.Sub(signupDay).Hours() / 24)

	switch out.DaysSinceSignup {
	case 0:
		out.Kind = KindD0
	case 1:
		out.Kind = KindD1
	default:
		out.Kind = KindNone
	}

	out.Due = in.AccountActive && out.Kind != KindNone && !in.CheckedInToday
	return out
}

// StartOfVNDay is the instant Vietnam's civil day begins (timezone-aware).
// Used to query users.created_at for the D0/D1 signup window.
func StartOfVNDay(t time.Time) time.Time {
	if t.IsZero() {
		t = time.Now()
	}
	local := t.In(streaktime.Location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, streaktime.Location)
}

// SignupWindow returns [start, end) covering yesterday + today in Vietnam so
// a refresh pass can find every D0/D1 candidate (plus a one-day lookback).
func SignupWindow(now time.Time) (from, to time.Time) {
	startToday := StartOfVNDay(now)
	return startToday.AddDate(0, 0, -1), startToday.AddDate(0, 0, 1)
}
