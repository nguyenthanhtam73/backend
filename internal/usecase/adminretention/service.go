// Package adminretention serves read-only retention counts for admins.
// Every figure comes from SELECT queries against users and skin_checks.
package adminretention

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/dto"
	"gorm.io/gorm"
)

const calendarNote = "Asia/Ho_Chi_Minh"

var (
	// ErrUnavailable means the database is not configured.
	ErrUnavailable = errors.New("admin retention stats unavailable")
)

// DateError is a rejected from/to query (YYYY-MM-DD Vietnam civil dates).
type DateError struct {
	Msg string
}

func (e *DateError) Error() string {
	if e == nil || e.Msg == "" {
		return "from and to must be YYYY-MM-DD Vietnam dates"
	}
	return e.Msg
}

// Query is an optional registration window plus exact emails to omit
// (admin and skin-review lists). Substring markers always apply.
type Query struct {
	From           *string
	To             *string
	ExcludedEmails []string
}

// Service aggregates retention stats. It never writes.
type Service struct {
	db  *gorm.DB
	now func() time.Time
}

// NewService wires the database. A nil db makes Stats return ErrUnavailable.
func NewService(db *gorm.DB) *Service {
	return &Service{db: db, now: time.Now}
}

// ParseDayRange validates optional from/to query values. Blank means unset.
// Both bounds are inclusive Vietnam civil dates.
func ParseDayRange(fromRaw, toRaw string) (from, to *string, err error) {
	from, err = parseDay("from", fromRaw)
	if err != nil {
		return nil, nil, err
	}
	to, err = parseDay("to", toRaw)
	if err != nil {
		return nil, nil, err
	}
	if from != nil && to != nil && *from > *to {
		return nil, nil, &DateError{Msg: "from must be on or before to"}
	}
	return from, to, nil
}

func parseDay(name, raw string) (*string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil || parsed.Format("2006-01-02") != raw {
		return nil, &DateError{Msg: name + " must be a Vietnam civil date (YYYY-MM-DD)"}
	}
	day := parsed.Format("2006-01-02")
	return &day, nil
}

// Stats returns retention counts. from/to filter registration day, not
// check-in day. Nil bounds mean all time.
func (s *Service) Stats(ctx context.Context, q Query) (dto.AdminRetentionStatsResponse, error) {
	var zero dto.AdminRetentionStatsResponse
	if s == nil || s.db == nil {
		return zero, ErrUnavailable
	}
	from, to, err := ParseDayRange(deref(q.From), deref(q.To))
	if err != nil {
		return zero, err
	}
	q.From, q.To = from, to

	d, err := dialectFor(s.db)
	if err != nil {
		return zero, err
	}
	where, args := d.userWhere(q.ExcludedEmails, q.From, q.To)

	var registered, withCheckin int64
	var d1, d2, d7, d14, maxDays int64
	var c1, c2, c7, c14, returned int64
	err = s.db.WithContext(ctx).Raw(d.summarySQL(where), args...).Row().Scan(
		&registered,
		&withCheckin,
		&d1, &d2, &d7, &d14, &maxDays,
		&c1, &c2, &c7, &c14,
		&returned,
	)
	if err != nil {
		return zero, fmt.Errorf("retention summary: %w", err)
	}

	weeks, err := s.signupWeeks(ctx, d.cohortSQL(where), args)
	if err != nil {
		return zero, err
	}

	nowFn := s.now
	if nowFn == nil {
		nowFn = time.Now
	}
	return dto.AdminRetentionStatsResponse{
		RegisteredUsers:  registered,
		UsersWithCheckin: withCheckin,
		DaysUsed: dto.AdminRetentionDaysUsed{
			AtLeast1:  d1,
			AtLeast2:  d2,
			AtLeast7:  d7,
			AtLeast14: d14,
			MaxDays:   maxDays,
		},
		Consecutive: dto.AdminRetentionConsecutive{
			AtLeast1:  c1,
			AtLeast2:  c2,
			AtLeast7:  c7,
			AtLeast14: c14,
		},
		ReturnNextDay: returned,
		BySignupWeek:  weeks,
		From:          q.From,
		To:            q.To,
		Calendar:      calendarNote,
		AsOf:          nowFn().UTC().Format(time.RFC3339),
	}, nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

type cohortRow struct {
	Week            string `gorm:"column:week"`
	Registered      int64  `gorm:"column:registered"`
	CheckedInOnce   int64  `gorm:"column:checked_in_once"`
	AtLeast2Days    int64  `gorm:"column:at_least_2_days"`
	ReturnedNextDay int64  `gorm:"column:returned_next_day"`
}

func (s *Service) signupWeeks(ctx context.Context, query string, args []any) ([]dto.AdminRetentionSignupWeek, error) {
	rows, err := s.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("retention cohorts: %w", err)
	}
	defer rows.Close()

	out := make([]dto.AdminRetentionSignupWeek, 0)
	for rows.Next() {
		var row cohortRow
		if err := rows.Scan(&row.Week, &row.Registered, &row.CheckedInOnce, &row.AtLeast2Days, &row.ReturnedNextDay); err != nil {
			return nil, fmt.Errorf("retention cohorts: %w", err)
		}
		out = append(out, dto.AdminRetentionSignupWeek{
			Week:            row.Week,
			Registered:      row.Registered,
			CheckedInOnce:   row.CheckedInOnce,
			AtLeast2Days:    row.AtLeast2Days,
			ReturnedNextDay: row.ReturnedNextDay,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("retention cohorts: %w", err)
	}
	return out, nil
}
