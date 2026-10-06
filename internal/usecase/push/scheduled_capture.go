package push

import (
	"context"

	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/google/uuid"
)

// SendScheduledCapture delivers one capture push for a saved schedule.
//
// It does not apply the Vietnam "already checked in" gate. The caller skips
// users who checked in on their own local civil day, then claims that day,
// then calls this. runDate is that local date and is stored on the push
// receipt. A missing subscription is ErrNotFound.
func (s *Service) SendScheduledCapture(
	ctx context.Context,
	userID uuid.UUID,
	nType pushsvc.NotificationType,
	runDate string,
) error {
	if s == nil || s.sender == nil {
		return ErrSenderUnavailable
	}
	if userID == uuid.Nil || nType == "" {
		return ErrNotFound
	}
	if err := s.SendByType(ctx, userID, nType, nil); err != nil {
		return err
	}
	if runDate != "" {
		s.markSentDurable(ctx, userID, string(nType), runDate)
	}
	return nil
}
