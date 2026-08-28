// Package adminactivity powers the admin “today” view: who checked in (with
// photos) and who ticked products on a Vietnam civil day.
package adminactivity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/streaktime"
)

var (
	ErrUnavailable  = errors.New("admin activity unavailable")
	ErrInvalidInput = errors.New("invalid input")
)

// Service lists daily check-ins and product ticks for operators.
type Service struct {
	checks   *repository.GormSkinCheckRepository
	routines *repository.GormRoutineEntryRepository
}

// NewService wires repositories. Either may be nil → ErrUnavailable on call.
func NewService(
	checks *repository.GormSkinCheckRepository,
	routines *repository.GormRoutineEntryRepository,
) *Service {
	return &Service{checks: checks, routines: routines}
}

// ForDate returns activity for a Vietnam civil day. Empty date uses today.
func (s *Service) ForDate(ctx context.Context, dateStr string) (dto.AdminActivityResponse, error) {
	var zero dto.AdminActivityResponse
	if s == nil || s.checks == nil || s.routines == nil {
		return zero, ErrUnavailable
	}
	day, err := parseActivityDate(dateStr)
	if err != nil {
		return zero, err
	}

	checks, err := s.checks.ListByCheckDate(ctx, day)
	if err != nil {
		return zero, fmt.Errorf("list check-ins: %w", err)
	}
	routines, err := s.routines.ListByDate(ctx, day)
	if err != nil {
		return zero, fmt.Errorf("list routines: %w", err)
	}

	out := dto.AdminActivityResponse{
		Date:         day.UTC().Format("2006-01-02"),
		CheckIns:     make([]dto.AdminActivityCheckIn, 0, len(checks)),
		ProductUsage: make([]dto.AdminActivityProductUse, 0, len(routines)),
	}
	for i := range checks {
		item := toCheckInDTO(&checks[i])
		out.CheckIns = append(out.CheckIns, item)
		if item.HasPhotos {
			out.CheckInPhotoCount++
		}
	}
	out.CheckInCount = len(out.CheckIns)

	for i := range routines {
		item, ok := toProductUseDTO(&routines[i])
		if !ok {
			continue
		}
		out.ProductUsage = append(out.ProductUsage, item)
	}
	out.ProductUsageCount = len(out.ProductUsage)
	return out, nil
}

func parseActivityDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return streaktime.Today(), nil
	}
	t, err := time.ParseInLocation("2006-01-02", raw, streaktime.Location)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: date must be YYYY-MM-DD", ErrInvalidInput)
	}
	return streaktime.DateOf(t), nil
}

func toCheckInDTO(c *domain.SkinCheck) dto.AdminActivityCheckIn {
	if c == nil {
		return dto.AdminActivityCheckIn{}
	}
	urls := dto.BuildPublicUploadURLs(c.ImageURLs)
	item := dto.AdminActivityCheckIn{
		UserID:      c.UserID.String(),
		Username:    c.User.Username,
		Email:       c.User.Email,
		DisplayName: c.User.DisplayName,
		CheckID:     c.ID.String(),
		HasPhotos:   len(urls) > 0,
		PhotoCount:  len(urls),
		PhotoURLs:   urls,
		CreatedAt:   c.CreatedAt.UTC().Format(time.RFC3339),
	}
	return item
}

func toProductUseDTO(r *domain.RoutineEntry) (dto.AdminActivityProductUse, bool) {
	if r == nil {
		return dto.AdminActivityProductUse{}, false
	}
	morning := tickedTitles(r.Morning)
	evening := tickedTitles(r.Evening)
	if len(morning) == 0 && len(evening) == 0 {
		return dto.AdminActivityProductUse{}, false
	}
	titles := append([]string{}, morning...)
	titles = append(titles, evening...)
	return dto.AdminActivityProductUse{
		UserID:        r.UserID.String(),
		Username:      r.User.Username,
		Email:         r.User.Email,
		DisplayName:   r.User.DisplayName,
		MorningTicked: len(morning),
		EveningTicked: len(evening),
		TickedTitles:  titles,
		UpdatedAt:     r.UpdatedAt.UTC().Format(time.RFC3339),
	}, true
}

type stepLite struct {
	Title     string `json:"title"`
	Completed bool   `json:"completed"`
}

func tickedTitles(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var steps []stepLite
	if err := json.Unmarshal(raw, &steps); err != nil {
		return nil
	}
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		title := strings.TrimSpace(s.Title)
		if title == "" || !s.Completed {
			continue
		}
		out = append(out, title)
	}
	return out
}
