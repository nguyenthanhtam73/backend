package dto

// GiftBoxItem is one wardrobe piece inside a prize.
// id is the mascot item to grant and equip. art_id is the design file.
// They match except for the old leaf clip: id is tree_clip_la_xanh and
// art_id is acc_kep_toc_la_xanh. slot is top, bottom, hair_acc, hat,
// earring, glasses, cheek, bracelet, or bag.
type GiftBoxItem struct {
	ID     string `json:"id"`
	ArtID  string `json:"art_id"`
	Name   string `json:"name"`
	Slot   string `json:"slot"`
	Set    string `json:"set,omitempty"`
	Legacy bool   `json:"legacy,omitempty"`
}

// GiftBoxSummary is one box on the caller's list, or the box that was opened.
type GiftBoxSummary struct {
	ID        string  `json:"id"`
	TreeType  string  `json:"tree_type"`
	ArtKey    string  `json:"art_key"`
	Name      string  `json:"name"`
	TreeName  string  `json:"tree_name"`
	RoomCode  string  `json:"room_code"`
	CreatedAt string  `json:"created_at"`
	Opened    bool    `json:"opened"`
	OpenedAt  *string `json:"opened_at,omitempty"`
}

// GiftBoxListResponse is GET /api/v1/mascot/gift-boxes.
// boxes is only the unopened ones, oldest first.
type GiftBoxListResponse struct {
	Boxes []GiftBoxSummary `json:"boxes"`
}

// GiftPrizeView is one outcome on the rate table and on an open.
// kind is set, accessory, or coins. weight_bps is the server rate out of
// 10000. coins is the xu paid when kind is coins. The client does not roll.
type GiftPrizeView struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Rarity     string        `json:"rarity"`
	RarityName string        `json:"rarity_name"`
	Kind       string        `json:"kind"`
	WeightBps  int           `json:"weight_bps"`
	DupeCoins  int           `json:"dupe_coins"`
	Coins      int           `json:"coins,omitempty"`
	Items      []GiftBoxItem `json:"items"`
}

// GiftBoxOpenResponse is POST /api/v1/mascot/gift-boxes/:id/open.
// A second call returns the same prize with already true and does not grant again.
// duplicate means the whole set or the accessory was already owned, so dupe_coins
// were added and granted_items is empty. partial means one piece of a set was
// missing: that piece is in granted_items and dupe_coins is 0.
// coins is set when the prize itself is xu. A xu prize is never a duplicate.
// balance is the wallet after this open.
type GiftBoxOpenResponse struct {
	Already      bool           `json:"already"`
	Box          GiftBoxSummary `json:"box"`
	Prize        GiftPrizeView  `json:"prize"`
	GrantedItems []GiftBoxItem  `json:"granted_items"`
	Duplicate    bool           `json:"duplicate"`
	Partial      bool           `json:"partial"`
	DupeCoins    int            `json:"dupe_coins"`
	Coins        int            `json:"coins,omitempty"`
	Balance      int64          `json:"balance"`
}

// GiftRateTier is one band on the «i» screen.
type GiftRateTier struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	WeightBps   int    `json:"weight_bps"`
	PrizeCount  int    `json:"prize_count"`
	PerPrizeBps int    `json:"per_prize_bps"`
	DupeCoins   int    `json:"dupe_coins"`
}

// GiftRateCard is the rate table for one tree. Boxes are not sold.
type GiftRateCard struct {
	TreeType string          `json:"tree_type"`
	ArtKey   string          `json:"art_key"`
	Name     string          `json:"name"`
	TreeName string          `json:"tree_name"`
	Tiers    []GiftRateTier  `json:"tiers"`
	Prizes   []GiftPrizeView `json:"prizes"`
}

// GiftRatesResponse is GET /api/v1/mascot/gift-boxes/rates.
// Optional query tree_type limits boxes to that tree.
type GiftRatesResponse struct {
	Boxes []GiftRateCard `json:"boxes"`
}
