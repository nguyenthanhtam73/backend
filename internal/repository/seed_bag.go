package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Seed-bag errors. Handlers map these to HTTP codes.
var (
	ErrNeedSeed           = errors.New("you need a seed to plant this tree")
	ErrNotEnoughCoins     = errors.New("not enough coins")
	ErrNotEnoughShards    = errors.New("not enough shards")
	ErrMysteryNotTradable = errors.New("mystery shards cannot be traded")
	ErrMysteryNotForSale  = errors.New("mystery shards are not sold")
	ErrBadShardType       = errors.New("that shard type is not available")
	ErrBadTrade           = errors.New("trade must offer the required shards")
)

// ShardView is one shard granted to a person.
type ShardView struct {
	TypeKey string
	NameVI  string
	Mystery bool
}

// BagStack is one counted type in the seed bag.
type BagStack struct {
	TypeKey string
	NameVI  string
	Count   int
}

// BagRules are the numbers the client should show. They come from seed_bag_config.
type BagRules struct {
	MergeCount      int
	TradeInCount    int
	ShardBuyPrice   int
	MysteryShardBps int
}

// SeedBag is GET /mascot/seed-bag and the body of a trade or buy.
type SeedBag struct {
	Shards        []BagStack
	MysteryShards int
	Seeds         []BagStack
	MysterySeeds  int
	Rules         BagRules
	Balance       int64
	Already       bool
}

// seedNextInt is the roll used for mystery shards and mystery seeds.
// Tests replace it and restore the previous function.
var seedNextInt = rand.Intn

// SetSeedNextInt replaces the shard roll. The returned function restores it.
func SetSeedNextInt(fn func(int) int) func(int) int {
	prev := seedNextInt
	if fn == nil {
		seedNextInt = rand.Intn
	} else {
		seedNextInt = fn
	}
	return prev
}

// rollShardType picks mystery or one common type.
// next(10000) below bps is mystery. Otherwise next(len(commons)) picks the type.
func rollShardType(next func(int) int, bps int, commons []string) string {
	if next == nil {
		next = rand.Intn
	}
	if bps < 0 {
		bps = 0
	}
	if bps > 10000 {
		bps = 10000
	}
	if len(commons) == 0 || next(10000) < bps {
		return domain.SeedTypeMystery
	}
	return commons[next(len(commons))]
}

func seedLedgerID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

// SeedBag loads the caller's shards, seeds, rules, and xu balance.
func (r *RoomTreeRepository) SeedBag(ctx context.Context, userID uuid.UUID) (SeedBag, error) {
	var zero SeedBag
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	var bag SeedBag
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loaded, err := loadSeedBag(tx, userID)
		if err != nil {
			return err
		}
		bag = loaded
		return nil
	})
	if err != nil {
		return zero, err
	}
	return bag, nil
}

// BuyShard spends xu for one common shard. The price is seed_bag_config.shard_buy_price.
// The same idempotency key returns already and does not charge again.
func (r *RoomTreeRepository) BuyShard(ctx context.Context, userID uuid.UUID, typeKey, idem string, now time.Time) (SeedBag, error) {
	return r.changeBag(ctx, userID, now, func(tx *gorm.DB) error {
		return buyShard(tx, userID, typeKey, idem, now)
	})
}

// TradeShards turns trade_in_count common shards, of one or more types, into
// one common shard of want. Mystery cannot be offered or received.
func (r *RoomTreeRepository) TradeShards(ctx context.Context, userID uuid.UUID, offer []BagStack, want, idem string, now time.Time) (SeedBag, error) {
	return r.changeBag(ctx, userID, now, func(tx *gorm.DB) error {
		return tradeShards(tx, userID, offer, want, idem, now)
	})
}

