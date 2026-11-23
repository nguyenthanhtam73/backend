package domain

import (
	"math"
	"math/rand"
	"testing"
)

func TestLeafGiftBoxCatalog(t *testing.T) {
	def, ok := GiftBoxByTree(TreeTypeLeaf)
	if !ok {
		t.Fatal("leaf box missing")
	}
	prizes, err := WeightedPrizes(def)
	if err != nil {
		t.Fatal(err)
	}
	if len(prizes) != 16 {
		t.Fatalf("prizes %d", len(prizes))
	}
	items := map[string]GiftItem{}
	sets := 0
	acc := 0
	for _, prize := range prizes {
		if prize.Kind == GiftKindSet {
			sets++
		} else {
			acc++
		}
		for _, item := range prize.Items {
			items[item.ID] = item
		}
	}
	if sets != 8 || acc != 8 || len(items) != 24 {
		t.Fatalf("sets %d accessories %d items %d", sets, acc, len(items))
	}
	clip := items[TreeItemClipLeaf]
	if clip.ArtID != GiftPrizeClipLeaf || clip.Slot != MascotLayerHairAcc || !clip.Legacy {
		t.Fatalf("clip %+v", clip)
	}
	if _, ok := items[GiftPrizeClipLeaf]; ok {
		t.Fatal("design id must not be a second wardrobe row")
	}
	wardrobe := GiftWardrobeItems(def)
	if len(wardrobe) != 23 {
		t.Fatalf("new rows %d", len(wardrobe))
	}
	for _, item := range wardrobe {
		if item.ID == TreeItemClipLeaf || item.IsFree || item.Price != nil {
			t.Fatalf("sold or duplicated %+v", item)
		}
	}
	if _, ok := GiftBoxByTree("cherry"); ok {
		t.Fatal("cherry box is data for a later version")
	}
	leaf := ApplyGiftBoxStage(TreeTypeLeaf, TreeStages(TreeItemClipLeaf, "Kẹp tóc lá xanh")[6])
	if !leaf.GiftBox || leaf.Coins != 0 || leaf.ItemID != "" {
		t.Fatalf("leaf stage 7 %+v", leaf)
	}
	cherry := ApplyGiftBoxStage("cherry", TreeStages(TreeItemClipCherry, "Kẹp hoa anh đào")[6])
	if cherry.GiftBox || cherry.Coins != 150 || cherry.ItemID != TreeItemClipCherry {
		t.Fatalf("cherry stage 7 %+v", cherry)
	}
}

func TestGiftRatesSplitEvenly(t *testing.T) {
	def, _ := GiftBoxByTree(TreeTypeLeaf)
	prizes, err := WeightedPrizes(def)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		n, each, coins int
	}{
		GiftRarityCommon: {10, 890, 8},
		GiftRarityRare:   {4, 250, 88},
		GiftRarityUltra:  {2, 50, 188},
	}
	got := map[string]int{}
	sum := 0
	for _, prize := range prizes {
		got[prize.Rarity]++
		sum += prize.WeightBps
		band := want[prize.Rarity]
		if prize.WeightBps != band.each || prize.DupeCoins != band.coins {
			t.Fatalf("prize %+v", prize)
		}
	}
	if sum != GiftRollSpan {
		t.Fatalf("sum %d", sum)
	}
	for key, band := range want {
		if got[key] != band.n {
			t.Fatalf("%s count %d", key, got[key])
		}
	}
}

func TestPickGiftPrizeBoundaries(t *testing.T) {
	def, _ := GiftBoxByTree(TreeTypeLeaf)
	prizes, err := WeightedPrizes(def)
	if err != nil {
		t.Fatal(err)
	}
	cursor := 0
	for i, prize := range prizes {
		first, err := PickGiftPrize(prizes, cursor)
		if err != nil || first.ID != prize.ID {
			t.Fatalf("start %d got %+v %v", i, first.ID, err)
		}
		last, err := PickGiftPrize(prizes, cursor+prize.WeightBps-1)
		if err != nil || last.ID != prize.ID {
			t.Fatalf("end %d got %+v %v", i, last.ID, err)
		}
		cursor += prize.WeightBps
	}
	if _, err := PickGiftPrize(prizes, -1); err == nil {
		t.Fatal("negative roll")
	}
	if _, err := PickGiftPrize(prizes, GiftRollSpan); err == nil {
		t.Fatal("span roll")
	}
}

