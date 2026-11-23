package domain

import "testing"

func TestShopSeedTypesAreRegularOnly(t *testing.T) {
	got := map[string]struct{}{}
	for _, row := range ShopSeedTypes() {
		got[row.Key] = struct{}{}
		if row.Rarity != TreeRarityCommon || row.Key == TreeTypeLeaf || row.Key == SeedTypeMystery {
			t.Fatalf("seed %s is not a regular shop piece", row.Key)
		}
	}
	for _, key := range []string{"cherry", "aloe", "sunflower"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing regular seed %s", key)
		}
	}
	for _, key := range []string{TreeTypeLeaf, SeedTypeMystery, "dandelion", "quynh", "sen_hong"} {
		if _, ok := got[key]; ok {
			t.Fatalf("%s is listed for sale", key)
		}
	}
}

func TestShopBlocksGiftAndMystery(t *testing.T) {
	for _, id := range []string{
		TreeItemSticker, TreeItemNameFrame, TreeItemClipLeaf, "acc_no_la_xanh", "spa_bonus", "spa_02_boy",
	} {
		if _, blocked := ShopBlocked(id); !blocked {
			t.Fatalf("%s should not be sold", id)
		}
	}
	quote := QuoteShop(ShopSeedItemID, SeedTypeMystery, nil, 0, false)
	if quote.Code != "not_for_sale" || quote.TreeType != "mystery" {
		t.Fatalf("mystery quote %+v", quote)
	}
	quote = QuoteShop(ShopSeedItemID, "dandelion", nil, 0, false)
	if quote.Code != "not_for_sale" {
		t.Fatalf("rare seed quote %+v", quote)
	}
}

func TestQuoteSetChargesOnlyMissingPieces(t *testing.T) {
	owned := map[string]struct{}{"spa_01": {}}
	quote := QuoteShop("set_spa", "", owned, 0, false)
	if quote.Code != "" || quote.Charge != 16000 || len(quote.Grant) != 3 || len(quote.Bonus) != 1 {
		t.Fatalf("partial set %+v", quote)
	}
	owned["spa_02"] = struct{}{}
	owned["spa_03"] = struct{}{}
	owned["spa_04"] = struct{}{}
	quote = QuoteShop("set_spa", "", owned, 0, false)
	if quote.Code != "" || quote.Charge != 0 || len(quote.Bonus) != 1 {
		t.Fatalf("bonus only %+v", quote)
	}
	owned["spa_bonus"] = struct{}{}
	quote = QuoteShop("set_spa", "", owned, 0, false)
	if quote.Code != "already_owned" {
		t.Fatalf("complete set %+v", quote)
	}
}

func TestQuotePearlStaysLockedUntilUnlock(t *testing.T) {
	locked := QuoteShop(ShopPearlSetID, "", nil, 12, false)
	if locked.Code != "streak_locked" || locked.Streak != 12 || locked.Need != ShopPearlStreak {
		t.Fatalf("locked %+v", locked)
	}
	open := QuoteShop("ngoctrai_05", "", nil, 0, true)
	if open.Code != "" || open.Charge != 8500 {
		t.Fatalf("unlocked %+v", open)
	}
}
