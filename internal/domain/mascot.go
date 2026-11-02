package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Mascot layers. room items live in loadout.room_items; the others are single slots.
const (
	MascotLayerBody      = "body"
	MascotLayerHair      = "hair"
	MascotLayerOutfit    = "outfit"
	MascotLayerAccessory = "accessory"
	MascotLayerRoom      = "room"
)

// Free starter ids. A later shop inserts more mascot_items rows; loadout stays JSON.
const (
	MascotItemBodyDefault      = "body_default"
	MascotItemHairDefault      = "hair_default"
	MascotItemOutfitDefault    = "outfit_default"
	MascotItemAccessoryDefault = "accessory_default"
	MascotItemRoomPlant        = "room_plant"
)

// MascotItem is one catalog row. Price is nil when the item is not sold.
type MascotItem struct {
	ID     string `gorm:"primaryKey;size:64" json:"id"`
	Layer  string `gorm:"size:32;not null" json:"layer"`
	Name   string `gorm:"size:128;not null" json:"name"`
	IsFree bool   `gorm:"not null" json:"is_free"`
	Price  *int   `json:"price,omitempty"`
}

func (MascotItem) TableName() string { return "mascot_items" }

// DefaultMascotItems are the free starters. Keep migrations/030_user_mascot.up.sql in sync.
func DefaultMascotItems() []MascotItem {
	return []MascotItem{
		{ID: MascotItemBodyDefault, Layer: MascotLayerBody, Name: "Bé Da", IsFree: true},
		{ID: MascotItemHairDefault, Layer: MascotLayerHair, Name: "Tóc mềm", IsFree: true},
		{ID: MascotItemOutfitDefault, Layer: MascotLayerOutfit, Name: "Áo thun", IsFree: true},
		{ID: MascotItemAccessoryDefault, Layer: MascotLayerAccessory, Name: "Nơ nhỏ", IsFree: true},
		{ID: MascotItemRoomPlant, Layer: MascotLayerRoom, Name: "Cây nhỏ", IsFree: true},
	}
}

// MascotLoadout is the equipped look. Stored as JSON so new items need no migration.
type MascotLoadout struct {
	Body      string   `json:"body"`
	Hair      string   `json:"hair"`
	Outfit    string   `json:"outfit"`
	Accessory string   `json:"accessory"`
	RoomItems []string `json:"room_items"`
}

// DefaultMascotLoadout is the free look created on the first GET.
func DefaultMascotLoadout() MascotLoadout {
	return MascotLoadout{
		Body:      MascotItemBodyDefault,
		Hair:      MascotItemHairDefault,
		Outfit:    MascotItemOutfitDefault,
		Accessory: MascotItemAccessoryDefault,
		RoomItems: []string{MascotItemRoomPlant},
	}
}

// UserMascot is one row per account. Mood is not stored; it is derived on read.
type UserMascot struct {
	UserID    uuid.UUID       `gorm:"type:uuid;primaryKey" json:"user_id"`
	Loadout   json.RawMessage `gorm:"type:jsonb;not null" json:"loadout"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`

	User User `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
}

func (UserMascot) TableName() string { return "user_mascot" }
