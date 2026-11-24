// Package coachcopyaudit serves the read-only admin count of coach copy leaks.
package coachcopyaudit

import (
	"context"
	"errors"
	"time"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"gorm.io/gorm"
)

var (
	// ErrUnavailable means the database is not wired.
	ErrUnavailable = errors.New("coach copy audit unavailable")
	// ErrInvalidDays means days is not a whole number from 1 to 90.
	ErrInvalidDays = errors.New("invalid days")
)

// Service counts completed coach rows. It does not update them.
type Service struct {
	repo *repository.CoachCopyAudit
}

// NewService wires the audit queries.
func NewService(db *gorm.DB) *Service {
	if db == nil {
		return &Service{}
	}
	return &Service{repo: repository.NewCoachCopyAudit(db)}
}

// Audit counts completed checks in the last days, excluding @example.com.
// days 0 uses the default of 7. The payload is counts only.
func (s *Service) Audit(ctx context.Context, days int) (dto.AdminCoachCopyAuditResponse, error) {
	var zero dto.AdminCoachCopyAuditResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	if days == 0 {
		days = 7
	}
	if days < 1 || days > 90 {
		return zero, ErrInvalidDays
	}
	since := time.Now().UTC().AddDate(0, 0, -days)
	counts, err := s.repo.CountCompletedByClient(ctx, since)
	if err != nil {
		return zero, err
	}
	texts, err := s.repo.ListCompletedCoachText(ctx, since)
	if err != nil {
		return zero, err
	}
	out := dto.AdminCoachCopyAuditResponse{
		Days:         days,
		ByClientKind: make([]dto.AdminCoachCopyClientCount, 0, len(counts)),
	}
	for _, row := range counts {
		out.ByClientKind = append(out.ByClientKind, dto.AdminCoachCopyClientCount{
			ClientKind:      row.ClientKind,
			ChecksCompleted: row.ChecksCompleted,
			DistinctUsers:   row.DistinctUsers,
		})
	}
	for _, row := range texts {
		corpus := ai.UserFacingCoachCorpus(
			row.SummaryNotes,
			row.Strengths,
			row.Improvements,
			row.RoutineHints,
			row.AvoidOrPatch,
			row.SafetyFlags,
			row.SkinScores,
		)
		if ai.CoachProseHasScoreLeak(corpus) {
			out.NotesMatchingScoreLeak++
		}
		if ai.CoachProseHasTaoMay(corpus) {
			out.NotesMatchingTaoMay++
		}
	}
	return out, nil
}
