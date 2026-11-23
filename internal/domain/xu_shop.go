package domain

import "fmt"

// Xu shop for Góc bình yên. Prices are xu from the same wallet as tree boxes.
// The shop sells regular pieces only. Gift-box Hiếm and Siêu hiếm pieces,
// tree-bloom rewards, and the mystery seed piece are not sold.
// Ngọc Trai is a shop set the design calls hiếm. It is sold only after the
// account has once reached a 30-day check-in streak. That is a lock, not the
// gift-box Hiếm tier.

const (
	ShopSeedItemID    = "seed_piece"
	ShopSeedPrice     = 5000
	ShopPearlStreak   = 30
	ShopPearlSetID    = "set_ngoctrai"
	ShopUnlockPearl   = "set_ngoctrai"
	CoinReasonShopBuy = "shop_buy"

	ShopGroupSets        = "sets"
	ShopGroupAccessories = "accessories"
	ShopGroupSeeds       = "seeds"
	ShopGroupDaily       = "daily"

	shopMinAccessoryPrice = 2500
)

// ShopPiece is one wearable row. Price 0 means it is not sold on its own.
// Companions are the other gender's art, granted with the piece, not sold alone.
type ShopPiece struct {
	ID         string
	Name       string
	Layer      string
	Price      int
	Companions []string
	ArtID      string
}

// ShopSet is one outfit collection. Price is the sum of its sold pieces.
type ShopSet struct {
	ID         string
	Name       string
	Pieces     []ShopPiece
	Bonus      ShopPiece
	StreakDays int
}

// ShopSKU is a piece sold outside a set.
type ShopSKU struct {
	ID    string
	Name  string
	Layer string
	Price int
	ArtID string
	Group string
}

// ShopSets is the five outfit collections. The first four sum to 25000.
// Ngọc Trai sums to 60000 and needs ShopPearlStreak once.
func ShopSets() []ShopSet {
	return []ShopSet{
		setSpa(), setLoHoi(), setNang(), setDem(), setNgocTrai(),
	}
}

// ShopAccessories is the ten pieces sold on their own.
func ShopAccessories() []ShopSKU {
	return []ShopSKU{
		{ID: "acc_kinh_mat_meo", Name: "Kính mắt mèo", Layer: MascotLayerGlasses, Price: 4500, Group: ShopGroupAccessories, ArtID: "acc_kinh_mat_meo"},
		{ID: "acc_kinh_tim", Name: "Kính tim", Layer: MascotLayerGlasses, Price: 3500, Group: ShopGroupAccessories, ArtID: "acc_kinh_tim"},
		{ID: "acc_kinh_gong_vang", Name: "Kính gọng vàng dây ngọc", Layer: MascotLayerGlasses, Price: 4000, Group: ShopGroupAccessories, ArtID: "acc_kinh_gong_vang"},
		{ID: "acc_bao_tay_ren", Name: "Bao tay ren", Layer: MascotLayerGloves, Price: 4000, Group: ShopGroupAccessories, ArtID: "acc_bao_tay_ren"},
		{ID: "acc_vong_tay_charm", Name: "Vòng tay charm", Layer: MascotLayerBracelet, Price: 3500, Group: ShopGroupAccessories, ArtID: "acc_vong_tay_charm"},
		{ID: "acc_no_lua", Name: "Nơ lụa", Layer: MascotLayerHairAcc, Price: 2500, Group: ShopGroupAccessories, ArtID: "acc_no_lua"},
		{ID: "acc_kep_ngoc_trai", Name: "Kẹp ngọc trai", Layer: MascotLayerHairAcc, Price: 3000, Group: ShopGroupAccessories, ArtID: "acc_kep_ngoc_trai"},
		{ID: "acc_khuyen_tai_giot_ngoc", Name: "Khuyên tai giọt ngọc", Layer: MascotLayerEarring, Price: 4000, Group: ShopGroupAccessories, ArtID: "acc_khuyen_tai_giot_ngoc"},
		{ID: "acc_tui_xach_mini", Name: "Túi xách mini", Layer: MascotLayerBag, Price: 5000, Group: ShopGroupAccessories, ArtID: "acc_tui_xach_mini"},
		{ID: "acc_khan_quang_co", Name: "Khăn quàng cổ", Layer: MascotLayerScarf, Price: 4500, Group: ShopGroupAccessories, ArtID: "acc_khan_quang_co"},
	}
}

