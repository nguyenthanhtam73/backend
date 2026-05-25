package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RoutineEntry stores a user's skincare routine for a calendar day (AM/PM steps as JSON).
//
// One row per (user, routine_date). The Morning / Evening columns hold an
// ordered JSON array of step objects (see dto.RoutineStep). We deliberately
// keep step shape in JSON instead of a separate `routine_steps` table because:
//   - steps are tiny and always loaded together with their parent day,
//   - reordering / re-templating doesn't need referential FK fan-out,
//   - it matches the way the AI service returns them (already JSON arrays).
//
// Source string tags an entry as "manual" (user edited), "ai_suggested" (AI
// applied), or "carried_over" (frontend rolled yesterday forward) — handy for
// future analytics on whether AI suggestions actually stick.
type RoutineEntry struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	RoutineDate time.Time       `gorm:"type:date;not null;index" json:"routine_date"`
	Morning     json.RawMessage `gorm:"type:jsonb" json:"morning,omitempty"`
	Evening     json.RawMessage `gorm:"type:jsonb" json:"evening,omitempty"`
	Notes       string          `gorm:"type:text" json:"notes,omitempty"`
	// Source describes how this entry was produced: "manual" | "ai_suggested" | "carried_over".
	Source string `gorm:"size:24;default:manual" json:"source,omitempty"`
	// SkillMode (beginner|intermediate|advanced) is captured when an entry is
	// AI-generated so future analytics can compare adherence by mode.
	SkillMode string `gorm:"size:24" json:"skill_mode,omitempty"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

func (RoutineEntry) TableName() string {
	return "routine_entries"
}

func (r *RoutineEntry) BeforeCreate(tx *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}
