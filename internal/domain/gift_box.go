package domain

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
)

// Gift box for a shared room tree. Opening one box yields exactly one prize.
// A set prize grants the top and the bottom together. An accessory prize
// grants that one piece. Rates do not move with the number of opens.
//
// Leaf ships now. The other five trees (cherry, aloe, sunflower, dandelion,
// quynh) are another GiftBoxDef in GiftBoxes, using the same GiftTierRates.
// Sen hồng is not part of this set. Boxes are not sold.

const (
	GiftKindSet       = "set"
	GiftKindAccessory = "accessory"

	GiftRarityCommon = "thuong"
	GiftRarityRare   = "hiem"
	GiftRarityUltra  = "sieu"

	// GiftRollSpan is the denominator for every prize weight.
	// 890 weight units is 8.9 percent.
	GiftRollSpan = 10000

	// CoinReasonGiftBoxDupe is the ledger reason when a box prize is already owned.
	CoinReasonGiftBoxDupe = "gift_box_dupe"

	// GiftBoxArtLeaf is the design folder for the leaf box (tree type "leaf").
	GiftBoxArtLeaf = "la_xanh"

	// GiftPrizeClipLeaf is the prize id of the old leaf clip.
	// The wardrobe row stays TreeItemClipLeaf so people who already own it
	// still own it. ArtID on that piece is this prize id.
	GiftPrizeClipLeaf = "acc_kep_toc_la_xanh"
)

// GiftTierRate is one rarity band. WeightBps is the band's share of GiftRollSpan.
// The band is split evenly across the prizes of that rarity.
type GiftTierRate struct {
	Key       string
	Name      string
	WeightBps int
	DupeCoins int
}

// GiftTierRates is the fixed rate table for every tree box.
func GiftTierRates() []GiftTierRate {
	return []GiftTierRate{
		{Key: GiftRarityCommon, Name: "Thường", WeightBps: 8900, DupeCoins: 8},
		{Key: GiftRarityRare, Name: "Hiếm", WeightBps: 1000, DupeCoins: 88},
		{Key: GiftRarityUltra, Name: "Siêu hiếm", WeightBps: 100, DupeCoins: 188},
	}
}

// GiftItem is one wardrobe piece inside a prize.
// ID is the mascot_items id that gets granted. ArtID is the design file id.
// They match except for the old leaf clip.
type GiftItem struct {
	ID     string
	ArtID  string
	Name   string
	Slot   string
	Set    string
	Legacy bool
}

// GiftPrize is one of the 16 outcomes.
type GiftPrize struct {
	ID        string
	Name      string
	Rarity    string
	Kind      string
	Items     []GiftItem
	WeightBps int
	DupeCoins int
}

// GiftBoxDef is one tree's box. Prizes are listed in rate-table order.
// WeightBps on each prize is filled by WeightedPrizes.
type GiftBoxDef struct {
	TreeType string
	ArtKey   string
	Name     string
	TreeName string
	Prizes   []GiftPrize
}

// GiftBoxes is the shipped catalog. Append a def to add a tree.
func GiftBoxes() []GiftBoxDef {
	return []GiftBoxDef{leafGiftBox()}
}

// GiftBoxByTree returns the box for a tree type key such as "leaf".
func GiftBoxByTree(treeType string) (GiftBoxDef, bool) {
	for _, box := range GiftBoxes() {
		if box.TreeType == treeType {
			return box, true
		}
	}
	return GiftBoxDef{}, false
}

// ApplyGiftBoxStage replaces stage 7's coins and exclusive piece with a box
// when this tree has a catalog. Other stages and other trees stay on the ladder.
func ApplyGiftBoxStage(treeType string, stage TreeStage) TreeStage {
	if stage.Stage != TreeStageCount {
		return stage
	}
	if _, ok := GiftBoxByTree(treeType); !ok {
		return stage
	}
	stage.Coins = 0
	stage.ItemID = ""
	stage.ItemName = ""
	stage.GiftBox = true
	return stage
}

