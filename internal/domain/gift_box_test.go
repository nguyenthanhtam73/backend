package domain

import (
	"fmt"
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
	if len(prizes) != 20 {
		t.Fatalf("prizes %d", len(prizes))
	}
	items := map[string]GiftItem{}
	sets, acc, coins := 0, 0, 0
	for _, prize := range prizes {
		switch prize.Kind {
		case GiftKindSet:
			sets++
		case GiftKindAccessory:
			acc++
		case GiftKindCoins:
			coins++
		default:
			t.Fatalf("kind %s", prize.Kind)
		}
		for _, item := range prize.Items {
			items[item.ID] = item
		}
	}
	if sets != 8 || acc != 8 || coins != 4 || len(items) != 24 {
		t.Fatalf("sets %d accessories %d coins %d items %d", sets, acc, coins, len(items))
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
		n, each, dupe, band int
	}{
		GiftRarityCommon: {10, 479, 8, 4790},
		GiftRarityRare:   {4, 50, 88, 200},
		GiftRarityUltra:  {2, 5, 188, 10},
	}
	got := map[string]int{}
	bandSum := map[string]int{}
	sum := 0
	for _, prize := range prizes {
		got[prize.Rarity]++
		sum += prize.WeightBps
		bandSum[prize.Rarity] += prize.WeightBps
		if prize.Kind == GiftKindCoins {
			continue
		}
		band := want[prize.Rarity]
		if prize.WeightBps != band.each || prize.DupeCoins != band.dupe || prize.Coins != 0 {
			t.Fatalf("prize %+v", prize)
		}
	}
	if sum != GiftRollSpan {
		t.Fatalf("sum %d", sum)
	}
	for key, band := range want {
		if got[key] != band.n || bandSum[key] != band.band {
			t.Fatalf("%s count %d sum %d", key, got[key], bandSum[key])
		}
	}
	coinWant := map[string]struct{ weight, coins int }{
		"coins_8":  {2000, 8},
		"coins_18": {1500, 18},
		"coins_28": {1000, 28},
		"coins_88": {500, 88},
	}
	if got[GiftRarityCoins] != len(coinWant) || bandSum[GiftRarityCoins] != 5000 {
		t.Fatalf("xu count %d sum %d", got[GiftRarityCoins], bandSum[GiftRarityCoins])
	}
	for _, prize := range prizes {
		if prize.Kind != GiftKindCoins {
			continue
		}
		want := coinWant[prize.ID]
		if prize.WeightBps != want.weight || prize.Coins != want.coins || prize.DupeCoins != 0 || prize.Name != fmt.Sprintf("%d xu", want.coins) {
			t.Fatalf("xu prize %+v", prize)
		}
		delete(coinWant, prize.ID)
	}
	if len(coinWant) != 0 {
		t.Fatalf("missing xu prizes %#v", coinWant)
	}
}

func TestTierPercentsAreExact(t *testing.T) {
	rates := GiftTierRates()
	sum := 0
	byKey := map[string]GiftTierRate{}
	for _, rate := range rates {
		sum += rate.WeightBps
		byKey[rate.Key] = rate
	}
	if sum != GiftRollSpan {
		t.Fatalf("sum %d", sum)
	}
	// 5000/10000 = 50%, 4790/10000 = 47.9%, 200/10000 = 2%, 10/10000 = 0.1%.
	if byKey[GiftRarityCoins].WeightBps*2 != GiftRollSpan {
		t.Fatal("xu is not 50%")
	}
	if byKey[GiftRarityCommon].WeightBps*1000 != 479*GiftRollSpan {
		t.Fatal("thuong is not 47.9%")
	}
	if byKey[GiftRarityRare].WeightBps*50 != GiftRollSpan {
		t.Fatal("hiem is not 2%")
	}
	if byKey[GiftRarityUltra].WeightBps*1000 != GiftRollSpan {
		t.Fatal("sieu is not 0.1%")
	}
	if byKey[GiftRarityCommon].Each*10 != byKey[GiftRarityCommon].WeightBps {
		t.Fatal("thuong items do not split the band")
	}
	if byKey[GiftRarityRare].Each*4 != byKey[GiftRarityRare].WeightBps {
		t.Fatal("hiem items do not split the band")
	}
	if byKey[GiftRarityUltra].Each*2 != byKey[GiftRarityUltra].WeightBps {
		t.Fatal("sieu items do not split the band")
	}
}

