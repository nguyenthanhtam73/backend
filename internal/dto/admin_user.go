package dto

// AdminUserListItem is one row in GET /api/v1/admin/users.
type AdminUserListItem struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name,omitempty"`
	PlanTier    string `json:"plan_tier"`
	IsActive    bool   `json:"is_active"`
	IsAdmin     bool   `json:"is_admin"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// AdminUserListResponse is the paginated admin user search result.
type AdminUserListResponse struct {
	Items    []AdminUserListItem `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	Query    string              `json:"query,omitempty"`
}

// AdminUpdatePlanRequest is PUT /api/v1/admin/users/:id/plan.
type AdminUpdatePlanRequest struct {
	PlanTier string `json:"plan_tier"`
	Reason   string `json:"reason,omitempty"`
}

// AdminPlanChangeLogDTO is one audit row.
type AdminPlanChangeLogDTO struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	ActorUserID string `json:"actor_user_id"`
	ActorEmail  string `json:"actor_email"`
	FromPlan    string `json:"from_plan"`
	ToPlan      string `json:"to_plan"`
	Reason      string `json:"reason,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// AdminUpdatePlanResponse is returned after a successful plan change.
type AdminUpdatePlanResponse struct {
	User AdminUserListItem      `json:"user"`
	Log  AdminPlanChangeLogDTO  `json:"log"`
}

// AdminUserDetailResponse is GET /api/v1/admin/users/:id.
type AdminUserDetailResponse struct {
	User         AdminUserListItem       `json:"user"`
	RecentChanges []AdminPlanChangeLogDTO `json:"recent_changes"`
}
