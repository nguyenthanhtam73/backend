package domain

import (
	"time"

	"github.com/google/uuid"
)

// Shared room tree (Cây chung). One tree per room, from the day the room
// opens until the room ends. There is no replant in this version.
//
// Stages 1..7. Reaching stage 2..7 pays that stage's reward to every real
// member in the room at the moment of the level. A person who joins later
// does not receive earlier stages. Coins and wardrobe grants stay after the
// room's 8 days end.
//
// A tree that has a gift box (leaf, in this version) pays that box at stage 7
// instead of the 150 xu and the exclusive piece. The bloom shard draw is
// unchanged: each real member can still flip one leaf.
//
// Tree types live in tree_types. Adding a type is a new row and art, not a
// code change and not a database enum. This version lets a room start as
// leaf only. Other enabled types are listed for the client to draw.

const (
	TreeTypeLeaf = "leaf"

	TreeRarityCommon = "common"
	TreeRarityRare   = "rare"

	// CoinReasonTreeStage is the user_coin_ledger reason for a tree level.
	CoinReasonTreeStage = "tree_stage"

	TreeStageCount = 7

	// Shared wardrobe pieces. Stage 7's piece comes from tree_types.exclusive_item_key.
	TreeItemSticker   = "tree_sticker_la_xanh"
	TreeItemNameFrame = "tree_frame_hoa"

	TreeItemClipLeaf      = "tree_clip_la_xanh"
	TreeItemClipCherry    = "tree_clip_hoa_anh_dao"
	TreeItemAloeWreath    = "tree_vong_la_lo_hoi"
	TreeItemSunflowerBow  = "tree_no_hoa_huong_duong"
	TreeItemClipDandelion = "tree_clip_bo_cong_anh"
	TreeItemClipQuynh     = "tree_clip_hoa_quynh"
	TreeItemClipSenHong   = "tree_clip_sen_hong"
)

// TreeTypeCreatable reports whether a room may be created as this type.
// Only leaf can be planted until seed unlocks exist. The key must still be
// an enabled tree_types row; this function is the temporary plant rule.
func TreeTypeCreatable(key string) bool {
	return key == TreeTypeLeaf
}

// TreeStage is one rung on the shared tree, including its reward.
// ItemID is empty when the rung pays coins only. Stage 1 pays nothing.
// GiftBox is set by ApplyGiftBoxStage when this tree pays a box at stage 7
// instead of Coins and ItemID.
type TreeStage struct {
	Stage    int
	Name     string
	Coins    int
	ItemID   string
	ItemName string
	GiftBox  bool
}

// TreeStages is the 7-rung ladder. exclusiveID and exclusiveName fill stage 7
// from the room's tree type. Coins for stages 2..7 sum to 420.
func TreeStages(exclusiveID, exclusiveName string) []TreeStage {
	return []TreeStage{
		{Stage: 1, Name: "Hạt giống", Coins: 0},
		{Stage: 2, Name: "Mầm non", Coins: 20},
		{Stage: 3, Name: "Cây con", Coins: 30},
		{Stage: 4, Name: "Cây nhỏ có nụ", Coins: 50, ItemID: TreeItemSticker, ItemName: "Nhãn dán lá xanh"},
		{Stage: 5, Name: "Nụ hồng hé nở", Coins: 70},
		{Stage: 6, Name: "Hoa nở", Coins: 100, ItemID: TreeItemNameFrame, ItemName: "Khung tên hoa"},
		{Stage: 7, Name: "Cây hoa rực rỡ", Coins: 150, ItemID: exclusiveID, ItemName: exclusiveName},
	}
}

// TreeStageByNumber returns one rung. Stage 7 uses the exclusive piece passed in.
func TreeStageByNumber(stage int, exclusiveID, exclusiveName string) (TreeStage, bool) {
	for _, row := range TreeStages(exclusiveID, exclusiveName) {
		if row.Stage == stage {
			return row, true
		}
	}
	return TreeStage{}, false
}

// TreeRewardCoins is the sum of stages 2..7. It does not depend on the tree type.
func TreeRewardCoins() int {
	total := 0
	for _, row := range TreeStages("", "") {
		total += row.Coins
	}
	return total
}

