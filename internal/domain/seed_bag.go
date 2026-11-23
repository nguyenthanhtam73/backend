package domain

import (
	"time"

	"github.com/google/uuid"
)

// Seed bag. Shards drop only when a room tree blooms. Five shards of one
// type become one seed. A seed plants one room. Mystery is not a tree type.
const (
	SeedKindShard = "shard"
	SeedKindSeed  = "seed"

	// SeedTypeMystery is the bag key for Cánh bí ẩn / Hạt bí ẩn.
	SeedTypeMystery = "mystery"

	MysteryShardName = "Cánh bí ẩn"
	MysterySeedName  = "Hạt bí ẩn"

	CoinReasonShardBuy = "shard_buy"

	SeedSourceBloom    = "bloom"
	SeedSourceTradeIn  = "trade_in"
	SeedSourceTradeOut = "trade_out"
	SeedSourceBuy      = "buy"
	SeedSourceMerge    = "merge"
	SeedSourcePlant    = "plant"

	SeedConfigMysteryBps = "mystery_shard_bps"
	SeedConfigBuyPrice   = "shard_buy_price"
	SeedConfigMergeCount = "merge_count"
	SeedConfigTradeCount = "trade_in_count"
	SeedConfigSlotCount  = "bag_slot_count"
)

// SeedBagConfig is one numeric rule. Prices and rates live here, not in code.
type SeedBagConfig struct {
	Key      string `gorm:"primaryKey;size:64"`
	IntValue int    `gorm:"not null"`
}

func (SeedBagConfig) TableName() string { return "seed_bag_config" }

// SeedBagDefaults are inserted when missing. A later edit of the row is kept.
func SeedBagDefaults() []SeedBagConfig {
	return []SeedBagConfig{
		{Key: SeedConfigMysteryBps, IntValue: 1000},
		{Key: SeedConfigBuyPrice, IntValue: 5000},
		{Key: SeedConfigMergeCount, IntValue: 5},
		{Key: SeedConfigTradeCount, IntValue: 3},
		{Key: SeedConfigSlotCount, IntValue: 18},
	}
}

// UserSeedBalance is how many shards or seeds of one type an account holds.
type UserSeedBalance struct {
	UserID  uuid.UUID `gorm:"type:uuid;primaryKey"`
	Kind    string    `gorm:"primaryKey;size:16"`
	TypeKey string    `gorm:"primaryKey;size:64"`
	Count   int       `gorm:"not null"`
}

func (UserSeedBalance) TableName() string { return "user_seed_balances" }

// UserSeedLedger is one shard or seed change. ID is the idempotency key.
// Delta is positive when pieces are added and negative when they leave.
// Source is bloom, trade_in, trade_out, buy, merge, or plant.
type UserSeedLedger struct {
	ID        string    `gorm:"primaryKey;size:80"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index"`
	Kind      string    `gorm:"size:16;not null"`
	TypeKey   string    `gorm:"size:64;not null"`
	Delta     int       `gorm:"not null"`
	Source    string    `gorm:"size:16;not null"`
	CreatedAt time.Time `gorm:"not null"`
}

func (UserSeedLedger) TableName() string { return "user_seed_ledger" }

// RoomBloomShard is one real member's leaf draw for a room's bloom.
// The row is created when the tree blooms, with an empty TypeKey.
// POST draw fills TypeKey once. The primary key blocks a second grant.
// Pick is the leaf the client tapped (0–2). It does not change the shard.
type RoomBloomShard struct {
	RoomCode       string     `gorm:"primaryKey;size:6"`
	UserID         uuid.UUID  `gorm:"type:uuid;primaryKey"`
	TypeKey        string     `gorm:"size:64;not null;default:''"`
	Pick           *int       `gorm:""`
	IdempotencyKey string     `gorm:"size:80;not null;default:''"`
	ClaimedAt      *time.Time `gorm:""`
	CreatedAt      time.Time  `gorm:"not null"`
}

func (RoomBloomShard) TableName() string { return "mascot_room_bloom_shards" }

// UserTreeReveal is a rare type this account has planted from a mystery seed.
// GET tree-types includes it for them. It stays out of everyone else's catalog.
type UserTreeReveal struct {
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	TypeKey   string    `gorm:"primaryKey;size:64"`
	CreatedAt time.Time `gorm:"not null"`
}

func (UserTreeReveal) TableName() string { return "user_tree_reveals" }