// ShopDaily is the cheap placeholder shelf. ArtID can change without a new id.
func ShopDaily() []ShopSKU {
	return []ShopSKU{
		{ID: "shop_sticker_hoa", Name: "Nhãn dán hoa", Layer: MascotLayerSticker, Price: 300, Group: ShopGroupDaily, ArtID: "tree_sticker_la_xanh"},
		{ID: "shop_sticker_may", Name: "Nhãn dán mây", Layer: MascotLayerSticker, Price: 500, Group: ShopGroupDaily, ArtID: "tree_sticker_la_xanh"},
		{ID: "shop_frame_tron", Name: "Khung tên tròn", Layer: MascotLayerNameFrame, Price: 800, Group: ShopGroupDaily, ArtID: "tree_frame_hoa"},
		{ID: "shop_bubble_la", Name: "Bong bóng chat lá", Layer: MascotLayerBubble, Price: 1000, Group: ShopGroupDaily, ArtID: "bubble_la"},
	}
}

// ShopSeedTypes are the regular tree pieces the shop sells. Leaf is free.
// Rare trees and the mystery piece are not in this list.
func ShopSeedTypes() []TreeType {
	out := make([]TreeType, 0, 3)
	for _, row := range TreeTypeSeed() {
		if row.Key == TreeTypeLeaf || row.Rarity != TreeRarityCommon || !row.Plantable || !row.Enabled {
			continue
		}
		out = append(out, row)
	}
	return out
}

// ShopMascotItems is every wardrobe row the shop can grant.
func ShopMascotItems() []MascotItem {
	seen := map[string]struct{}{}
	var out []MascotItem
	add := func(id, name, layer string, price int) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		item := MascotItem{ID: id, Name: name, Layer: layer}
		if price > 0 {
			p := price
			item.Price = &p
		}
		out = append(out, item)
	}
	for _, set := range ShopSets() {
		for _, piece := range set.Pieces {
			add(piece.ID, piece.Name, piece.Layer, piece.Price)
			for _, id := range piece.Companions {
				add(id, piece.Name+" · Nam", piece.Layer, 0)
			}
		}
		add(set.Bonus.ID, set.Bonus.Name, set.Bonus.Layer, 0)
	}
	for _, row := range ShopAccessories() {
		add(row.ID, row.Name, row.Layer, row.Price)
	}
	for _, row := range ShopDaily() {
		add(row.ID, row.Name, row.Layer, row.Price)
	}
	return out
}

// ShopBlocked reports why an id must not be sold. Gift-box pieces, bloom
// rewards, set bonuses, and gender companions are blocked.
func ShopBlocked(id string) (reason string, blocked bool) {
	if shopBonus(id) {
		return "set_bonus", true
	}
	if shopCompanion(id) {
		return "companion", true
	}
	if shopGiftOrBloom(id) {
		return "gift_box", true
	}
	return "", false
}

// ShopQuote is the charge and the grants for one buy, before the wallet moves.
type ShopQuote struct {
	Kind     string
	ItemID   string
	TreeType string
	Charge   int
	Grant    []ShopPiece
	Bonus    []ShopPiece
	Code     string
	Streak   int
	Need     int
}

