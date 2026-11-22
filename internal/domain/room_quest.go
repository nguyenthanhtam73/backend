package domain

import (
	"time"

	"github.com/google/uuid"
)

// Room quest chests for one 8-person room that lasts 8 days.
// A perfect room is 8 members × 8 Vietnam days = 64 member-days. Real rooms
// are smaller, so the chests sit where a few friends can still open them:
//
//	5  — first chest. Five people on day one, or two people across three days.
//	15 — mid chest. About two full days if all eight show up, or two people
//	     for almost the whole room (2×8 = 16).
//	30 — big chest. About half of a perfect room: four people every day, or
//	     the full room for four days. This chest pays the most coins.
//
// Coins are credited once per (room, milestone, member).
const (
	RoomQuestTierSmall  = "small"
	RoomQuestTierMedium = "medium"
	RoomQuestTierBig    = "big"
)

// RoomQuestMilestone is one chest on the shared progress bar.
type RoomQuestMilestone struct {
	MemberDays int
	Coins      int
	Tier       string
}

// RoomQuestMilestones is the chest ladder. Order is lowest member-days first.
var RoomQuestMilestones = []RoomQuestMilestone{
	{MemberDays: 5, Coins: 40, Tier: RoomQuestTierSmall},
	{MemberDays: 15, Coins: 120, Tier: RoomQuestTierMedium},
	{MemberDays: 30, Coins: 300, Tier: RoomQuestTierBig},
}

// RoomQuestMilestoneByDays returns the chest for an exact member-day count.
func RoomQuestMilestoneByDays(days int) (RoomQuestMilestone, bool) {
	for _, m := range RoomQuestMilestones {
		if m.MemberDays == days {
			return m, true
		}
	}
	return RoomQuestMilestone{}, false
}

// MascotRoomQuestClaim is one member opening one chest.
// The primary key is the lock: a second insert for the same room, milestone,
// and user does nothing and does not pay again.
type MascotRoomQuestClaim struct {
	RoomCode   string    `gorm:"primaryKey;size:6"`
	MemberDays int       `gorm:"primaryKey"`
	UserID     uuid.UUID `gorm:"type:uuid;primaryKey"`
	Coins      int       `gorm:"not null"`
	CreatedAt  time.Time `gorm:"not null"`
}

func (MascotRoomQuestClaim) TableName() string { return "mascot_room_quest_claims" }

// MascotRoomNudge is one "Nhắc bạn" from sender to target on a Vietnam date.
// The primary key allows one nudge per sender, target, and civil day.
type MascotRoomNudge struct {
	RoomCode     string    `gorm:"primaryKey;size:6"`
	SenderUserID uuid.UUID `gorm:"type:uuid;primaryKey"`
	TargetUserID uuid.UUID `gorm:"type:uuid;primaryKey"`
	VNDate       string    `gorm:"primaryKey;size:10"`
	Message      string    `gorm:"size:240;not null"`
	CreatedAt    time.Time `gorm:"not null"`
}

func (MascotRoomNudge) TableName() string { return "mascot_room_nudges" }

// CoinReasonRoomQuestChest is the ledger reason for a room quest chest.
const CoinReasonRoomQuestChest = "room_quest_chest"

// UserCoinWallet is one account's coin balance.
// Room quest chests add to Balance. There is no spend path in this batch.
type UserCoinWallet struct {
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	Balance   int64     `gorm:"not null;default:0"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (UserCoinWallet) TableName() string { return "user_coin_wallets" }

// UserCoinLedger is one coin credit. ID is the idempotency key, so the same
// chest cannot add coins twice even if the claim insert is retried.
type UserCoinLedger struct {
	ID        string    `gorm:"primaryKey;size:80"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index"`
	Amount    int64     `gorm:"not null"`
	Reason    string    `gorm:"size:32;not null"`
	CreatedAt time.Time `gorm:"not null"`
}

func (UserCoinLedger) TableName() string { return "user_coin_ledger" }