func TestMissingUltraPoolKeepsItsWeight(t *testing.T) {
	def := leafGiftBox()
	kept := make([]GiftPrize, 0, len(def.Prizes))
	for _, prize := range def.Prizes {
		if prize.Rarity == GiftRarityUltra {
			continue
		}
		kept = append(kept, prize)
	}
	def.Prizes = kept
	prizes, err := WeightedPrizes(def)
	if err != nil {
		t.Fatal(err)
	}
	sum := 0
	bands := map[string]int{}
	var fallback GiftPrize
	found := false
	for _, prize := range prizes {
		sum += prize.WeightBps
		bands[prize.Rarity] += prize.WeightBps
		if prize.ID == GiftUltraFallbackID {
			fallback = prize
			found = true
		}
	}
	if sum != GiftRollSpan {
		t.Fatalf("sum %d", sum)
	}
	if bands[GiftRarityCoins] != 5000 || bands[GiftRarityCommon] != 4790 || bands[GiftRarityRare] != 200 || bands[GiftRarityUltra] != 10 {
		t.Fatalf("bands %#v", bands)
	}
	if !found || !fallback.Fallback || fallback.Kind != GiftKindFallback || fallback.Coins != GiftUltraFallbackCoins || fallback.WeightBps != 10 || fallback.Rarity != GiftRarityUltra {
		t.Fatalf("fallback %+v", fallback)
	}
	grant := ResolveGiftGrant(fallback, map[string]struct{}{"anything": {}})
	if grant.Duplicate || grant.DupeCoins != 0 || grant.Coins != GiftUltraFallbackCoins {
		t.Fatalf("grant %+v", grant)
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
	var xu GiftPrize
	for _, prize := range prizes {
		if prize.ID == "coins_88" {
			xu = prize
		}
	}
	paid := ResolveGiftGrant(xu, map[string]struct{}{TreeItemClipLeaf: {}})
	if paid.Duplicate || paid.Partial || paid.DupeCoins != 0 || paid.Coins != 88 || len(paid.GrantIDs) != 0 {
		t.Fatalf("xu prize %+v", paid)
	}
}

func TestCoinRatesAreSharedAcrossTrees(t *testing.T) {
	leaf, err := WeightedPrizes(leafGiftBox())
	if err != nil {
		t.Fatal(err)
	}
	other := leafGiftBox()
	other.TreeType = "cherry"
	for i := range other.Prizes {
		other.Prizes[i].ID = "cherry_" + other.Prizes[i].ID
		for j := range other.Prizes[i].Items {
			other.Prizes[i].Items[j].ID = "cherry_" + other.Prizes[i].Items[j].ID
		}
	}
	got, err := WeightedPrizes(other)
	if err != nil {
		t.Fatal(err)
	}
	shared := GiftCoinPrizes()
	if len(leaf) < len(shared) || len(got) < len(shared) {
		t.Fatal("short prize list")
	}
	for i, prize := range shared {
		if leaf[i].ID != prize.ID || leaf[i].WeightBps != prize.WeightBps || leaf[i].Coins != prize.Coins {
			t.Fatalf("leaf xu %+v", leaf[i])
		}
		if got[i].ID != prize.ID || got[i].WeightBps != prize.WeightBps || got[i].Coins != prize.Coins {
			t.Fatalf("other tree copied a different xu table %+v", got[i])
		}
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
