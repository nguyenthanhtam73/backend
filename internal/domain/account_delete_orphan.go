package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AccountDeleteOrphanKey is an object account deletion could not remove.
// The account row is already gone, so this stores a hash of the user id
// and the object key for a later cleanup. It has no foreign key to users.
type AccountDeleteOrphanKey struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	UserIDHash string    `gorm:"size:64;not null;index"`
	ObjectKey  string    `gorm:"type:text;not null"`
	LastError  string    `gorm:"type:text"`
	CreatedAt  time.Time
}

func (AccountDeleteOrphanKey) TableName() string {
	return "account_delete_orphan_keys"
}

func (o *AccountDeleteOrphanKey) BeforeCreate(tx *gorm.DB) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	if o.CreatedAt.IsZero() {
		o.CreatedAt = time.Now().UTC()
	}
	return nil
}