// QuoteShop prices one purchase. owned is the wardrobe ids already held.
// unlocked is the permanent Ngọc Trai flag. streak is the live check-in streak.
func QuoteShop(itemID, treeType string, owned map[string]struct{}, streak int, unlocked bool) ShopQuote {
	itemID = trimShopID(itemID)
	treeType = trimShopID(treeType)
	if itemID == ShopSeedItemID {
		return quoteSeed(treeType)
	}
	if reason, blocked := ShopBlocked(itemID); blocked {
		return ShopQuote{ItemID: itemID, Code: "not_for_sale", TreeType: reason}
	}
	if set, ok := shopSetByID(itemID); ok {
		return quoteSet(set, owned, streak, unlocked)
	}
	if piece, set, ok := shopPieceByID(itemID); ok {
		if set.StreakDays > 0 && !unlocked && streak < set.StreakDays {
			return ShopQuote{ItemID: itemID, Code: "streak_locked", Streak: streak, Need: set.StreakDays}
		}
		if _, has := owned[piece.ID]; has {
			return ShopQuote{ItemID: itemID, Code: "already_owned"}
		}
		q := ShopQuote{Kind: "item", ItemID: itemID, Charge: piece.Price, Grant: []ShopPiece{piece}}
		if setComplete(set, owned, piece.ID) {
			q.Bonus = []ShopPiece{set.Bonus}
		}
		return q
	}
	if _, ok := shopSKU(itemID); ok {
		row, _ := shopSKU(itemID)
		if _, has := owned[row.ID]; has {
			return ShopQuote{ItemID: itemID, Code: "already_owned"}
		}
		return ShopQuote{Kind: "item", ItemID: itemID, Charge: row.Price, Grant: []ShopPiece{{
			ID: row.ID, Name: row.Name, Layer: row.Layer, Price: row.Price, ArtID: row.ArtID,
		}}}
	}
	return ShopQuote{ItemID: itemID, Code: "not_for_sale"}
}

func quoteSeed(treeType string) ShopQuote {
	if treeType == "" {
		return ShopQuote{ItemID: ShopSeedItemID, Code: "not_for_sale", TreeType: "missing_tree"}
	}
	if treeType == SeedTypeMystery {
		return ShopQuote{ItemID: ShopSeedItemID, Code: "not_for_sale", TreeType: "mystery"}
	}
	var name string
	ok := false
	for _, row := range ShopSeedTypes() {
		if row.Key == treeType {
			ok = true
			name = row.NameVI
		}
	}
	if !ok {
		return ShopQuote{ItemID: ShopSeedItemID, Code: "not_for_sale", TreeType: "tree_type"}
	}
	return ShopQuote{
		Kind: ShopGroupSeeds, ItemID: ShopSeedItemID, TreeType: treeType, Charge: ShopSeedPrice,
		Grant: []ShopPiece{{ID: "seed:" + treeType, Name: "Mảnh hạt " + name, Layer: "seed"}},
	}
}

func quoteSet(set ShopSet, owned map[string]struct{}, streak int, unlocked bool) ShopQuote {
	if set.StreakDays > 0 && !unlocked && streak < set.StreakDays {
		return ShopQuote{ItemID: set.ID, Code: "streak_locked", Streak: streak, Need: set.StreakDays}
	}
	var grant []ShopPiece
	charge := 0
	for _, piece := range set.Pieces {
		if _, has := owned[piece.ID]; has {
			continue
		}
		grant = append(grant, piece)
		charge += piece.Price
	}
	if len(grant) == 0 {
		if _, has := owned[set.Bonus.ID]; has || set.Bonus.ID == "" {
			return ShopQuote{ItemID: set.ID, Code: "already_owned"}
		}
		return ShopQuote{Kind: "set", ItemID: set.ID, Charge: 0, Bonus: []ShopPiece{set.Bonus}}
	}
	q := ShopQuote{Kind: "set", ItemID: set.ID, Charge: charge, Grant: grant}
	if setComplete(set, owned, grantIDs(grant)...) {
		q.Bonus = []ShopPiece{set.Bonus}
	}
	return q
}

func setComplete(set ShopSet, owned map[string]struct{}, extra ...string) bool {
	have := map[string]struct{}{}
	for id := range owned {
		have[id] = struct{}{}
	}
	for _, id := range extra {
		have[id] = struct{}{}
	}
	for _, piece := range set.Pieces {
		if _, ok := have[piece.ID]; !ok {
			return false
		}
	}
	return set.Bonus.ID != ""
}

func grantIDs(pieces []ShopPiece) []string {
	out := make([]string, len(pieces))
	for i, piece := range pieces {
		out[i] = piece.ID
	}
	return out
}

func shopSetByID(id string) (ShopSet, bool) {
	for _, set := range ShopSets() {
		if set.ID == id {
			return set, true
		}
	}
	return ShopSet{}, false
}

