// Package premium is the plan × feature gate for DaDiary (Free / Premium / Premium+).
//
// Callers should use CanUseFeature / GetRemainingQuota / AssertFeature instead of
// comparing plan_tier strings ad hoc. Metered Free usage is stored in user_usages
// (UTC calendar months); Premium / Premium+ metered features are unlimited and
// are not incremented.
package premium

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// systemExpiryActorEmail is stored on plan_change_logs for cron downgrades.
const systemExpiryActorEmail = "expiry@system.dadiary"

// Reason codes returned alongside CanUseFeature / Quota.Reason.
const (
	ReasonOK            = ""
	ReasonQuotaExceeded = "quota_exceeded"
	ReasonFeatureDenied = "feature_denied"
)

var (
	// ErrUnavailable means repos are not wired (fail closed on writes).
	ErrUnavailable = errors.New("premium service unavailable")
	// ErrUnknownFeature is returned when the feature id is empty / not registered.
	ErrUnknownFeature = errors.New("unknown feature")
	// ErrFeatureDenied means the plan does not include the feature (boolean off).
	ErrFeatureDenied = errors.New("feature not available on current plan")
	// ErrQuotaExceeded means a monthly metered feature is exhausted.
	ErrQuotaExceeded = errors.New("monthly quota exceeded")
)

// Quota describes remaining allowance for a feature on the user's current plan.
type Quota struct {
	Feature       domain.Feature  `json:"feature"`
	PlanTier      domain.PlanTier `json:"plan_tier"`
	Kind          EntitlementKind `json:"kind"`
	Allowed       bool            `json:"allowed"`
	Unlimited     bool            `json:"unlimited"`
	Used          int             `json:"used"`
	Limit         int             `json:"limit"`          // monthly cap; -1 when unlimited
	Remaining     int             `json:"remaining"`      // 0 when denied / exhausted; -1 when unlimited
	HistoryMonths int             `json:"history_months"` // for progress; 0 = all time when Allowed
	// Reason is empty when Allowed; otherwise quota_exceeded / feature_denied.
	Reason string `json:"reason,omitempty"`
}

// Service resolves plan tier and evaluates the entitlement catalog against user_usages.
type Service struct {
	users  repository.UserRepository
	usages repository.UserUsageRepository

	// Optional: daily plan-expiry cron (downgrade paid → free).
	db        *gorm.DB
	planUsers *repository.GormUserRepository
	logs      *repository.PlanChangeLogRepository
}

// NewService wires dependencies. Either repo may be nil (guards fail closed).
func NewService(users repository.UserRepository, usages repository.UserUsageRepository) *Service {
	return &Service{users: users, usages: usages}
}

// AttachPlanExpiryDeps enables DowngradeExpiredPlans (daily cron). Safe to call once at boot.
func (s *Service) AttachPlanExpiryDeps(
	db *gorm.DB,
	users *repository.GormUserRepository,
	logs *repository.PlanChangeLogRepository,
) {
	if s == nil {
		return
	}
	s.db = db
	s.planUsers = users
	s.logs = logs
}

// PlanTier loads the user's effective plan (respects plan_expires_at).
func (s *Service) PlanTier(ctx context.Context, userID uuid.UUID) (domain.PlanTier, error) {
	if s == nil || s.users == nil {
		return domain.PlanFree, fmt.Errorf("%w", ErrUnavailable)
	}
	if userID == uuid.Nil {
		return domain.PlanFree, fmt.Errorf("%w: user id required", ErrUnavailable)
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return domain.PlanFree, err
	}
	if u == nil {
		return domain.PlanFree, nil
	}
	// CanUseFeature / quotas all go through here — expired paid → Free immediately.
	return domain.EffectivePlanTier(u, time.Now().UTC()), nil
}

// IsPaidPlan is true for Premium and Premium+.
func (s *Service) IsPaidPlan(ctx context.Context, userID uuid.UUID) (bool, error) {
	tier, err := s.PlanTier(ctx, userID)
	if err != nil {
		return false, err
	}
	return tier.IsPaidPlan(), nil
}

// IsPremiumPlus is true only for the top tier.
func (s *Service) IsPremiumPlus(ctx context.Context, userID uuid.UUID) (bool, error) {
	tier, err := s.PlanTier(ctx, userID)
	if err != nil {
		return false, err
	}
	return tier == domain.PlanPremiumPlus, nil
}

