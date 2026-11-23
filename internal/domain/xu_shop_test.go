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

func TestShopBubbleColoursWearArtID(t *testing.T) {
	want := map[string]struct {
		art   string
		price int
	}{
		"shop_bubble_kem_hong": {"kem_hong", 400},
		"shop_bubble_mint_mat": {"mint_mat", 400},
		"shop_bubble_vang_mat": {"vang_mat", 450},
		"shop_bubble_dao_cam":  {"dao_cam", 450},
	}
	got := map[string]ShopSKU{}
	for _, row := range ShopDaily() {
		got[row.ID] = row
	}
	for id, spec := range want {
		row, ok := got[id]
		if !ok || row.EquipID() != spec.art || row.ArtID != spec.art || row.Price != spec.price || row.Layer != MascotLayerBubble {
			t.Fatalf("%s %+v", id, row)
		}
		quote := QuoteShop(id, "", nil, 0, false)
		if quote.Code != "" || quote.Charge != spec.price || len(quote.Grant) != 1 || quote.Grant[0].ID != spec.art {
			t.Fatalf("quote %s %+v", id, quote)
		}
	}
	leaf := got["shop_bubble_la"]
	if leaf.EquipID() != "shop_bubble_la" || leaf.Price != 1000 {
		t.Fatalf("leaf bubble %+v", leaf)
	}
	if QuoteShop("kem_hong", "", nil, 0, false).Code != "not_for_sale" {
		t.Fatal("art id is sold on its own")
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