func shopPieceByID(id string) (ShopPiece, ShopSet, bool) {
	for _, set := range ShopSets() {
		for _, piece := range set.Pieces {
			if piece.ID == id {
				return piece, set, true
			}
		}
	}
	return ShopPiece{}, ShopSet{}, false
}

func shopSKU(id string) (ShopSKU, bool) {
	for _, row := range ShopAccessories() {
		if row.ID == id {
			return row, true
		}
	}
	for _, row := range ShopDaily() {
		if row.ID == id {
			return row, true
		}
	}
	return ShopSKU{}, false
}

func shopBonus(id string) bool {
	for _, set := range ShopSets() {
		if set.Bonus.ID == id {
			return true
		}
	}
	return false
}

func shopCompanion(id string) bool {
	for _, set := range ShopSets() {
		for _, piece := range set.Pieces {
			for _, extra := range piece.Companions {
				if extra == id {
					return true
				}
			}
		}
	}
	return false
}

func shopGiftOrBloom(id string) bool {
	for _, item := range TreeCatalogItems() {
		if item.ID == id {
			return true
		}
	}
	for _, box := range GiftBoxes() {
		for _, item := range GiftWardrobeItems(box) {
			if item.ID == id {
				return true
			}
		}
		for _, prize := range box.Prizes {
			for _, item := range prize.Items {
				if item.ID == id {
					return true
				}
			}
		}
	}
	return false
}

func trimShopID(id string) string {
	for len(id) > 0 && (id[0] == ' ' || id[0] == '\n' || id[0] == '\t') {
		id = id[1:]
	}
	for len(id) > 0 && (id[len(id)-1] == ' ' || id[len(id)-1] == '\n' || id[len(id)-1] == '\t') {
		id = id[:len(id)-1]
	}
	return id
}

func setSpa() ShopSet {
	return ShopSet{
		ID: "set_spa", Name: "Spa Sáng Sớm",
		Pieces: []ShopPiece{
			{ID: "spa_01", Name: "Áo choàng tắm bông", Layer: MascotLayerOutfit, Price: 9000, ArtID: "spa_01"},
			{ID: "spa_02", Name: "Khăn quấn tóc tai thỏ", Layer: MascotLayerHat, Price: 6500, Companions: []string{"spa_02_boy"}, ArtID: "spa_02"},
			{ID: "spa_03", Name: "Dép lông", Layer: MascotLayerShoes, Price: 5000, ArtID: "spa_03"},
			{ID: "spa_04", Name: "Ly trà hoa cúc", Layer: MascotLayerHeld, Price: 4500, ArtID: "spa_04"},
		},
		Bonus: ShopPiece{ID: "spa_bonus", Name: "Hơi nước lấp lánh", Layer: MascotLayerFX, ArtID: "spa_bonus"},
	}
}

func setLoHoi() ShopSet {
	return ShopSet{
		ID: "set_lohoi", Name: "Vườn Lô Hội",
		Pieces: []ShopPiece{
			{ID: "lohoi_01", Name: "Yếm xanh lá", Layer: MascotLayerOutfit, Price: 9000, Companions: []string{"lohoi_01_boy"}, ArtID: "lohoi_01"},
			{ID: "lohoi_02", Name: "Bình tưới mini", Layer: MascotLayerHeld, Price: 6000, ArtID: "lohoi_02"},
			{ID: "lohoi_03", Name: "Mũ lá lô hội", Layer: MascotLayerHat, Price: 5000, ArtID: "lohoi_03"},
			{ID: "lohoi_04", Name: "Giày búp lá", Layer: MascotLayerShoes, Price: 5000, ArtID: "lohoi_04"},
		},
		Bonus: ShopPiece{ID: "lohoi_bonus", Name: "Chậu lô hội trong phòng", Layer: MascotLayerRoom, ArtID: "lohoi_bonus"},
	}
}

