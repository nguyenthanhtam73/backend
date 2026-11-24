package domain

import (
	"time"

	"github.com/google/uuid"
)

// UserShopOrder is one successful xu purchase. The idempotency key is the
// lock that keeps a double tap from charging twice.
type UserShopOrder struct {
	UserID     uuid.UUID `gorm:"type:uuid;primaryKey"`
	IdemKey    string    `gorm:"primaryKey;size:80"`
	ItemID     string    `gorm:"size:64;not null"`
	TreeType   string    `gorm:"size:64;not null;default:''"`
	Charged    int       `gorm:"not null"`
	GrantedIDs string    `gorm:"size:512;not null;default:''"`
	CreatedAt  time.Time `gorm:"not null"`

	User User `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
}

func (UserShopOrder) TableName() string { return "user_shop_orders" }

// UserShopUnlock is a permanent shop gate. Ngọc Trai stays open after the
// check-in streak that first reached 30 days later breaks.
type UserShopUnlock struct {
	UserID     uuid.UUID `gorm:"type:uuid;primaryKey"`
	UnlockKey  string    `gorm:"primaryKey;size:64"`
	UnlockedAt time.Time `gorm:"not null"`

	User User `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
}

func (UserShopUnlock) TableName() string { return "user_shop_unlocks" }
