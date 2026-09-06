package dto

import (
	"strings"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

const (
	maxPaywallFeatureRunes = 64
	maxPaywallPlanRunes    = 32
	defaultPaywallFeature  = "generic"
)

// LogPaywallViewRequest is POST /analytics/paywall-view body.
type LogPaywallViewRequest struct {
	Surface         string `json:"surface"`
	Feature         string `json:"feature,omitempty"`
	RecommendedPlan string `json:"recommended_plan,omitempty"`
}

// PaywallViewResponse is returned after logging an impression.
type PaywallViewResponse struct {
	ID       string `json:"id"`
	LoggedAt string `json:"logged_at"`
}

// ValidateAndMap checks the request and maps to a domain row.
// userID may be uuid.Nil for guest pricing views.
func (r LogPaywallViewRequest) ValidateAndMap(userID uuid.UUID) (*domain.PaywallView, string) {
	surface := strings.ToLower(strings.TrimSpace(r.Surface))
	if surface == "" {
		return nil, "surface is required"
	}
	if !domain.IsValidPaywallSurface(surface) {
		return nil, "invalid surface"
	}

	feature := strings.ToLower(strings.TrimSpace(r.Feature))
	if feature == "" {
		feature = defaultPaywallFeature
	}
	if utf8.RuneCountInString(feature) > maxPaywallFeatureRunes {
		return nil, "feature is too long"
	}

	plan := strings.ToLower(strings.TrimSpace(r.RecommendedPlan))
	if utf8.RuneCountInString(plan) > maxPaywallPlanRunes {
		return nil, "recommended_plan is too long"
	}

	row := &domain.PaywallView{
		Surface:         surface,
		Feature:         feature,
		RecommendedPlan: plan,
	}
	if userID != uuid.Nil {
		id := userID
		row.UserID = &id
	}
	return row, ""
}
