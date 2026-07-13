package dto

import (
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

const maxAppFeedbackCommentRunes = 2000

// CreateFeedbackRequest is POST /api/v1/feedbacks.
type CreateFeedbackRequest struct {
	Type    string `json:"type"`
	Comment string `json:"comment"`
}

// FeedbackCreateResponse is returned after a successful submission.
type FeedbackCreateResponse struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// UpdateFeedbackStatusRequest is PATCH /api/v1/admin/feedbacks/:id.
type UpdateFeedbackStatusRequest struct {
	Status string `json:"status"`
}

// AdminFeedbackItem is one row in the admin feedback table.
type AdminFeedbackItem struct {
	ID            string `json:"id"`
	Type          string `json:"type"`
	Comment       string `json:"comment"`
	Status        string `json:"status"`
	UserEmail     string `json:"user_email"`
	UserUsername  string `json:"user_username"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// AdminFeedbackListResponse is GET /api/v1/admin/feedbacks.
type AdminFeedbackListResponse struct {
	Items    []AdminFeedbackItem `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

// ValidateAndMap converts create DTO to domain row or returns an error message.
func (r CreateFeedbackRequest) ValidateAndMap(userID uuid.UUID) (*domain.Feedback, string) {
	typ := strings.TrimSpace(r.Type)
	if !domain.IsValidFeedbackType(typ) {
		return nil, "type must be one of ai_feedback | bug_report | feature_request | general"
	}
	comment := strings.TrimSpace(r.Comment)
	if comment == "" {
		return nil, "comment is required"
	}
	if runes := []rune(comment); len(runes) > maxAppFeedbackCommentRunes {
		comment = strings.TrimSpace(string(runes[:maxAppFeedbackCommentRunes]))
	}
	return &domain.Feedback{
		UserID:  userID,
		Type:    typ,
		Comment: comment,
		Status:  string(domain.FeedbackStatusNew),
	}, ""
}

// FromDomainAppFeedback maps a persisted row (with optional User preload) to admin list shape.
func FromDomainAppFeedback(f domain.Feedback) AdminFeedbackItem {
	item := AdminFeedbackItem{
		ID:        f.ID.String(),
		Type:      f.Type,
		Comment:   f.Comment,
		Status:    f.Status,
		CreatedAt: f.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: f.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if f.User.ID != uuid.Nil {
		item.UserEmail = f.User.Email
		item.UserUsername = f.User.Username
	}
	return item
}