func (r *RoomTreeRepository) changeBag(ctx context.Context, userID uuid.UUID, now time.Time, fn func(*gorm.DB) error) (SeedBag, error) {
	var zero SeedBag
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("user required")
	}
	var bag SeedBag
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := fn(tx)
		if errors.Is(err, errBagReplay) {
			loaded, loadErr := loadSeedBag(tx, userID)
			if loadErr != nil {
				return loadErr
			}
			loaded.Already = true
			bag = loaded
			return nil
		}
		if err != nil {
			return err
		}
		loaded, err := loadSeedBag(tx, userID)
		if err != nil {
			return err
		}
		bag = loaded
		return nil
	})
	if err != nil {
		return zero, err
	}
	return bag, nil
}

func buyShard(tx *gorm.DB, userID uuid.UUID, typeKey, idem string, now time.Time) error {
	key := strings.ToLower(strings.TrimSpace(typeKey))
	if key == domain.SeedTypeMystery {
		return ErrMysteryNotForSale
	}
	if _, err := commonShardType(tx, key); err != nil {
		return err
	}
	idem = strings.TrimSpace(idem)
	if idem == "" {
		idem = uuid.NewString()
	}
	coinID := seedLedgerID("buy-coin", userID.String(), idem)
	price, err := seedConfig(tx, domain.SeedConfigBuyPrice)
	if err != nil {
		return err
	}
	if price <= 0 {
		return fmt.Errorf("shard price is not configured")
	}
	_, fresh, err := debitCoins(tx, userID, int64(price), coinID, domain.CoinReasonShardBuy, now)
	if err != nil {
		return err
	}
	if !fresh {
		return errBagReplay
	}
	_, err = addShard(tx, userID, key, domain.SeedSourceBuy, seedLedgerID("buy-shard", userID.String(), idem), now)
	return err
}

func tradeShards(tx *gorm.DB, userID uuid.UUID, offer []BagStack, want, idem string, now time.Time) error {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == domain.SeedTypeMystery || offerHasMystery(offer) {
		return ErrMysteryNotTradable
	}
	idem = strings.TrimSpace(idem)
	if idem == "" {
		idem = uuid.NewString()
	}
	inID := seedLedgerID("trade-in", userID.String(), idem)
	var existing int64
	if err := tx.Model(&domain.UserSeedLedger{}).Where("id = ?", inID).Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return errBagReplay
	}
	need, err := seedConfig(tx, domain.SeedConfigTradeCount)
	if err != nil {
		return err
	}
	folded, total, err := foldOffer(tx, offer)
	if err != nil {
		return err
	}
	if total != need || need <= 0 {
		return ErrBadTrade
	}
	if _, err := commonShardType(tx, want); err != nil {
		return err
	}
	for key, count := range folded {
		ok, err := appendLedger(tx, seedLedgerID("trade-out", userID.String(), idem, key), userID, domain.SeedKindShard, key, -count, domain.SeedSourceTradeOut, now)
		if err != nil {
			return err
		}
		if !ok {
			return errBagReplay
		}
		if err := adjustBalance(tx, userID, domain.SeedKindShard, key, -count); err != nil {
			return err
		}
		if err := mergeShards(tx, userID, key, seedLedgerID("trade-merge", userID.String(), idem, key), now); err != nil {
			return err
		}
	}
	_, err = addShard(tx, userID, want, domain.SeedSourceTradeIn, inID, now)
	return err
}

func offerHasMystery(offer []BagStack) bool {
	for _, row := range offer {
		if strings.EqualFold(strings.TrimSpace(row.TypeKey), domain.SeedTypeMystery) {
			return true
		}
	}
	return false
}

func foldOffer(tx *gorm.DB, offer []BagStack) (map[string]int, int, error) {
	out := map[string]int{}
	total := 0
	for _, row := range offer {
		key := strings.ToLower(strings.TrimSpace(row.TypeKey))
		if key == "" || row.Count <= 0 {
			return nil, 0, ErrBadTrade
		}
		if _, dup := out[key]; dup {
			return nil, 0, ErrBadTrade
		}
		if _, err := commonShardType(tx, key); err != nil {
			return nil, 0, err
		}
		out[key] = row.Count
		total += row.Count
	}
	if len(out) == 0 {
		return nil, 0, ErrBadTrade
	}
	return out, total, nil
}

