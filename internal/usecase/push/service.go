// Package push manages Web Push subscription lifecycle and outbound send (Phase 1–2).
package push

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/google/uuid"
)

var (
	ErrUnavailable = errors.New("push subscription service unavailable")
	ErrNotFound    = errors.New("push subscription not found")
	// ErrAlreadyCheckedIn means the user already has a SkinCheck for today (VN calendar).
	ErrAlreadyCheckedIn = errors.New("already checked in today")
	// ErrAtRisk means the user should get streak_at_risk instead of daily_reminder.
	ErrAtRisk = errors.New("streak at risk — use streak_at_risk notification")
	// ErrNoLongerAtRisk means the user left the at-risk window before send (TTL / race).
	ErrNoLongerAtRisk = errors.New("streak no longer at risk")
)

// CheckInLookup answers whether a user already checked in on the current civil day.
// Implemented by GormSkinCheckRepository; optional on Service (nil = skip filter).
type CheckInLookup interface {
	HasCheckedInToday(ctx context.Context, userID uuid.UUID) (bool, error)
}

// StreakAtRiskSource lists at-risk users and loads a single streak for send-time re-check.
// Implemented by GormStreakRepository.
type StreakAtRiskSource interface {
	ListUsersAtRisk(ctx context.Context) ([]uuid.UUID, error)
	GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Streak, error)
}

// SendReceiptStore records successful evening pushes so retries skip delivered users.
// Implemented by repository.GormPushSendReceiptRepository; nil = in-memory only.
type SendReceiptStore interface {
	HasSent(ctx context.Context, userID uuid.UUID, notificationType, runDate string) (bool, error)
	MarkSent(ctx context.Context, userID uuid.UUID, notificationType, runDate string) error
}

// Service orchestrates subscribe / unsubscribe / lookup / send.
type Service struct {
	repo     *repository.GormPushSubscriptionRepository
	sender   *pushsvc.PushSender
	checkIns CheckInLookup
	streaks  StreakAtRiskSource
	receipts SendReceiptStore
	// streakAtRiskSent dedupes successful streak_at_risk pushes per VN day (fast path).
	streakAtRiskSent streakAtRiskDedupe
}

// NewService constructs Service. Optional deps may be nil when unused.
func NewService(
	repo *repository.GormPushSubscriptionRepository,
	sender *pushsvc.PushSender,
	checkIns CheckInLookup,
	streaks StreakAtRiskSource,
	receipts SendReceiptStore,
) *Service {
	return &Service{
		repo:     repo,
		sender:   sender,
		checkIns: checkIns,
		streaks:  streaks,
		receipts: receipts,
	}
}

// alreadySentDurable checks the DB receipt table (cross-replica retry safe).
func (s *Service) alreadySentDurable(
	ctx context.Context,
	userID uuid.UUID,
	notificationType, runDate string,
) bool {
	if s == nil || s.receipts == nil {
		return false
	}
	ok, err := s.receipts.HasSent(ctx, userID, notificationType, runDate)
	if err != nil {
		slog.Warn("push: HasSent receipt lookup failed — continuing without durable skip",
			"user_id", userID.String(),
			"type", notificationType,
			"err", err,
		)
		return false
	}
	return ok
}

// markSentDurable records a successful send; failures are logged only.
func (s *Service) markSentDurable(
	ctx context.Context,
	userID uuid.UUID,
	notificationType, runDate string,
) {
	if s == nil || s.receipts == nil {
		return
	}
	if err := s.receipts.MarkSent(ctx, userID, notificationType, runDate); err != nil {
		slog.Warn("push: MarkSent receipt failed — retry may double-notify this user",
			"user_id", userID.String(),
			"type", notificationType,
			"err", err,
		)
	}
}

// SubscribeInput is the validated payload for Subscribe (alias of DTO for clarity).
type SubscribeInput = dto.SubscribePushRequest

// Subscribe stores (or replaces) the user's Web Push subscription.
//
// Phase 1 policy: one active subscription per user. We clear any prior row for
// this user and any row sharing the same endpoint (device switch / re-login).
func (s *Service) Subscribe(
	ctx context.Context,
	userID uuid.UUID,
	subscriptionInput SubscribeInput,
) (dto.PushSubscriptionResponse, error) {
	var zero dto.PushSubscriptionResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("user id required")
	}

	row, msg := subscriptionInput.ValidateAndMap(userID)
	if row == nil {
		return zero, fmt.Errorf("%s", msg)
	}

	// Drop stale rows for this endpoint (another account or prior device mapping).
	if err := s.repo.DeleteByEndpoint(ctx, row.Endpoint); err != nil {
		return zero, err
	}
	// Phase 1: replace any existing subscription for this user.
	if err := s.repo.DeleteByUserID(ctx, userID); err != nil {
		return zero, err
	}
	if err := s.repo.Create(ctx, row); err != nil {
		return zero, err
	}
	return dto.FromDomainPushSubscription(*row), nil
}

// Unsubscribe removes all push subscriptions for the authenticated user.
func (s *Service) Unsubscribe(ctx context.Context, userID uuid.UUID) error {
	if s == nil || s.repo == nil {
		return ErrUnavailable
	}
	if userID == uuid.Nil {
		return fmt.Errorf("user id required")
	}
	return s.repo.DeleteByUserID(ctx, userID)
}

// GetActiveSubscription returns the user's current active subscription, if any.
func (s *Service) GetActiveSubscription(
	ctx context.Context,
	userID uuid.UUID,
) (dto.PushSubscriptionResponse, error) {
	var zero dto.PushSubscriptionResponse
	if s == nil || s.repo == nil {
		return zero, ErrUnavailable
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("user id required")
	}
	row, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return zero, err
	}
	if row == nil {
		return zero, ErrNotFound
	}
	return dto.FromDomainPushSubscription(*row), nil
}