func setNang() ShopSet {
	return ShopSet{
		ID: "set_nang", Name: "Nắng Hè Xinh",
		Pieces: []ShopPiece{
			{ID: "nang_01", Name: "Áo khoác chống nắng", Layer: MascotLayerOutfit, Price: 9000, Companions: []string{"nang_01_boy"}, ArtID: "nang_01"},
			{ID: "nang_02", Name: "Mũ rộng vành", Layer: MascotLayerHat, Price: 7000, ArtID: "nang_02"},
			{ID: "nang_03", Name: "Ô nhỏ cầm tay", Layer: MascotLayerHeld, Price: 5000, ArtID: "nang_03"},
			{ID: "nang_04", Name: "Kính tròn", Layer: MascotLayerGlasses, Price: 4000, ArtID: "nang_04"},
		},
		Bonus: ShopPiece{ID: "nang_bonus", Name: "Khung tên mặt trời", Layer: MascotLayerNameFrame, ArtID: "nang_bonus"},
	}
}

func setDem() ShopSet {
	return ShopSet{
		ID: "set_dem", Name: "Đêm Ngủ Ngon",
		Pieces: []ShopPiece{
			{ID: "dem_01", Name: "Đồ ngủ in sao", Layer: MascotLayerOutfit, Price: 9000, ArtID: "dem_01"},
			{ID: "dem_02", Name: "Gối ôm", Layer: MascotLayerHeld, Price: 6500, ArtID: "dem_02"},
			{ID: "dem_03", Name: "Mũ ngủ hình trăng", Layer: MascotLayerHat, Price: 5000, ArtID: "dem_03"},
			{ID: "dem_04", Name: "Bịt mắt mây", Layer: MascotLayerGlasses, Price: 4500, ArtID: "dem_04"},
		},
		Bonus: ShopPiece{ID: "dem_bonus", Name: "Sao bay lúc đắp mặt nạ", Layer: MascotLayerFX, ArtID: "dem_bonus"},
	}
}

func setNgocTrai() ShopSet {
	return ShopSet{
		ID: ShopPearlSetID, Name: "Ngọc Trai Hoàng Gia", StreakDays: ShopPearlStreak,
		Pieces: []ShopPiece{
			{ID: "ngoctrai_01", Name: "Vương miện ngọc trai", Layer: MascotLayerHat, Price: 16000, ArtID: "ngoctrai_01"},
			{ID: "ngoctrai_02", Name: "Áo choàng ánh hồng", Layer: MascotLayerOutfit, Price: 15000, Companions: []string{"ngoctrai_02_boy"}, ArtID: "ngoctrai_02"},
			{ID: "ngoctrai_03", Name: "Gậy ngọc trai", Layer: MascotLayerHeld, Price: 11000, ArtID: "ngoctrai_03"},
			{ID: "ngoctrai_04", Name: "Vòng cổ ngọc trai", Layer: MascotLayerNecklace, Price: 9500, ArtID: "ngoctrai_04"},
			{ID: "ngoctrai_05", Name: "Giày ngọc trai", Layer: MascotLayerShoes, Price: 8500, ArtID: "ngoctrai_05"},
		},
		Bonus: ShopPiece{ID: "ngoctrai_bonus", Name: "Hào quang cả phòng", Layer: MascotLayerFX, ArtID: "ngoctrai_bonus"},
	}
}

func init() {
	for _, set := range ShopSets() {
		sum := 0
		for _, piece := range set.Pieces {
			if piece.Price <= 0 {
				panic(fmt.Sprintf("shop piece %s has no price", piece.ID))
			}
			sum += piece.Price
		}
		want := 25000
		if set.ID == ShopPearlSetID {
			want = 60000
		}
		if sum != want {
			panic(fmt.Sprintf("shop set %s sums to %d", set.ID, sum))
		}
	}
	for _, row := range ShopAccessories() {
		if row.Price < shopMinAccessoryPrice || row.Price > 5000 {
			panic(fmt.Sprintf("accessory %s price %d", row.ID, row.Price))
		}
	}
	for _, row := range ShopDaily() {
		if row.Price < 300 || row.Price > 1000 {
			panic(fmt.Sprintf("daily %s price %d", row.ID, row.Price))
		}
	}
	if len(ShopSeedTypes()) == 0 {
		panic("shop has no regular seed types")
	}
}
