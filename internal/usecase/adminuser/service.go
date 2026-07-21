// Package adminuser powers the internal admin console for user plan grants.
package adminuser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrUnavailable  = errors.New("admin user service unavailable")
	ErrInvalidInput = errors.New("invalid input")
	ErrNotFound     = errors.New("user not found")
	ErrSamePlan     = errors.New("plan unchanged")
)

// Service searches users and applies audited plan_tier changes.
type Service struct {
	db     *gorm.DB
	users  *repository.GormUserRepository
	logs   *repository.PlanChangeLogRepository
	cfg    *config.Config
}

// NewService wires admin user dependencies.
func NewService(
	db *gorm.DB,
	users *repository.GormUserRepository,
	logs *repository.PlanChangeLogRepository,
	cfg *config.Config,
) *Service {
	return &Service{db: db, users: users, logs: logs, cfg: cfg}
}

// Search returns a paginated admin user list.
func (s *Service) Search(
	ctx context.Context,
	query string,
	page, pageSize int,
) (dto.AdminUserListResponse, error) {
	var zero dto.AdminUserListResponse
	if s == nil || s.users == nil {
		return zero, ErrUnavailable
	}
	rows, total, err := s.users.SearchAdmin(ctx, repository.AdminUserSearchFilter{
		Query:    query,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		return zero, fmt.Errorf("search users: %w", err)
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	items := make([]dto.AdminUserListItem, 0, len(rows))
	for i := range rows {
		items = append(items, s.toListItem(&rows[i]))
	}
	return dto.AdminUserListResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Query:    strings.TrimSpace(query),
	}, nil
}

// Get returns one user plus recent plan-change logs.
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (dto.AdminUserDetailResponse, error) {
	var zero dto.AdminUserDetailResponse
	if s == nil || s.users == nil || s.logs == nil {
		return zero, ErrUnavailable
	}
	if userID == uuid.Nil {
		return zero, ErrInvalidInput
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return zero, err
	}
	if u == nil {
		return zero, ErrNotFound
	}
	logs, err := s.logs.ListForUser(ctx, userID, 20)
	if err != nil {
		return zero, fmt.Errorf("list plan logs: %w", err)
	}
	out := dto.AdminUserDetailResponse{
		User:          s.toListItem(u),
		RecentChanges: make([]dto.AdminPlanChangeLogDTO, 0, len(logs)),
	}
	for i := range logs {
		out.RecentChanges = append(out.RecentChanges, toLogDTO(&logs[i]))
	}
	return out, nil
}

// UpdatePlan grants/revokes a plan tier and writes an audit log.
func (s *Service) UpdatePlan(
	ctx context.Context,
	actorID uuid.UUID,
	actorEmail string,
	targetID uuid.UUID,
	req dto.AdminUpdatePlanRequest,
) (dto.AdminUpdatePlanResponse, error) {
	var zero dto.AdminUpdatePlanResponse
	if s == nil || s.db == nil || s.users == nil || s.logs == nil {
		return zero, ErrUnavailable
	}
	if actorID == uuid.Nil || targetID == uuid.Nil {
		return zero, ErrInvalidInput
	}
	to, err := parsePlanTier(req.PlanTier)
	if err != nil {
		return zero, err
	}

	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return zero, err
	}
	if target == nil {
		return zero, ErrNotFound
	}
	from := domain.NormalizePlanTier(target.PlanTier)
	if from == to {
		return zero, ErrSamePlan
	}

	reason := strings.TrimSpace(req.Reason)
	if len(reason) > 500 {
		reason = reason[:500]
	}
	actorEmail = strings.TrimSpace(actorEmail)

	var updated *domain.User
	var logRow domain.PlanChangeLog
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Admin grants are lifetime (plan_expires_at = NULL). Free clears expiry.
		u, uErr := s.users.UpdatePlanTierTx(tx, targetID, to, nil)
		if uErr != nil {
			return uErr
		}
		if u == nil {
			return ErrNotFound
		}
		updated = u
		logRow = domain.PlanChangeLog{
			UserID:      targetID,
			ActorUserID: actorID,
			ActorEmail:  actorEmail,
			FromPlan:    from,
			ToPlan:      to,
			Reason:      reason,
		}
		return s.logs.CreateTx(tx, &logRow)
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return zero, ErrNotFound
		}
		return zero, fmt.Errorf("update plan: %w", err)
	}

	slog.Info("admin: plan_tier changed",
		"actor_user_id", actorID.String(),
		"actor_email", actorEmail,
		"target_user_id", targetID.String(),
		"from_plan", string(from),
		"to_plan", string(to),
		"reason", reason,
	)

	return dto.AdminUpdatePlanResponse{
		User: s.toListItem(updated),
		Log:  toLogDTO(&logRow),
	}, nil
}

func (s *Service) toListItem(u *domain.User) dto.AdminUserListItem {
	if u == nil {
		return dto.AdminUserListItem{}
	}
	isAdmin := false
	if s.cfg != nil {
		isAdmin = s.cfg.IsAdminEmail(u.Email)
	}
	return dto.AdminUserListItem{
		ID:          u.ID.String(),
		Email:       u.Email,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		PlanTier:    string(domain.NormalizePlanTier(u.PlanTier)),
		IsActive:    u.IsActive,
		IsAdmin:     isAdmin,
		CreatedAt:   u.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   u.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toLogDTO(l *domain.PlanChangeLog) dto.AdminPlanChangeLogDTO {
	if l == nil {
		return dto.AdminPlanChangeLogDTO{}
	}
	return dto.AdminPlanChangeLogDTO{
		ID:          l.ID.String(),
		UserID:      l.UserID.String(),
		ActorUserID: l.ActorUserID.String(),
		ActorEmail:  l.ActorEmail,
		FromPlan:    string(l.FromPlan),
		ToPlan:      string(l.ToPlan),
		Reason:      l.Reason,
		CreatedAt:   l.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func parsePlanTier(raw string) (domain.PlanTier, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(domain.PlanFree):
		return domain.PlanFree, nil
	case string(domain.PlanPremium):
		return domain.PlanPremium, nil
	case string(domain.PlanPremiumPlus):
		return domain.PlanPremiumPlus, nil
	case "":
		return "", fmt.Errorf("%w: plan_tier is required", ErrInvalidInput)
	default:
		return "", fmt.Errorf("%w: plan_tier must be free, premium, or premium_plus", ErrInvalidInput)
	}
}