// WeightedPrizes copies the box prizes and fills each weight from GiftTierRates.
// A band that does not split evenly across its prizes is an error.
func WeightedPrizes(def GiftBoxDef) ([]GiftPrize, error) {
	rates := GiftTierRates()
	byKey := make(map[string]GiftTierRate, len(rates))
	total := 0
	for _, rate := range rates {
		if rate.Key == "" || rate.WeightBps <= 0 || rate.DupeCoins <= 0 {
			return nil, fmt.Errorf("gift tier %q is incomplete", rate.Key)
		}
		if _, dup := byKey[rate.Key]; dup {
			return nil, fmt.Errorf("duplicate gift tier %s", rate.Key)
		}
		byKey[rate.Key] = rate
		total += rate.WeightBps
	}
	if total != GiftRollSpan {
		return nil, fmt.Errorf("gift tier weights sum to %d", total)
	}
	counts := map[string]int{}
	for _, prize := range def.Prizes {
		counts[prize.Rarity]++
	}
	for _, rate := range rates {
		n := counts[rate.Key]
		if n == 0 || rate.WeightBps%n != 0 {
			return nil, fmt.Errorf("tier %s weight %d does not split across %d prizes", rate.Key, rate.WeightBps, n)
		}
	}
	out := make([]GiftPrize, len(def.Prizes))
	seen := map[string]struct{}{}
	for i, prize := range def.Prizes {
		if _, dup := seen[prize.ID]; dup {
			return nil, fmt.Errorf("duplicate gift prize %s", prize.ID)
		}
		seen[prize.ID] = struct{}{}
		rate, ok := byKey[prize.Rarity]
		if !ok {
			return nil, fmt.Errorf("prize %s has unknown rarity %s", prize.ID, prize.Rarity)
		}
		switch prize.Kind {
		case GiftKindSet:
			if len(prize.Items) != 2 {
				return nil, fmt.Errorf("set %s must grant two pieces", prize.ID)
			}
		case GiftKindAccessory:
			if len(prize.Items) != 1 {
				return nil, fmt.Errorf("accessory %s must grant one piece", prize.ID)
			}
		default:
			return nil, fmt.Errorf("prize %s has unknown kind %s", prize.ID, prize.Kind)
		}
		prize.WeightBps = rate.WeightBps / counts[prize.Rarity]
		prize.DupeCoins = rate.DupeCoins
		out[i] = prize
	}
	sum := 0
	for _, prize := range out {
		sum += prize.WeightBps
	}
	if sum != GiftRollSpan {
		return nil, fmt.Errorf("prize weights sum to %d", sum)
	}
	return out, nil
}

// PickGiftPrize returns the prize for a roll in [0, GiftRollSpan).
func PickGiftPrize(prizes []GiftPrize, roll int) (GiftPrize, error) {
	if roll < 0 || roll >= GiftRollSpan {
		return GiftPrize{}, fmt.Errorf("gift roll out of range")
	}
	cursor := 0
	for _, prize := range prizes {
		if prize.WeightBps <= 0 {
			return GiftPrize{}, fmt.Errorf("prize %s has no weight", prize.ID)
		}
		cursor += prize.WeightBps
		if roll < cursor {
			return prize, nil
		}
	}
	return GiftPrize{}, fmt.Errorf("gift prizes do not cover the roll")
}

// GiftRollStart is the first roll that lands on prize id, using prize order.
func GiftRollStart(prizes []GiftPrize, id string) (int, bool) {
	cursor := 0
	for _, prize := range prizes {
		if prize.ID == id {
			return cursor, true
		}
		cursor += prize.WeightBps
	}
	return 0, false
}

// CryptoGiftRoll draws one roll with crypto/rand.
func CryptoGiftRoll() (int, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(GiftRollSpan))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}

// GiftGrant is what an open gives after comparing the prize with ownership.
// Duplicate means the whole set, or the accessory, is already owned: pay DupeCoins
// and grant nothing. Partial means one piece of a set was missing: grant that
// piece and pay nothing.
type GiftGrant struct {
	Prize     GiftPrize
	GrantIDs  []string
	DupeCoins int
	Duplicate bool
	Partial   bool
}

// ResolveGiftGrant applies the duplicate rule. owned is the set of item ids
// the account already holds.
func ResolveGiftGrant(prize GiftPrize, owned map[string]struct{}) GiftGrant {
	missing := make([]string, 0, len(prize.Items))
	have := 0
	for _, item := range prize.Items {
		if _, ok := owned[item.ID]; ok {
			have++
			continue
		}
		missing = append(missing, item.ID)
	}
	if len(missing) == 0 {
		return GiftGrant{Prize: prize, DupeCoins: prize.DupeCoins, Duplicate: true}
	}
	return GiftGrant{
		Prize:    prize,
		GrantIDs: missing,
		Partial:  have > 0 && len(prize.Items) > 1,
	}
}

// GiftWardrobeItems is every new mascot_items row for this box.
// The legacy leaf clip is already in the tree catalog, so it is left out.
func GiftWardrobeItems(def GiftBoxDef) []MascotItem {
	seen := map[string]struct{}{}
	out := make([]MascotItem, 0, 24)
	for _, prize := range def.Prizes {
		for _, item := range prize.Items {
			if item.ID == "" || item.ID == TreeItemClipLeaf {
				continue
			}
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			out = append(out, MascotItem{ID: item.ID, Layer: item.Slot, Name: item.Name})
		}
	}
	return out
}