func TestGiftPrizeDistribution(t *testing.T) {
	def, _ := GiftBoxByTree(TreeTypeLeaf)
	prizes, err := WeightedPrizes(def)
	if err != nil {
		t.Fatal(err)
	}
	const n = 200000
	rng := rand.New(rand.NewSource(1))
	counts := map[string]int{}
	for i := 0; i < n; i++ {
		prize, err := PickGiftPrize(prizes, rng.Intn(GiftRollSpan))
		if err != nil {
			t.Fatal(err)
		}
		counts[prize.ID]++
	}
	for _, prize := range prizes {
		want := float64(n) * float64(prize.WeightBps) / float64(GiftRollSpan)
		got := float64(counts[prize.ID])
		sigma := math.Sqrt(want * (1 - float64(prize.WeightBps)/float64(GiftRollSpan)))
		if math.Abs(got-want) > 6*sigma {
			t.Fatalf("%s got %.0f want %.0f sigma %.1f", prize.ID, got, want, sigma)
		}
	}
}

func TestCryptoGiftRollSpan(t *testing.T) {
	seen := map[int]struct{}{}
	for i := 0; i < 64; i++ {
		n, err := CryptoGiftRoll()
		if err != nil || n < 0 || n >= GiftRollSpan {
			t.Fatalf("roll %d %v", n, err)
		}
		seen[n] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatal("crypto roll did not vary")
	}
}

func TestResolveGiftGrant(t *testing.T) {
	def, _ := GiftBoxByTree(TreeTypeLeaf)
	prizes, err := WeightedPrizes(def)
	if err != nil {
		t.Fatal(err)
	}
	var set, clip GiftPrize
	for _, prize := range prizes {
		if prize.ID == "set_la_non" {
			set = prize
		}
		if prize.ID == GiftPrizeClipLeaf {
			clip = prize
		}
	}
	fresh := ResolveGiftGrant(set, nil)
	if fresh.Duplicate || fresh.Partial || fresh.DupeCoins != 0 || len(fresh.GrantIDs) != 2 {
		t.Fatalf("fresh %+v", fresh)
	}
	one := ResolveGiftGrant(set, map[string]struct{}{set.Items[0].ID: {}})
	if one.Duplicate || !one.Partial || one.DupeCoins != 0 || len(one.GrantIDs) != 1 || one.GrantIDs[0] != set.Items[1].ID {
		t.Fatalf("partial %+v", one)
	}
	both := ResolveGiftGrant(set, map[string]struct{}{set.Items[0].ID: {}, set.Items[1].ID: {}})
	if !both.Duplicate || both.Partial || both.DupeCoins != 8 || len(both.GrantIDs) != 0 {
		t.Fatalf("duplicate set %+v", both)
	}
	ownedClip := ResolveGiftGrant(clip, map[string]struct{}{TreeItemClipLeaf: {}})
	if !ownedClip.Duplicate || ownedClip.DupeCoins != 8 || len(ownedClip.GrantIDs) != 0 {
		t.Fatalf("duplicate clip %+v", ownedClip)
	}
	rare := prizes[0]
	for _, prize := range prizes {
		if prize.Rarity == GiftRarityRare && prize.Kind == GiftKindSet {
			rare = prize
			break
		}
	}
	dupeRare := ResolveGiftGrant(rare, map[string]struct{}{rare.Items[0].ID: {}, rare.Items[1].ID: {}})
	if dupeRare.DupeCoins != 88 {
		t.Fatalf("rare dupe %+v", dupeRare)
	}
}

func TestResolveClothing(t *testing.T) {
	outfit := MascotLoadout{Outfit: MascotItemOutfitCozyPajamas}
	if got := ResolveClothing(outfit); got.Outfit != MascotItemOutfitCozyPajamas {
		t.Fatalf("outfit %+v", got)
	}
	over := outfit
	over.Top = "top_la_non"
	over.Bottom = "bottom_quan_short_kem"
	got := ResolveClothing(over)
	if got.Outfit != "" || got.Top != "top_la_non" || got.Bottom != "bottom_quan_short_kem" {
		t.Fatalf("top replaces outfit %+v", got)
	}
	onlyTop := ResolveClothing(MascotLoadout{Outfit: MascotItemOutfitCozyPajamas, Top: "top_la_non", Bottom: "bottom_kept"})
	if onlyTop.Outfit != "" || onlyTop.Top != "top_la_non" || onlyTop.Bottom != "bottom_kept" {
		t.Fatalf("kept bottom %+v", onlyTop)
	}
}

func TestOwnedItemSourceKeepsShopAndBox(t *testing.T) {
	if OwnedItemSource(TreeItemClipLeaf, MascotSourceShop) != MascotSourceShop {
		t.Fatal("shop grant was relabeled")
	}
	if OwnedItemSource(TreeItemClipLeaf, MascotSourceGiftBox) != MascotSourceGiftBox {
		t.Fatal("box grant was relabeled")
	}
	if OwnedItemSource(TreeItemClipLeaf, MascotSourceRoomReward) != "leaf/7" {
		t.Fatal("bloom grant source")
	}
}