// errBagReplay tells changeBag to reload the bag and mark already.
var errBagReplay = errors.New("seed bag replay")

// Wrap changeBag so replay is success. Redefine the method body by replacing changeBag's error handling.
// The function above is unused until changeBag checks it. See changeBag.

func addShard(tx *gorm.DB, userID uuid.UUID, typeKey, source, ledgerID string, now time.Time) (bool, error) {
	fresh, err := appendLedger(tx, ledgerID, userID, domain.SeedKindShard, typeKey, 1, source, now)
	if err != nil || !fresh {
		return fresh, err
	}
	if err := adjustBalance(tx, userID, domain.SeedKindShard, typeKey, 1); err != nil {
		return false, err
	}
	if err := mergeShards(tx, userID, typeKey, ledgerID, now); err != nil {
		return false, err
	}
	return true, nil
}

func mergeShards(tx *gorm.DB, userID uuid.UUID, typeKey, trigger string, now time.Time) error {
	n, err := seedConfig(tx, domain.SeedConfigMergeCount)
	if err != nil || n <= 0 {
		return err
	}
	for step := 1; step < 20; step++ {
		res := tx.Model(&domain.UserSeedBalance{}).
			Where("user_id = ? AND kind = ? AND type_key = ? AND count >= ?", userID, domain.SeedKindShard, typeKey, n).
			Update("count", gorm.Expr("count - ?", n))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		if _, err := appendLedger(tx, seedLedgerID(trigger, "merge-shard", fmt.Sprint(step)), userID, domain.SeedKindShard, typeKey, -n, domain.SeedSourceMerge, now); err != nil {
			return err
		}
		seedID := seedLedgerID(trigger, "merge-seed", fmt.Sprint(step))
		if _, err := appendLedger(tx, seedID, userID, domain.SeedKindSeed, typeKey, 1, domain.SeedSourceMerge, now); err != nil {
			return err
		}
		if err := adjustBalance(tx, userID, domain.SeedKindSeed, typeKey, 1); err != nil {
			return err
		}
	}
	return nil
}

func appendLedger(tx *gorm.DB, id string, userID uuid.UUID, kind, typeKey string, delta int, source string, now time.Time) (bool, error) {
	if delta == 0 || id == "" {
		return false, fmt.Errorf("seed ledger requires a change")
	}
	row := domain.UserSeedLedger{
		ID: id, UserID: userID, Kind: kind, TypeKey: typeKey,
		Delta: delta, Source: source, CreatedAt: now.UTC(),
	}
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoNothing: true,
	}).Create(&row)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func adjustBalance(tx *gorm.DB, userID uuid.UUID, kind, typeKey string, delta int) error {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		row := domain.UserSeedBalance{UserID: userID, Kind: kind, TypeKey: typeKey, Count: delta}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "kind"}, {Name: "type_key"}},
			DoUpdates: clause.Assignments(map[string]any{
				"count": gorm.Expr("user_seed_balances.count + ?", delta),
			}),
		}).Create(&row).Error
	}
	need := -delta
	res := tx.Model(&domain.UserSeedBalance{}).
		Where("user_id = ? AND kind = ? AND type_key = ? AND count >= ?", userID, kind, typeKey, need).
		Update("count", gorm.Expr("count - ?", need))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		if kind == domain.SeedKindSeed {
			return ErrNeedSeed
		}
		return ErrNotEnoughShards
	}
	return nil
}

