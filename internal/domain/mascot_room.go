package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// How long a Góc bình yên room stays active.
// MascotRoomDuration is the length written for a room created from now on.
// LegacyMascotRoomDuration is the previous length. A room with no stored end
// is treated as created_at plus that value. Backfill then sets every existing
// room to the earlier of that end and (when the backfill runs + MascotRoomDuration).
// A room that has already ended is not given a later end, and nothing is deleted.
const (
	MascotRoomDuration       = 8 * 24 * time.Hour
	LegacyMascotRoomDuration = 21 * 24 * time.Hour
)

// MascotRoom is one 8-person invite room (phòng).
// The code is 6 uppercase characters with no 0, O, 1, or I.
// A new room ends at CreatedAt + MascotRoomDuration. Ending does not delete
// the row or its members; those people are simply no longer in an active room.
// Membership lives in MascotRoomMember. Closing a room does not delete
// cosmetic item grants; those rows have no foreign key to this table.
type MascotRoom struct {
	Code        string    `gorm:"primaryKey;size:6" json:"code"`
	OwnerUserID uuid.UUID `gorm:"type:uuid;not null;index" json:"-"`
	CreatedAt   time.Time `gorm:"not null" json:"created_at"`
	EndsAt      time.Time `gorm:"not null;index" json:"-"`
	MaxMembers  int       `gorm:"not null;default:8" json:"-"`
	// TreeType is the shared tree for this room (tree_types.key).
	// It is chosen when the room is created. Empty is stored as leaf.
	// There is no database enum: new types are rows, not a migration.
	TreeType string `gorm:"size:64;not null;default:leaf" json:"-"`
}

// BeforeCreate stores the default leaf when the caller left the type blank.
func (r *MascotRoom) BeforeCreate(tx *gorm.DB) error {
	if r == nil {
		return nil
	}
	if strings.TrimSpace(r.TreeType) == "" {
		r.TreeType = TreeTypeLeaf
	}
	return nil
}

func (MascotRoom) TableName() string { return "mascot_rooms" }

// MascotRoomMember is one account in a room. At most MaxMembers rows per room.
// Nickname is the Bé Da persona from the live join. It is not the account name.
// There is no kick, ban, or remove-other-member. A row disappears only when
// that account leaves, or when the account itself is deleted.
// New creates and joins allow one active room per account. An account that
// already sits in several active rooms keeps every row until those rooms end
// or the account leaves.
// Room member kinds. Only RoomMemberUser is a real person.
// A future virtual buddy is stored with another kind (for example "ai").
// The shared tree counts and waters real members only. An AI row cannot water
// and does not change the day's required count.
const (
	RoomMemberUser = "user"
	RoomMemberAI   = "ai"
)

// RealRoomMember reports a human account. Blank is treated as user so rows
// written before the kind column still count.
func RealRoomMember(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "", RoomMemberUser:
		return true
	default:
		return false
	}
}

type MascotRoomMember struct {
	RoomCode string    `gorm:"primaryKey;size:6" json:"-"`
	UserID   uuid.UUID `gorm:"type:uuid;primaryKey;index" json:"-"`
	JoinedAt time.Time `gorm:"not null" json:"-"`
	Nickname string    `gorm:"size:96;not null;default:''" json:"-"`
	Kind     string    `gorm:"size:16;not null;default:user" json:"-"`
}

// BeforeCreate marks a blank kind as a real member.
func (m *MascotRoomMember) BeforeCreate(tx *gorm.DB) error {
	if m == nil {
		return nil
	}
	if strings.TrimSpace(m.Kind) == "" {
		m.Kind = RoomMemberUser
	}
	return nil
}

func (MascotRoomMember) TableName() string { return "mascot_room_members" }

// MascotRoomActiveSeat is the race-safe "one active room" slot.
// user_id is the primary key, so two concurrent joins cannot both commit a
// seat. Accounts that already belong to more than one active room have no
// seat row; their memberships stay, and create/join refuses them until none
// of those rooms are still active. A seat for a room that has ended is
// removed on the next create or join.
type MascotRoomActiveSeat struct {
	UserID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	RoomCode string    `gorm:"size:6;not null;index"`
	EndsAt   time.Time `gorm:"not null"`
}

func (MascotRoomActiveSeat) TableName() string { return "mascot_room_active_seats" }
