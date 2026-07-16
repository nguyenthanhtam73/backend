package push

import (
	"context"
	"errors"
	"fmt"

	"github.com/dadiary/backend/internal/dto"
	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/google/uuid"
)

var (
	// ErrSenderUnavailable means VAPID keys / sender were not configured.
	ErrSenderUnavailable = errors.New("push sender unavailable")
)

// SendToUser delivers a rich notification to the user's active subscription.
// Thin usecase wrapper so handlers / future jobs stay on the application layer.
func (s *Service) SendToUser(
	ctx context.Context,
	userID uuid.UUID,
	payload pushsvc.NotificationPayload,
) error {
	if s == nil || s.sender == nil {
		return ErrSenderUnavailable
	}
	if userID == uuid.Nil {
		return fmt.Errorf("user id required")
	}
	err := s.sender.SendToUser(ctx, userID, payload)
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, pushsvc.ErrNotConfigured):
		return ErrSenderUnavailable
	case errors.Is(err, pushsvc.ErrNoSubscription):
		return ErrNotFound
	default:
		return err
	}
}

// SendByType builds a typed payload via BuildNotificationPayload then sends it.
// Future jobs (daily reminder, streak) should call this instead of hand-rolling copy.
func (s *Service) SendByType(
	ctx context.Context,
	userID uuid.UUID,
	nType pushsvc.NotificationType,
	data map[string]any,
) error {
	return s.SendToUser(ctx, userID, pushsvc.BuildNotificationPayload(nType, data))
}

// SendTestNotification sends the rich Phase 2.2 test payload to the user.
func (s *Service) SendTestNotification(
	ctx context.Context,
	userID uuid.UUID,
) (dto.PushTestResponse, error) {
	var zero dto.PushTestResponse
	if err := s.SendByType(ctx, userID, pushsvc.NotificationTypeTest, nil); err != nil {
		return zero, err
	}
	return dto.PushTestResponse{Message: "test_notification_sent"}, nil
}