// CanUseFeature reports whether the user may perform the feature right now,
// plus a machine-readable reason when denied.
//
//   - boolean: Enabled
//   - monthly_quota: unlimited OR remaining > 0 (from user_usages)
//   - history_months: always true when Enabled (window clamping is separate)
func (s *Service) CanUseFeature(ctx context.Context, userID uuid.UUID, feature domain.Feature) (bool, string, error) {
	q, err := s.GetRemainingQuota(ctx, userID, feature)
	if err != nil {
		return false, "", err
	}
	return q.Allowed, q.Reason, nil
}

// GetRemainingQuota returns the full entitlement snapshot for one feature.
// Metered Free usage is read from user_usages for the current UTC month.
func (s *Service) GetRemainingQuota(ctx context.Context, userID uuid.UUID, feature domain.Feature) (Quota, error) {
	var out Quota
	if feature == "" {
		return out, ErrUnknownFeature
	}
	tier, err := s.PlanTier(ctx, userID)
	if err != nil {
		return out, err
	}
	ent := EntitlementFor(tier, feature)
	out.Feature = feature
	out.PlanTier = tier
	out.Kind = ent.Kind

	switch ent.Kind {
	case EntitlementBoolean:
		out.Allowed = ent.Enabled
		out.Unlimited = ent.Enabled
		if ent.Enabled {
			out.Limit = UnlimitedMonthly
			out.Remaining = UnlimitedMonthly
		} else {
			out.Reason = ReasonFeatureDenied
		}
		return out, nil

	case EntitlementHistoryMonths:
		out.Allowed = ent.Enabled
		out.HistoryMonths = ent.HistoryMonths
		out.Unlimited = ent.Enabled && ent.HistoryMonths == 0
		if !ent.Enabled {
			out.Reason = ReasonFeatureDenied
		}
		return out, nil

	case EntitlementMonthlyQuota:
		if !ent.Enabled {
			out.Reason = ReasonFeatureDenied
			return out, nil
		}
		if ent.MonthlyLimit == UnlimitedMonthly {
			out.Allowed = true
			out.Unlimited = true
			out.Limit = UnlimitedMonthly
			out.Remaining = UnlimitedMonthly
			return out, nil
		}
		view, err := s.loadUsage(ctx, userID, feature, ent.MonthlyLimit)
		if err != nil {
			return out, err
		}
		out.Used = view.UsageCount
		out.Limit = ent.MonthlyLimit
		out.Remaining = view.Remaining
		out.Allowed = view.Remaining > 0
		if !out.Allowed {
			out.Reason = ReasonQuotaExceeded
		}
		return out, nil

	default:
		return out, fmt.Errorf("%w: %s", ErrUnknownFeature, feature)
	}
}

// AssertFeature returns nil when the user can use the feature, or a typed error.
func (s *Service) AssertFeature(ctx context.Context, userID uuid.UUID, feature domain.Feature) error {
	q, err := s.GetRemainingQuota(ctx, userID, feature)
	if err != nil {
		return err
	}
	if q.Allowed {
		return nil
	}
	switch q.Reason {
	case ReasonQuotaExceeded:
		return ErrQuotaExceeded
	case ReasonFeatureDenied:
		return ErrFeatureDenied
	default:
		if q.Kind == EntitlementMonthlyQuota && q.Limit > 0 && q.Used >= q.Limit {
			return ErrQuotaExceeded
		}
		return ErrFeatureDenied
	}
}

// IncrementUsage records one successful use of a metered feature.
// No-ops for unlimited plans. Concurrent-safe via user_usages row lock + cap.
func (s *Service) IncrementUsage(ctx context.Context, userID uuid.UUID, feature domain.Feature) error {
	if s == nil {
		return fmt.Errorf("%w", ErrUnavailable)
	}
	q, err := s.GetRemainingQuota(ctx, userID, feature)
	if err != nil {
		return err
	}
	if q.Unlimited {
		slog.Debug("user_usage: skip increment (unlimited)",
			"user_id", userID.String(),
			"feature", string(feature),
			"plan_tier", string(q.PlanTier),
		)
		return nil
	}
	if q.Kind != EntitlementMonthlyQuota {
		return nil
	}
	if s.usages == nil {
		return fmt.Errorf("%w", ErrUnavailable)
	}
	if !domain.IsMeteredFeature(string(feature)) {
		return fmt.Errorf("%w: feature %q is not metered", ErrUnknownFeature, feature)
	}

	newCount, ok, err := s.usages.IncrementUsage(ctx, userID, string(feature), q.Limit)
	if err != nil {
		return err
	}
	if !ok {
		slog.Warn("user_usage: assert passed but increment blocked",
			"user_id", userID.String(),
			"feature", string(feature),
			"usage_count", newCount,
			"limit", q.Limit,
		)
		return ErrQuotaExceeded
	}
	return nil
}

