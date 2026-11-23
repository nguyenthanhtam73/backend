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

// SeedBagResponse is GET /api/v1/mascot/seed-bag and a successful trade or buy.
type SeedBagResponse struct {
	Shards           []BagStack   `json:"shards"`
	MysteryShards    int          `json:"mystery_shards"`
	MysteryShardName string       `json:"mystery_shard_name"`
	Seeds            []BagStack   `json:"seeds"`
	MysterySeeds     int          `json:"mystery_seeds"`
	MysterySeedName  string       `json:"mystery_seed_name"`
	Rules            SeedBagRules `json:"rules"`
	Balance          int64        `json:"balance"`
	Already          bool         `json:"already"`
}