// TreeGiftBox is one unopened or opened box. One real member receives one box
// when that room's tree blooms. The room code is a note: deleting the room
// does not delete the box, and the box is not sold.
type TreeGiftBox struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey"`
	RoomCode       string    `gorm:"size:6;not null;uniqueIndex:ux_tree_gift_box_room_user"`
	UserID         uuid.UUID `gorm:"type:uuid;not null;index;uniqueIndex:ux_tree_gift_box_room_user"`
	TreeType       string    `gorm:"size:64;not null"`
	CreatedAt      time.Time `gorm:"not null"`
	OpenedAt       *time.Time
	PrizeID        string `gorm:"size:64;not null;default:''"`
	GrantedItemIDs string `gorm:"size:256;not null;default:''"`
	DupeCoins      int    `gorm:"not null;default:0"`

	User User `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
}

func (TreeGiftBox) TableName() string { return "tree_gift_boxes" }

func init() {
	for _, box := range GiftBoxes() {
		if _, err := WeightedPrizes(box); err != nil {
			panic(err)
		}
	}
}

func leafGiftBox() GiftBoxDef {
	return GiftBoxDef{
		TreeType: TreeTypeLeaf,
		ArtKey:   GiftBoxArtLeaf,
		Name:     "Hộp quà Lá xanh",
		TreeName: "Cây lá xanh",
		Prizes: []GiftPrize{
			setPrize("set_la_non", "Bộ Lá Non", GiftRarityCommon, "Lá Non",
				"top_la_non", "Áo thun Lá Non", "bottom_quan_short_kem", "Quần short Kem Sữa"),
			setPrize("set_suong_som", "Bộ Sương Sớm", GiftRarityCommon, "Sương Sớm",
				"top_ao_len_suong_som", "Áo len Sương Sớm", "bottom_vay_xep_ly_ma_non", "Váy xếp ly Mạ Non"),
			setPrize("set_co_la_xinh", "Bộ Cổ Lá Xinh", GiftRarityCommon, "Cổ Lá Xinh",
				"top_ao_co_la", "Áo cổ Lá Xinh", "bottom_quan_dai_reu_nhat", "Quần dài Rêu Nhạt"),
			setPrize("set_ke_la_me", "Bộ Kẻ Lá Me", GiftRarityCommon, "Kẻ Lá Me",
				"top_ao_ke_la_me", "Áo kẻ Lá Me", "bottom_chan_vay_la_dam", "Chân váy Lá Đậm"),
			setPrize("set_dao_vuon", "Bộ Dạo Vườn", GiftRarityCommon, "Dạo Vườn",
				"top_ao_dao_vuon", "Áo túi lá Dạo Vườn", "bottom_quan_lung_kaki", "Quần lửng Kaki và ủng xanh"),
			accPrize(GiftPrizeClipLeaf, "Kẹp tóc lá xanh", GiftRarityCommon, MascotLayerHairAcc, true, TreeItemClipLeaf),
			accPrize("acc_sticker_ma_la", "Sticker má Lá Nhỏ", GiftRarityCommon, MascotLayerCheek, false, ""),
			accPrize("acc_kinh_tron_la_ma", "Kính tròn Lá Mạ", GiftRarityCommon, MascotLayerGlasses, false, ""),
			accPrize("acc_vong_tay_hat_la", "Vòng tay Hạt Lá", GiftRarityCommon, MascotLayerBracelet, false, ""),
			accPrize("acc_no_la_xanh", "Nơ Lá Xanh", GiftRarityCommon, MascotLayerHairAcc, false, ""),
			setPrize("set_giot_suong_mai", "Bộ Giọt Sương Mai", GiftRarityRare, "Giọt Sương Mai",
				"top_ao_tay_phong_giot_suong", "Áo tay phồng Giọt Sương", "bottom_vay_tang_la_biec", "Váy tầng Lá Biếc"),
			setPrize("set_rung_nho", "Bộ Rừng Nhỏ", GiftRarityRare, "Rừng Nhỏ",
				"top_ao_khoac_rung_nho", "Áo khoác Rừng Nhỏ", "bottom_quan_ong_rong_la_theu", "Quần ống rộng Lá Thêu"),
			accPrize("acc_khuyen_tai_giot_suong", "Khuyên tai Giọt Sương", GiftRarityRare, MascotLayerEarring, false, ""),
			accPrize("acc_tui_la_biec", "Túi Lá Biếc", GiftRarityRare, MascotLayerBag, false, ""),
			setPrize("set_la_ngoc", "Bộ Công Chúa Lá Ngọc", GiftRarityUltra, "Công Chúa Lá Ngọc",
				"top_ao_la_ngoc", "Áo Lá Ngọc", "bottom_vay_xoe_la_ngoc", "Váy xoè Lá Ngọc"),
			accPrize("acc_vuong_mien_la_ngoc", "Vương miện Lá Ngọc", GiftRarityUltra, MascotLayerHat, false, ""),
		},
	}
}

func setPrize(id, name, rarity, setName, topID, topName, bottomID, bottomName string) GiftPrize {
	return GiftPrize{
		ID: id, Name: name, Rarity: rarity, Kind: GiftKindSet,
		Items: []GiftItem{
			{ID: topID, ArtID: topID, Name: topName, Slot: MascotLayerTop, Set: setName},
			{ID: bottomID, ArtID: bottomID, Name: bottomName, Slot: MascotLayerBottom, Set: setName},
		},
	}
}

func accPrize(id, name, rarity, slot string, legacy bool, grantID string) GiftPrize {
	if grantID == "" {
		grantID = id
	}
	return GiftPrize{
		ID: id, Name: name, Rarity: rarity, Kind: GiftKindAccessory,
		Items: []GiftItem{{
			ID: grantID, ArtID: id, Name: name, Slot: slot, Legacy: legacy,
		}},
	}
}