// TreeCatalogItems are the wardrobe rows the tree grants.
// None of them are sold: Price is nil and IsFree is false.
// The sticker and the name frame have no wear slot, so they are preview-only.
// The leaf clip wears on hair_acc. The other stage-7 pieces stay on the
// accessory slot until their own gift box ships.
// OwnedItemSource is the mascot owned_items source for a grant.
// Tree wardrobe pieces are "treeType/stage" (shared pieces use "tree").
// A shop purchase and a gift-box grant keep the source stored on the row,
// so a bought piece still shows up in owned_items as shop.
// Every other grant keeps the source stored on the row.
func OwnedItemSource(itemID, stored string) string {
	if stored == MascotSourceShop || stored == MascotSourceGiftBox {
		return stored
	}
	switch itemID {
	case TreeItemSticker:
		return "tree/4"
	case TreeItemNameFrame:
		return "tree/6"
	case TreeItemClipLeaf:
		return "leaf/7"
	case TreeItemClipCherry:
		return "cherry/7"
	case TreeItemAloeWreath:
		return "aloe/7"
	case TreeItemSunflowerBow:
		return "sunflower/7"
	case TreeItemClipDandelion:
		return "dandelion/7"
	case TreeItemClipQuynh:
		return "quynh/7"
	case TreeItemClipSenHong:
		return "sen_hong/7"
	default:
		return stored
	}
}

func TreeCatalogItems() []MascotItem {
	return []MascotItem{
		{ID: TreeItemSticker, Layer: MascotLayerSticker, Name: "Nhãn dán lá xanh", PreviewOnly: true},
		{ID: TreeItemNameFrame, Layer: MascotLayerNameFrame, Name: "Khung tên hoa", PreviewOnly: true},
		{ID: TreeItemClipLeaf, Layer: MascotLayerHairAcc, Name: "Kẹp tóc lá xanh"},
		{ID: TreeItemClipCherry, Layer: MascotLayerAccessory, Name: "Kẹp hoa anh đào"},
		{ID: TreeItemAloeWreath, Layer: MascotLayerAccessory, Name: "Vòng lá lô hội"},
		{ID: TreeItemSunflowerBow, Layer: MascotLayerAccessory, Name: "Nơ hoa hướng dương"},
		{ID: TreeItemClipDandelion, Layer: MascotLayerAccessory, Name: "Kẹp hoa bồ công anh"},
		{ID: TreeItemClipQuynh, Layer: MascotLayerAccessory, Name: "Kẹp hoa quỳnh"},
		{ID: TreeItemClipSenHong, Layer: MascotLayerAccessory, Name: "Kẹp sen hồng"},
	}
}

// TreeType is one row of the shared-tree catalog.
// Key is stable. ImageKey is what the client uses to draw. Enabled rows are
// listed by the catalog endpoint. Disabled rows stay in the table for later.
type TreeType struct {
	Key              string `gorm:"primaryKey;size:64"`
	NameVI           string `gorm:"column:name_vi;size:128;not null"`
	Rarity           string `gorm:"size:16;not null"`
	ExclusiveItemKey string `gorm:"size:64;not null"`
	ImageKey         string `gorm:"size:64;not null"`
	SortOrder        int    `gorm:"not null"`
	Enabled          bool   `gorm:"not null"`
	// Plantable is separate from Enabled. Enabled rows are the public catalog.
	// A rare type can be planted from a mystery seed while Enabled stays false,
	// so it stays out of the catalog until that user has revealed it.
	Plantable bool `gorm:"not null;default:false"`
	// MysteryPool is the rare set a Hạt bí ẩn may reveal. Sen hồng stays out
	// until a later row flips this on.
	MysteryPool bool `gorm:"not null;default:false"`
}

func (TreeType) TableName() string { return "tree_types" }

