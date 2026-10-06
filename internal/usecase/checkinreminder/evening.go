package checkinreminder

import (
	"time"

	"github.com/dadiary/backend/internal/streaktime"
)

// EveningInput is the data needed to choose a 19:30 ICT reminder email.
//
// Calendar is the Vietnam civil day (streaktime), anchored on the user's
// first skin check — not signup.
//
//	F+0  first check-in (no email from this selector)
//	F+1  D1 email if they have not checked in that day
//	F+2  "day 2" — a check-in here suppresses the Day-3 email
//	F+3  Day-3 email, once, only if they missed day 2 and have not checked in today
type EveningInput struct {
	FirstCheckDate  time.Time
	Now             time.Time
	CheckedInToday  bool
	CheckedInOnDay2 bool
	AccountActive   bool
}

// EveningState is the 19:30 email decision for one user.
type EveningState struct {
	Kind                Kind
	Due                 bool
	DaysSinceFirstCheck int
}

// SelectEveningEmail decides the D1 or Day-3 reminder email.
// Due is false when the account is inactive or they already checked in today.
func SelectEveningEmail(in EveningInput) EveningState {
	out := EveningState{Kind: KindNone, DaysSinceFirstCheck: -1}
	if in.FirstCheckDate.IsZero() {
		return out
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	first := streaktime.DateOf(in.FirstCheckDate)
	today := streaktime.DateOf(now)
	out.DaysSinceFirstCheck = int(today.Sub(first).Hours() / 24)
	if out.DaysSinceFirstCheck < 0 {
		return out
	}

	switch out.DaysSinceFirstCheck {
	case 1:
		out.Kind = KindD1
	case 3:
		if !in.CheckedInOnDay2 {
			out.Kind = KindD3
		}
	}
	out.Due = in.AccountActive && out.Kind != KindNone && !in.CheckedInToday
	return out
}