// ResetMonthlyUsage deletes completed UTC-month counter rows (cron entrypoint).
func (s *Service) ResetMonthlyUsage(ctx context.Context) (int64, error) {
	if s == nil || s.usages == nil {
		return 0, fmt.Errorf("%w", ErrUnavailable)
	}
	currentStart, _, _ := domain.CurrentUTCMonthPeriod(time.Now())
	deleted, err := s.usages.ResetMonthlyUsage(ctx, currentStart)
	if err != nil {
		return 0, err
	}
	slog.Info("premium: monthly usage reset complete",
		"deleted_rows", deleted,
		"current_period_start", currentStart.Format("2006-01-02"),
	)
	return deleted, nil
}

// DowngradeExpiredPlans sets expired paid users back to Free (daily cron).
// Each user is locked FOR UPDATE so a concurrent SePay IPN cannot race the downgrade.
func (s *Service) DowngradeExpiredPlans(ctx context.Context) (int, error) {
	if s == nil || s.db == nil || s.planUsers == nil {
		return 0, fmt.Errorf("%w", ErrUnavailable)
	}
	now := time.Now().UTC()
	candidates, err := s.planUsers.ListExpiredPaidUsers(ctx, now, 500)
	if err != nil {
		return 0, err
	}
	downgraded := 0
	for i := range candidates {
		uid := candidates[i].ID
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			u, err := s.planUsers.GetByIDForUpdateTx(tx, uid)
			if err != nil {
				return err
			}
			if u == nil {
				return nil
			}
			// Re-check under lock (renewal IPN may have extended expiry).
			if domain.EffectivePlanTier(u, now).IsPaidPlan() {
				return nil
			}
			from := domain.NormalizePlanTier(u.PlanTier)
			if !from.IsPaidPlan() {
				return nil
			}
			updated, err := s.planUsers.UpdatePlanTierTx(tx, uid, domain.PlanFree, nil)
			if err != nil {
				return err
			}
			if updated == nil {
				return nil
			}
			if s.logs != nil {
				logRow := domain.PlanChangeLog{
					UserID:      uid,
					ActorUserID: uid,
					ActorEmail:  systemExpiryActorEmail,
					FromPlan:    from,
					ToPlan:      domain.PlanFree,
					Reason:      "plan_expired",
				}
				if err := s.logs.CreateTx(tx, &logRow); err != nil {
					return err
				}
			}
			downgraded++
			return nil
		})
		if err != nil {
			slog.Error("premium: expiry downgrade failed",
				"user_id", uid.String(),
				"error", err,
			)
			continue
		}
	}
	slog.Info("premium: expiry downgrade complete",
		"candidates", len(candidates),
		"downgraded", downgraded,
	)
	return downgraded, nil
}

// ProgressHistoryMonths returns the lookback window for progress (0 = all time).
func (s *Service) ProgressHistoryMonths(ctx context.Context, userID uuid.UUID) (int, error) {
	q, err := s.GetRemainingQuota(ctx, userID, domain.FeatureProgressFullHistory)
	if err != nil {
		return 3, err // fail closed to Free window
	}
	if !q.Allowed {
		return 3, nil
	}
	return q.HistoryMonths, nil
}

// MaxProgressDays converts the plan's history window into a day cap.
// 0 means unlimited (Premium+). Approximate months as 30 days for range clamping.
func MaxProgressDays(historyMonths int) int {
	if historyMonths <= 0 {
		return 0
	}
	return historyMonths * 30
}

// ClampProgressRange shrinks a requested lookback to the plan's max window.
// requestedDays == 0 means "all"; clamped result may become a finite window.
func ClampProgressRange(historyMonths, requestedDays int, today time.Time) (rangeDays int, since time.Time) {
	maxDays := MaxProgressDays(historyMonths)
	today = today.UTC().Truncate(24 * time.Hour)

	if maxDays == 0 {
		if requestedDays <= 0 {
			return 0, time.Time{}
		}
		return requestedDays, today.AddDate(0, 0, -requestedDays)
	}

	if requestedDays <= 0 || requestedDays > maxDays {
		requestedDays = maxDays
	}
	return requestedDays, today.AddDate(0, 0, -requestedDays)
}

func (s *Service) loadUsage(
	ctx context.Context,
	userID uuid.UUID,
	feature domain.Feature,
	monthlyLimit int,
) (repository.UsageView, error) {
	if s == nil || s.usages == nil {
		return repository.UsageView{}, fmt.Errorf("%w", ErrUnavailable)
	}
	return s.usages.GetUsage(ctx, userID, string(feature), monthlyLimit)
}