func grantBloomShards(tx *gorm.DB, room domain.MascotRoom, members []uuid.UUID, at time.Time) (map[uuid.UUID]ShardView, error) {
	if tx == nil || !tx.Migrator().HasTable(&domain.RoomBloomShard{}) {
		return nil, nil
	}
	bps, err := seedConfig(tx, domain.SeedConfigMysteryBps)
	if err != nil {
		return nil, err
	}
	commons, err := commonShardKeys(tx)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]ShardView{}
	for _, userID := range members {
		if userID == uuid.Nil {
			continue
		}
		key := rollShardType(seedNextInt, bps, commons)
		row := domain.RoomBloomShard{
			RoomCode: room.Code, UserID: userID, TypeKey: key, CreatedAt: at.UTC(),
		}
		res := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "room_code"}, {Name: "user_id"}},
			DoNothing: true,
		}).Create(&row)
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected == 0 {
			continue
		}
		if _, err := addShard(tx, userID, key, domain.SeedSourceBloom, seedLedgerID("bloom", room.Code, userID.String()), at); err != nil {
			return nil, err
		}
		view, err := shardView(tx, key)
		if err != nil {
			return nil, err
		}
		out[userID] = view
	}
	return out, nil
}

type plantPlan struct {
	treeType   string
	consumeKey string
	revealed   bool
	name       string
	image      string
}

func preparePlant(tx *gorm.DB, owner uuid.UUID, requested string) (plantPlan, error) {
	key := strings.ToLower(strings.TrimSpace(requested))
	if key == "" || key == domain.TreeTypeLeaf {
		return plantPlan{treeType: domain.TreeTypeLeaf, name: "Cây lá xanh", image: domain.TreeTypeLeaf}, nil
	}
	if !tx.Migrator().HasTable(&domain.UserSeedBalance{}) {
		return plantPlan{}, ErrTreeTypeUnavailable
	}
	if key == domain.SeedTypeMystery {
		n, err := balanceCount(tx, owner, domain.SeedKindSeed, domain.SeedTypeMystery)
		if err != nil {
			return plantPlan{}, err
		}
		if n < 1 {
			return plantPlan{}, ErrNeedSeed
		}
		rare, err := rollRare(tx)
		if err != nil {
			return plantPlan{}, err
		}
		return plantPlan{
			treeType: rare.Key, consumeKey: domain.SeedTypeMystery, revealed: true,
			name: rare.NameVI, image: rare.ImageKey,
		}, nil
	}
	var typ domain.TreeType
	err := tx.Where("key = ?", key).Take(&typ).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return plantPlan{}, ErrTreeTypeUnknown
	}
	if err != nil {
		return plantPlan{}, err
	}
	if typ.Rarity != domain.TreeRarityCommon || !typ.Enabled || !typ.Plantable || typ.Key == domain.TreeTypeLeaf {
		return plantPlan{}, ErrTreeTypeUnavailable
	}
	n, err := balanceCount(tx, owner, domain.SeedKindSeed, typ.Key)
	if err != nil {
		return plantPlan{}, err
	}
	if n < 1 {
		return plantPlan{}, ErrNeedSeed
	}
	return plantPlan{treeType: typ.Key, consumeKey: typ.Key, name: typ.NameVI, image: typ.ImageKey}, nil
}

func consumePlant(tx *gorm.DB, owner uuid.UUID, plan plantPlan, roomCode string, now time.Time) error {
	if plan.consumeKey == "" {
		return nil
	}
	id := seedLedgerID("plant", owner.String(), roomCode, plan.consumeKey)
	fresh, err := appendLedger(tx, id, owner, domain.SeedKindSeed, plan.consumeKey, -1, domain.SeedSourcePlant, now)
	if err != nil {
		return err
	}
	if !fresh {
		return ErrNeedSeed
	}
	if err := adjustBalance(tx, owner, domain.SeedKindSeed, plan.consumeKey, -1); err != nil {
		return err
	}
	if plan.revealed {
		row := domain.UserTreeReveal{UserID: owner, TypeKey: plan.treeType, CreatedAt: now.UTC()}
		res := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "type_key"}},
			DoNothing: true,
		}).Create(&row)
		if res.Error != nil {
			return res.Error
		}
	}
	return nil
}

func rollRare(tx *gorm.DB) (domain.TreeType, error) {
	var rows []domain.TreeType
	err := tx.Where("rarity = ? AND plantable = ?", domain.TreeRarityRare, true).
		Order("sort_order ASC, key ASC").Find(&rows).Error
	if err != nil {
		return domain.TreeType{}, err
	}
	if len(rows) == 0 {
		return domain.TreeType{}, ErrTreeTypeUnavailable
	}
	return rows[seedNextInt(len(rows))], nil
}