// TreeTypeSeed is the catalog shipped with this version.
// Rare types are present, disabled in the public catalog, and plantable
// through a mystery seed. Leaf is free. Other common types need a seed.
func TreeTypeSeed() []TreeType {
	return []TreeType{
		{Key: TreeTypeLeaf, NameVI: "Cây lá xanh", Rarity: TreeRarityCommon, ExclusiveItemKey: TreeItemClipLeaf, ImageKey: TreeTypeLeaf, SortOrder: 1, Enabled: true, Plantable: true},
		{Key: "cherry", NameVI: "Hoa anh đào", Rarity: TreeRarityCommon, ExclusiveItemKey: TreeItemClipCherry, ImageKey: "cherry", SortOrder: 2, Enabled: true, Plantable: true},
		{Key: "aloe", NameVI: "Lô hội", Rarity: TreeRarityCommon, ExclusiveItemKey: TreeItemAloeWreath, ImageKey: "aloe", SortOrder: 3, Enabled: true, Plantable: true},
		{Key: "sunflower", NameVI: "Hướng dương", Rarity: TreeRarityCommon, ExclusiveItemKey: TreeItemSunflowerBow, ImageKey: "sunflower", SortOrder: 4, Enabled: true, Plantable: true},
		{Key: "dandelion", NameVI: "Bồ công anh", Rarity: TreeRarityRare, ExclusiveItemKey: TreeItemClipDandelion, ImageKey: "dandelion", SortOrder: 5, Enabled: false, Plantable: true, MysteryPool: true},
		{Key: "quynh", NameVI: "Hoa quỳnh", Rarity: TreeRarityRare, ExclusiveItemKey: TreeItemClipQuynh, ImageKey: "quynh", SortOrder: 6, Enabled: false, Plantable: true, MysteryPool: true},
		{Key: "sen_hong", NameVI: "Sen hồng", Rarity: TreeRarityRare, ExclusiveItemKey: TreeItemClipSenHong, ImageKey: "sen_hong", SortOrder: 7, Enabled: false, Plantable: true},
	}
}

// MascotRoomTree is the one shared tree on a room.
// ProgressNum/ProgressDen is the banked fraction toward the next stage,
// always in [0, 1). It does not include today's waters until that Vietnam
// day closes. An immediate level leaves this as the start-of-day bank; the
// leftover (bank + today's fraction − 1) is written when the day closes.
// LeveledOn is the Vietnam date of the latest level (at most one a day).
// SettledThrough is the last Vietnam date whose close has been applied.
type MascotRoomTree struct {
	RoomCode       string    `gorm:"primaryKey;size:6"`
	Stage          int       `gorm:"not null;default:1"`
	ProgressNum    int64     `gorm:"not null;default:0"`
	ProgressDen    int64     `gorm:"not null;default:1"`
	LeveledOn      string    `gorm:"size:10;not null;default:''"`
	SettledThrough string    `gorm:"size:10;not null;default:''"`
	UpdatedAt      time.Time `gorm:"not null"`
}

func (MascotRoomTree) TableName() string { return "mascot_room_trees" }

// MascotRoomTreeWater is one real member watering once on a Vietnam date.
// The primary key blocks a second water, a fast repeat, and a replay.
// IdempotencyKey is optional. The same non-empty key reads back the first result.
type MascotRoomTreeWater struct {
	RoomCode       string    `gorm:"primaryKey;size:6;index:idx_tree_water_day,priority:1"`
	UserID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	VNDate         string    `gorm:"primaryKey;size:10;index:idx_tree_water_day,priority:2"`
	IdempotencyKey string    `gorm:"size:80;not null;default:''"`
	CreatedAt      time.Time `gorm:"not null"`
}

func (MascotRoomTreeWater) TableName() string { return "mascot_room_tree_waters" }

// MascotRoomTreeReward is one stage paid to one member of one room.
// The primary key is the "once per person per tree" lock. Deleting the room
// removes this note. The coin ledger row and the wardrobe grant do not
// reference the room, so they stay after the room ends or is deleted.
type MascotRoomTreeReward struct {
	RoomCode  string     `gorm:"primaryKey;size:6"`
	UserID    uuid.UUID  `gorm:"type:uuid;primaryKey"`
	Stage     int        `gorm:"primaryKey"`
	Coins     int        `gorm:"not null;default:0"`
	ItemID    string     `gorm:"size:64;not null;default:''"`
	CreatedAt time.Time  `gorm:"not null"`
	SeenAt    *time.Time `gorm:"column:seen_at"`
}

func (MascotRoomTreeReward) TableName() string { return "mascot_room_tree_rewards" }

// MascotRoomMemberLeave records a membership that ended, so a later read can
// still see who was in the room at 00:00 Vietnam time.
// The primary key is one stint (room, user, joined_at).
type MascotRoomMemberLeave struct {
	RoomCode string    `gorm:"primaryKey;size:6"`
	UserID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	JoinedAt time.Time `gorm:"primaryKey"`
	LeftAt   time.Time `gorm:"not null;index"`
	Kind     string    `gorm:"size:16;not null;default:user"`
}

func (MascotRoomMemberLeave) TableName() string { return "mascot_room_member_leaves" }
