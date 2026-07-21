package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PlanChangeLog records an admin grant/revoke of a user's plan_tier.
// Used for internal testing audit trails (who changed what, when).
type PlanChangeLog struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID      uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`       // target account
	ActorUserID uuid.UUID `gorm:"type:uuid;not null;index" json:"actor_user_id"` // admin who changed
	ActorEmail  string    `gorm:"size:255;not null" json:"actor_email"`
	FromPlan    PlanTier  `gorm:"size:16;not null" json:"from_plan"`
	ToPlan      PlanTier  `gorm:"size:16;not null" json:"to_plan"`
	Reason      string    `gorm:"type:text" json:"reason,omitempty"`

	CreatedAt time.Time `json:"created_at"`

	User  User `gorm:"foreignKey:UserID" json:"-"`
	Actor User `gorm:"foreignKey:ActorUserID" json:"-"`
}

func (PlanChangeLog) TableName() string {
	return "plan_change_logs"
}

func (l *PlanChangeLog) BeforeCreate(tx *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return nil
}
