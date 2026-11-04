package domain

import (
	"time"

	"github.com/google/uuid"
)

// MascotRoom is one 8-person invite room (phòng).
// The code is 6 uppercase characters with no 0, O, 1, or I.
// Membership lives in MascotRoomMember. Closing a room does not delete
// cosmetic item grants; those rows have no foreign key to this table.
type MascotRoom struct {
	Code        string    `gorm:"primaryKey;size:6" json:"code"`
	OwnerUserID uuid.UUID `gorm:"type:uuid;not null;index" json:"-"`
	CreatedAt   time.Time `gorm:"not null" json:"created_at"`
	MaxMembers  int       `gorm:"not null;default:8" json:"-"`
}

func (MascotRoom) TableName() string { return "mascot_rooms" }

// MascotRoomMember is one account in a room. At most MaxMembers rows per room.
// Nickname is the Bé Da persona from the live join. It is not the account name.
// There is no kick, ban, or remove-other-member. A row disappears only when
// that account leaves, or when the account itself is deleted.
type MascotRoomMember struct {
	RoomCode string    `gorm:"primaryKey;size:6" json:"-"`
	UserID   uuid.UUID `gorm:"type:uuid;primaryKey;index" json:"-"`
	JoinedAt time.Time `gorm:"not null" json:"-"`
	Nickname string    `gorm:"size:96;not null;default:''" json:"-"`
}

func (MascotRoomMember) TableName() string { return "mascot_room_members" }
