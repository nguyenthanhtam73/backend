package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SkincareProduct is an item in the user's skincare wardrobe (cabinet).
type SkincareProduct struct {
	ID     uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	UserID uuid.UUID `gorm:"type:uuid;not null;index" json:"user_id"`

	Name     string `gorm:"size:200;not null" json:"name"`
	Brand    string `gorm:"size:120" json:"brand,omitempty"`
	Category string `gorm:"size:64" json:"category,omitempty"` // cleanser, serum, SPF, etc.
	Notes    string `gorm:"type:text" json:"notes,omitempty"`

	OpenedAt *time.Time `json:"opened_at,omitempty"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`

	User User `gorm:"foreignKey:UserID" json:"-"`
}

func (SkincareProduct) TableName() string {
	return "skincare_products"
}

func (p *SkincareProduct) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}
