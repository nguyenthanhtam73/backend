package dto

// BagStack is one shard or seed type in the bag.
type BagStack struct {
	TypeKey string `json:"type_key"`
	NameVI  string `json:"name_vi,omitempty"`
	Count   int    `json:"count"`
}

// SeedBagRules are the live numbers from seed_bag_config.
type SeedBagRules struct {
	MergeCount      int `json:"merge_count"`
	TradeInCount    int `json:"trade_in_count"`
	ShardBuyPrice   int `json:"shard_buy_price"`
	MysteryShardBps int `json:"mystery_shard_bps"`
}

// BagCatalogItem is one tree in the seed-bag grid. Leaf is not a slot.
// ComingSoon is a known type that cannot be earned yet. Blank slots are
// coming_soon_count, so slot_count = len(catalog) + coming_soon_count.
type BagCatalogItem struct {
	TypeKey       string `json:"type_key"`
	NameVI        string `json:"name_vi"`
	Rarity        string `json:"rarity"`
	ImageKey      string `json:"image_key"`
	Shards        int    `json:"shards"`
	Seeds         int    `json:"seeds"`
	Available     bool   `json:"available"`
	ComingSoon    bool   `json:"coming_soon"`
	InMysteryPool bool   `json:"in_mystery_pool"`
	Revealed      bool   `json:"revealed"`
}

// SeedBagResponse is GET /api/v1/mascot/seed-bag and a successful trade or buy.
type SeedBagResponse struct {
	Shards           []BagStack       `json:"shards"`
	MysteryShards    int              `json:"mystery_shards"`
	MysteryShardName string           `json:"mystery_shard_name"`
	Seeds            []BagStack       `json:"seeds"`
	MysterySeeds     int              `json:"mystery_seeds"`
	MysterySeedName  string           `json:"mystery_seed_name"`
	Catalog          []BagCatalogItem `json:"catalog"`
	SlotCount        int              `json:"slot_count"`
	ComingSoonCount  int              `json:"coming_soon_count"`
	Rules            SeedBagRules     `json:"rules"`
	Balance          int64            `json:"balance"`
	Already          bool             `json:"already"`
}

// DrawResponse is POST /api/v1/mascot/rooms/:code/tree/draw.
// pick is the leaf that was tapped. It does not choose the shard.
type DrawResponse struct {
	Already bool      `json:"already"`
	Pick    *int      `json:"pick,omitempty"`
	MyShard TreeShard `json:"my_shard"`
}
