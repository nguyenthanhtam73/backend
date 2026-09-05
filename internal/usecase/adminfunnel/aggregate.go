// Package adminfunnel serves founder leaky-bucket proxies for Darwin.
package adminfunnel

import (
	"time"

	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

// SignupRow is one non-deleted account used for D0/D1 proxies.
type SignupRow struct {
	UserID    uuid.UUID
	CreatedAt time.Time
}

// CheckDateRow is one distinct (user, Vietnam civil day) with a skin_check.
type CheckDateRow struct {
	UserID    uuid.UUID
	CheckDate time.Time
}

// RetentionProxies are D0/D1 counts derived from signup + check-date rows.
type RetentionProxies struct {
	D0Users      int64
	D1Users      int64
	D1Eligible   int64
	D0Users7d    int64
	D1Users7d    int64
	D1Eligible7d int64
}

// CountRetentionProxies counts users who checked in on signup day (D0) and
// the next Vietnam civil day (D1).
//
// D1 is only computable when the signup Vietnam day is strictly before today
// (the user has had a D1). now and CreatedAt are interpreted with streaktime
// (Asia/Ho_Chi_Minh), matching SkinCheck.check_date.
//
// since7d limits the *_7d fields to users whose CreatedAt is on/after that instant.
func CountRetentionProxies(signups []SignupRow, checks []CheckDateRow, now time.Time, since7d time.Time) RetentionProxies {
	if now.IsZero() {
		now = time.Now()
	}
	today := streaktime.DateOf(now)

	byUser := make(map[uuid.UUID]map[string]struct{}, len(signups))
	for _, c := range checks {
		if c.UserID == uuid.Nil {
			continue
		}
		day := streaktime.DateOf(c.CheckDate).Format("2006-01-02")
		days := byUser[c.UserID]
		if days == nil {
			days = make(map[string]struct{})
			byUser[c.UserID] = days
		}
		days[day] = struct{}{}
	}

	var out RetentionProxies
	seen := make(map[uuid.UUID]struct{}, len(signups))
	for _, u := range signups {
		if u.UserID == uuid.Nil {
			continue
		}
		if _, dup := seen[u.UserID]; dup {
			continue
		}
		seen[u.UserID] = struct{}{}
		if u.CreatedAt.IsZero() {
			continue
		}

		signupDay := streaktime.DateOf(u.CreatedAt)
		d0Key := signupDay.Format("2006-01-02")
		d1Day := signupDay.AddDate(0, 0, 1)
		d1Key := d1Day.Format("2006-01-02")
		days := byUser[u.UserID]
		_, hasD0 := days[d0Key]
		_, hasD1 := days[d1Key]
		in7d := !u.CreatedAt.Before(since7d)
		d1Eligible := today.After(signupDay)

		if hasD0 {
			out.D0Users++
			if in7d {
				out.D0Users7d++
			}
		}
		if d1Eligible {
			out.D1Eligible++
			if in7d {
				out.D1Eligible7d++
			}
			if hasD1 {
				out.D1Users++
				if in7d {
					out.D1Users7d++
				}
			}
		}
	}
	return out
}
