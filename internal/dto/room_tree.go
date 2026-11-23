package dto

// TreeReward is one rung, or one rung this member has already received.
type TreeReward struct {
	Stage       int    `json:"stage"`
	Name        string `json:"name,omitempty"`
	Coins       int    `json:"coins"`
	ItemID      string `json:"item_id,omitempty"`
	ItemName    string `json:"item_name,omitempty"`
	PreviewOnly bool   `json:"preview_only,omitempty"`
}

// RoomTreeResponse is GET /api/v1/mascot/rooms/:code/tree and the body of a
// successful POST /api/v1/mascot/rooms/:code/tree/water.
//
// stage is 1..7. progress is the fraction toward the next stage, from 0 up to
// and including values at or above 1 when today's partial water plus the bank
// has filled the bar but the day has not closed yet. A full day (waters >=
// required, required > 0) levels immediately and progress resets.
// watered is display names only, in the order they watered today.
// can_water is false with reason needs_skin_check, already_watered, bloomed,
// or room_ended.
// rewards is the full ladder for this tree type. my_rewards is what the caller
// has been paid on this tree. just_granted is what this call paid them.
type RoomTreeResponse struct {
	Stage       int          `json:"stage"`
	StageName   string       `json:"stage_name"`
	TreeType    string       `json:"tree_type"`
	TreeName    string       `json:"tree_name"`
	Rarity      string       `json:"rarity"`
	ImageKey    string       `json:"image_key"`
	Waters      int          `json:"waters"`
	Required    int          `json:"required"`
	Watered     []string     `json:"watered"`
	Progress    float64      `json:"progress"`
	CanWater    bool         `json:"can_water"`
	Reason      string       `json:"reason,omitempty"`
	Bloomed     bool         `json:"bloomed"`
	Rewards     []TreeReward `json:"rewards"`
	MyRewards   []TreeReward `json:"my_rewards"`
	Balance     int64        `json:"balance"`
	Already     bool         `json:"already"`
	Leveled     bool         `json:"leveled"`
	JustGranted []TreeReward `json:"just_granted,omitempty"`
}

// TreeCatalogEntry is one enabled tree the client can draw.
// Creatable is true only for leaf until seed unlocks exist.
type TreeCatalogEntry struct {
	Key               string `json:"key"`
	NameVI            string `json:"name_vi"`
	Rarity            string `json:"rarity"`
	ImageKey          string `json:"image_key"`
	SortOrder         int    `json:"sort_order"`
	ExclusiveItemKey  string `json:"exclusive_item_key"`
	ExclusiveItemName string `json:"exclusive_item_name,omitempty"`
	Creatable         bool   `json:"creatable"`
}

// TreeCatalogResponse is GET /api/v1/mascot/tree-types.
type TreeCatalogResponse struct {
	Types []TreeCatalogEntry `json:"types"`
}