func commonShardType(tx *gorm.DB, key string) (domain.TreeType, error) {
	var typ domain.TreeType
	err := tx.Where("key = ? AND rarity = ? AND enabled = ? AND plantable = ? AND key <> ?",
		key, domain.TreeRarityCommon, true, true, domain.TreeTypeLeaf).Take(&typ).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.TreeType{}, ErrBadShardType
	}
	if err != nil {
		return domain.TreeType{}, err
	}
	return typ, nil
}

func commonShardKeys(tx *gorm.DB) ([]string, error) {
	var rows []domain.TreeType
	err := tx.Where("rarity = ? AND enabled = ? AND plantable = ? AND key <> ?",
		domain.TreeRarityCommon, true, true, domain.TreeTypeLeaf).
		Order("sort_order ASC, key ASC").Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Key)
	}
	return out, nil
}

func shardView(tx *gorm.DB, key string) (ShardView, error) {
	if key == domain.SeedTypeMystery {
		return ShardView{TypeKey: key, NameVI: domain.MysteryShardName, Mystery: true}, nil
	}
	var typ domain.TreeType
	if err := tx.Where("key = ?", key).Take(&typ).Error; err != nil {
		return ShardView{}, err
	}
	return ShardView{TypeKey: typ.Key, NameVI: typ.NameVI}, nil
}

func balanceCount(tx *gorm.DB, userID uuid.UUID, kind, typeKey string) (int, error) {
	var row domain.UserSeedBalance
	err := tx.Where("user_id = ? AND kind = ? AND type_key = ?", userID, kind, typeKey).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return row.Count, nil
}

func seedConfig(tx *gorm.DB, key string) (int, error) {
	var row domain.SeedBagConfig
	err := tx.Where("key = ?", key).Take(&row).Error
	if err != nil {
		return 0, err
	}
	return row.IntValue, nil
}

func loadSeedBag(tx *gorm.DB, userID uuid.UUID) (SeedBag, error) {
	var bag SeedBag
	rules, err := loadRules(tx)
	if err != nil {
		return bag, err
	}
	bag.Rules = rules
	bag.Shards = []BagStack{}
	bag.Seeds = []BagStack{}
	if userID == uuid.Nil {
		return bag, nil
	}
	bal, err := coinBalance(tx, userID)
	if err != nil {
		return bag, err
	}
	bag.Balance = bal
	if !tx.Migrator().HasTable(&domain.UserSeedBalance{}) {
		return bag, nil
	}
	var rows []domain.UserSeedBalance
	if err := tx.Where("user_id = ? AND count > 0", userID).Order("kind ASC, type_key ASC").Find(&rows).Error; err != nil {
		return bag, err
	}
	names, err := treeNames(tx)
	if err != nil {
		return bag, err
	}
	for _, row := range rows {
		if row.TypeKey == domain.SeedTypeMystery {
			if row.Kind == domain.SeedKindShard {
				bag.MysteryShards = row.Count
			}
			if row.Kind == domain.SeedKindSeed {
				bag.MysterySeeds = row.Count
			}
			continue
		}
		stack := BagStack{TypeKey: row.TypeKey, NameVI: names[row.TypeKey], Count: row.Count}
		if row.Kind == domain.SeedKindShard {
			bag.Shards = append(bag.Shards, stack)
		}
		if row.Kind == domain.SeedKindSeed {
			bag.Seeds = append(bag.Seeds, stack)
		}
	}
	return bag, nil
}

func loadRules(tx *gorm.DB) (BagRules, error) {
	var rules BagRules
	if !tx.Migrator().HasTable(&domain.SeedBagConfig{}) {
		return rules, nil
	}
	var rows []domain.SeedBagConfig
	if err := tx.Find(&rows).Error; err != nil {
		return rules, err
	}
	for _, row := range rows {
		switch row.Key {
		case domain.SeedConfigMergeCount:
			rules.MergeCount = row.IntValue
		case domain.SeedConfigTradeCount:
			rules.TradeInCount = row.IntValue
		case domain.SeedConfigBuyPrice:
			rules.ShardBuyPrice = row.IntValue
		case domain.SeedConfigMysteryBps:
			rules.MysteryShardBps = row.IntValue
		}
	}
	return rules, nil
}

