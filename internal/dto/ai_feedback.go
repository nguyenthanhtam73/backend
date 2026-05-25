package dto

import (
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// maxFeedbackCommentRunes caps free-text reasons to keep storage + prompt
// injection sane. Long rants go to support; the AI loop only needs a short
// signal.
const maxFeedbackCommentRunes = 600

// CreateAIFeedbackRequest is POST /api/v1/ai/feedback.
//
// `comment` is the optional free-text "lý do ngắn" the user types in the
// FeedbackButtons reveal. We trim + length-cap it before persisting.
type CreateAIFeedbackRequest struct {
	// TargetType — one of domain.AllAIFeedbackTargets values.
	TargetType string `json:"target_type"`
	// TargetID — UUID string. Persisted analyses/profiles use their real id;
	// transient surfaces (suggested_routine, progress_summary) carry a
	// per-render UUID returned by the originating endpoint.
	TargetID string `json:"target_id"`
	// Rating — domain.AIFeedbackHelpful | domain.AIFeedbackNotHelpful.
	Rating string `json:"rating"`
	// Comment — optional short reason ("Chưa hợp gu của tôi vì…"). Trimmed + capped.
	Comment string `json:"comment,omitempty"`
}

// AIFeedbackResponse is a minimal ack returned by POST /ai/feedback.
type AIFeedbackResponse struct {
	ID      string `json:"id"`
	Message string `json:"message"`
}

// AIFeedbackHistoryItem is one row in the user's feedback history list.
type AIFeedbackHistoryItem struct {
	ID         string `json:"id"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Rating     string `json:"rating"`
	Comment    string `json:"comment,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// AIFeedbackHistoryResponse is the payload for GET /ai/feedback/me.
type AIFeedbackHistoryResponse struct {
	Items []AIFeedbackHistoryItem `json:"items"`
	Count int                     `json:"count"`
}

// FromDomainFeedback maps a domain row to its public history shape.
func FromDomainFeedback(f domain.AIUserFeedback) AIFeedbackHistoryItem {
	return AIFeedbackHistoryItem{
		ID:         f.ID.String(),
		TargetType: f.TargetType,
		TargetID:   f.TargetID.String(),
		Rating:     f.Rating,
		Comment:    f.Comment,
		CreatedAt:  f.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// ValidateAndMap converts DTO to domain row or returns err message.
func (r CreateAIFeedbackRequest) ValidateAndMap(userID uuid.UUID) (*domain.AIUserFeedback, string) {
	tt := strings.TrimSpace(r.TargetType)
	rt := strings.TrimSpace(r.Rating)
	if !domain.IsValidAIFeedbackTarget(tt) {
		return nil, "target_type must be one of skin_analysis | starter_routine | suggested_routine | progress_summary | daily_check_in"
	}
	switch rt {
	case string(domain.AIFeedbackHelpful), string(domain.AIFeedbackNotHelpful):
	default:
		return nil, "rating must be helpful or not_helpful"
	}
	id, err := uuid.Parse(strings.TrimSpace(r.TargetID))
	if err != nil || id == uuid.Nil {
		return nil, "target_id must be a valid UUID"
	}
	comment := strings.TrimSpace(r.Comment)
	if runes := []rune(comment); len(runes) > maxFeedbackCommentRunes {
		comment = strings.TrimSpace(string(runes[:maxFeedbackCommentRunes]))
	}
	return &domain.AIUserFeedback{
		UserID:     userID,
		TargetType: tt,
		TargetID:   id,
		Rating:     rt,
		Comment:    comment,
	}, ""
}
