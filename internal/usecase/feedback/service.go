// Package feedback handles user-submitted product feedback (bugs, ideas, general notes).
package feedback

import (
	"context"
	"errors"
	"fmt"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
)

var (
	ErrUnavailable = errors.New("feedback service unavailable")
	ErrNotFound    = errors.New("feedback not found")
)

// Service persists and triages feedback rows.
type Service struct {
	repo *repository.GormFeedbackRepository
}

// NewService constructs Service.
func NewService(repo *repository.GormFeedbackRepository) *Service {
	return &Service{repo: repo}
}

// Create stores one feedback submission from an authenticated user.
func (s *Service) Create(
	ctx context.Context,
	userID uuid.UUID,
	req dto.CreateFeedbackRequest,
) (dto.FeedbackCreateResponse, error) {
	var zero dto.FeedbackCreateResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	row, msg := req.ValidateAndMap(userID)
	if row == nil {
		return zero, fmt.Errorf("%s", msg)
	}
	if err := s.repo.Create(ctx, row); err != nil {
		return zero, err
	}
	return dto.FeedbackCreateResponse{
		ID:      row.ID.String(),
		Message: "feedback_submitted",
	}, nil
}

// ListAdmin returns paginated feedback for the admin console.
func (s *Service) ListAdmin(
	ctx context.Context,
	filter repository.FeedbackListFilter,
) (dto.AdminFeedbackListResponse, error) {
	var zero dto.AdminFeedbackListResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	rows, total, err := s.repo.ListAdmin(ctx, filter)
	if err != nil {
		return zero, err
	}
	out := dto.AdminFeedbackListResponse{
		Items:    make([]dto.AdminFeedbackItem, 0, len(rows)),
		Total:    total,
		Page:     filter.Page,
		PageSize: filter.PageSize,
	}
	if out.Page <= 0 {
		out.Page = 1
	}
	if out.PageSize <= 0 {
		out.PageSize = 20
	}
	for _, r := range rows {
		out.Items = append(out.Items, dto.FromDomainAppFeedback(r))
	}
	return out, nil
}

// UpdateStatus changes triage status for one feedback row.
func (s *Service) UpdateStatus(
	ctx context.Context,
	id uuid.UUID,
	status string,
) (dto.AdminFeedbackItem, error) {
	var zero dto.AdminFeedbackItem
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	if !isValidStatus(status) {
		return zero, fmt.Errorf("status must be one of new | read | resolved")
	}
	row, err := s.repo.UpdateStatus(ctx, id, status)
	if err != nil {
		return zero, err
	}
	if row == nil {
		return zero, ErrNotFound
	}
	return dto.FromDomainAppFeedback(*row), nil
}

func isValidStatus(v string) bool {
	switch v {
	case "new", "read", "resolved":
		return true
	default:
		return false
	}
}