func treeNames(tx *gorm.DB) (map[string]string, error) {
	out := map[string]string{}
	if !tx.Migrator().HasTable(&domain.TreeType{}) {
		return out, nil
	}
	var rows []domain.TreeType
	if err := tx.Select("key", "name_vi").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.Key] = row.NameVI
	}
	return out, nil
}

// ListForUser is the catalog for one account.
// Enabled types are public. A revealed rare is added for that account only.
// Creatable is leaf, or any type this account holds a seed for.
func (r *RoomTreeRepository) ListForUser(ctx context.Context, userID uuid.UUID) ([]TreeCatalogItem, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	var out []TreeCatalogItem
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []domain.TreeType
		if err := tx.Where("enabled = ?", true).Order("sort_order ASC, key ASC").Find(&rows).Error; err != nil {
			return err
		}
		seen := map[string]struct{}{}
		for _, row := range rows {
			seen[row.Key] = struct{}{}
		}
		if userID != uuid.Nil && tx.Migrator().HasTable(&domain.UserTreeReveal{}) {
			var reveals []domain.UserTreeReveal
			if err := tx.Where("user_id = ?", userID).Find(&reveals).Error; err != nil {
				return err
			}
			if len(reveals) > 0 {
				keys := make([]string, len(reveals))
				for i, row := range reveals {
					keys[i] = row.TypeKey
				}
				var extra []domain.TreeType
				if err := tx.Where("key IN ?", keys).Order("sort_order ASC, key ASC").Find(&extra).Error; err != nil {
					return err
				}
				for _, row := range extra {
					if _, ok := seen[row.Key]; ok {
						continue
					}
					rows = append(rows, row)
					seen[row.Key] = struct{}{}
				}
			}
		}
		seeds := map[string]int{}
		if userID != uuid.Nil {
			var bals []domain.UserSeedBalance
			if tx.Migrator().HasTable(&domain.UserSeedBalance{}) {
				if err := tx.Where("user_id = ? AND kind = ?", userID, domain.SeedKindSeed).Find(&bals).Error; err != nil {
					return err
				}
			}
			for _, row := range bals {
				seeds[row.TypeKey] = row.Count
			}
		}
		names, err := itemNames(tx, exclusiveKeys(rows))
		if err != nil {
			return err
		}
		out = make([]TreeCatalogItem, len(rows))
		for i, row := range rows {
			out[i] = TreeCatalogItem{
				Key: row.Key, NameVI: row.NameVI, Rarity: row.Rarity, ImageKey: row.ImageKey,
				SortOrder: row.SortOrder, ExclusiveItemKey: row.ExclusiveItemKey,
				ExclusiveItemName: names[row.ExclusiveItemKey], Enabled: row.Enabled,
				Creatable: row.Key == domain.TreeTypeLeaf || seeds[row.Key] > 0,
				Revealed:  !row.Enabled,
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SeedSeedBag inserts missing rules and marks the shipped tree types plantable.
func SeedSeedBag(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	if !db.Migrator().HasTable(&domain.SeedBagConfig{}) {
		return nil
	}
	for _, row := range domain.SeedBagDefaults() {
		var n int64
		if err := db.Model(&domain.SeedBagConfig{}).Where("key = ?", row.Key).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		item := row
		if err := db.Create(&item).Error; err != nil && !IsUniqueConflict(err) {
			return err
		}
	}
	if db.Migrator().HasColumn(&domain.TreeType{}, "plantable") {
		for _, row := range domain.TreeTypeSeed() {
			if err := db.Model(&domain.TreeType{}).Where("key = ?", row.Key).Update("plantable", row.Plantable).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
