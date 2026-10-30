// Package adminstats serves admin-only aggregate registration counts.
package adminstats

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	// Zone data for time.LoadLocation. The server image has no zoneinfo.
	_ "time/tzdata"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
)

const (
	// DefaultTimezone buckets signup instants on the Vietnam civil calendar
	// when the request omits tz. Same zone as streak and funnel D0/D1.
	DefaultTimezone = "Asia/Ho_Chi_Minh"
)

var (
	// ErrUnavailable means the user repository is not wired.
	ErrUnavailable = errors.New("admin registration stats unavailable")
	// ErrInvalidSince means since is missing or not YYYY-MM-DD.
	ErrInvalidSince = errors.New("invalid since")
	// ErrInvalidTimezone means tz is not an IANA location name.
	ErrInvalidTimezone = errors.New("invalid timezone")
)

// Service aggregates first-touch signup columns already stored on users.
type Service struct {
	users *repository.GormUserRepository
}

// NewService wires the user repository. A nil repo returns ErrUnavailable.
func NewService(users *repository.GormUserRepository) *Service {
	return &Service{users: users}
}

// RegistrationsBySource counts non-deleted signups per civil day and source
// from since (inclusive) through the latest stored created_at.
// tz blank uses DefaultTimezone. The payload has counts only.
func (s *Service) RegistrationsBySource(ctx context.Context, sinceRaw, tzRaw string) (dto.AdminRegistrationsBySourceResponse, error) {
	var zero dto.AdminRegistrationsBySourceResponse
	if s == nil || s.users == nil {
		return zero, ErrUnavailable
	}
	tzName, loc, err := resolveTimezone(tzRaw)
	if err != nil {
		return zero, err
	}
	sinceDay, start, err := parseSince(sinceRaw, loc)
	if err != nil {
		return zero, err
	}

	rows, err := s.users.ListRegistrationAttributionSince(ctx, start.UTC())
	if err != nil {
		return zero, fmt.Errorf("list registration attribution: %w", err)
	}

	byDay := map[string]*dto.AdminRegistrationSourceDay{}
	for i := range rows {
		day := rows[i].CreatedAt.In(loc).Format("2006-01-02")
		bucket := byDay[day]
		if bucket == nil {
			bucket = &dto.AdminRegistrationSourceDay{Date: day}
			byDay[day] = bucket
		}
		switch classifyRegistrationSource(rows[i].UTMSource, rows[i].FBCLID, rows[i].TTCLID) {
		case sourceTikTok:
			bucket.TikTok++
		case sourceFacebook:
			bucket.Facebook++
		case sourceOther:
			bucket.Other++
		default:
			bucket.None++
		}
	}

	dates := make([]string, 0, len(byDay))
	for day := range byDay {
		dates = append(dates, day)
	}
	sort.Strings(dates)
	days := make([]dto.AdminRegistrationSourceDay, 0, len(dates))
	for _, day := range dates {
		days = append(days, *byDay[day])
	}

	return dto.AdminRegistrationsBySourceResponse{
		Timezone: tzName,
		Since:    sinceDay,
		Days:     days,
	}, nil
}

func resolveTimezone(raw string) (string, *time.Location, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		name = DefaultTimezone
	}
	if strings.EqualFold(name, "Local") {
		return "", nil, fmt.Errorf("%w: tz must be an IANA name", ErrInvalidTimezone)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return "", nil, fmt.Errorf("%w: tz must be an IANA name", ErrInvalidTimezone)
	}
	return loc.String(), loc, nil
}

func parseSince(raw string, loc *time.Location) (string, time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", time.Time{}, fmt.Errorf("%w: since is required", ErrInvalidSince)
	}
	start, err := time.ParseInLocation("2006-01-02", raw, loc)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("%w: since must be YYYY-MM-DD", ErrInvalidSince)
	}
	return start.Format("2006-01-02"), start, nil
}
