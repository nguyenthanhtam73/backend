package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/lobby"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Seed-bag errors. Handlers map these to HTTP codes.
var (
	ErrNeedSeed            = errors.New("you need a seed to plant this tree")
	ErrNotEnoughCoins      = errors.New("not enough coins")
	ErrNotEnoughShards     = errors.New("not enough shards")
	ErrMysteryNotTradable  = errors.New("mystery shards cannot be traded")
	ErrMysteryNotForSale   = errors.New("mystery shards are not sold")
	ErrBadShardType        = errors.New("that shard type is not available")
	ErrBadTrade            = errors.New("trade must offer the required shards")
	ErrSameShardType       = errors.New("the shard you receive must be a different type")
	ErrDrawUnavailable     = errors.New("there is no leaf to draw")
	ErrBadPick             = errors.New("pick must be 0, 1, or 2")
	ErrIdempotencyKey      = errors.New("idempotency key is required")
	ErrIdempotencyConflict = errors.New("idempotency key was used for a different request")
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

// BagCatalogItem is one non-leaf tree in the 18-slot bag.
// Available types can be earned now. ComingSoon is a known type that is not
// in the shard or mystery pool yet (sen hồng). Blank slots are ComingSoonCount.
type BagCatalogItem struct {
	TypeKey       string
	NameVI        string
	Rarity        string
	ImageKey      string
	Shards        int
	Seeds         int
	Available     bool
	ComingSoon    bool
	InMysteryPool bool
	Revealed      bool
}

// SeedBag is GET /mascot/seed-bag and the body of a trade or buy.
type SeedBag struct {
	Shards          []BagStack
	MysteryShards   int
	Seeds           []BagStack
	MysterySeeds    int
	Rules           BagRules
	Balance         int64
	Already         bool
	Catalog         []BagCatalogItem
	SlotCount       int
	ComingSoonCount int
}

// DrawResult is POST /mascot/rooms/:code/tree/draw.
// Pick is echoed and does not choose the shard.
type DrawResult struct {
	Already bool
	Pick    *int
	Shard   ShardView
	CanDraw bool
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

// bloomLedgerID and plantLedgerID include the room's created instant so a
// reused room code cannot replay an old bloom or plant.
func bloomLedgerID(room domain.MascotRoom, userID uuid.UUID) string {
	return seedLedgerID("bloom", room.Code, fmt.Sprint(room.CreatedAt.UTC().UnixNano()), userID.String())
}

func plantLedgerID(roomCode string, created time.Time, owner uuid.UUID, consumeKey string) string {
	return seedLedgerID("plant", owner.String(), roomCode, fmt.Sprint(created.UTC().UnixNano()), consumeKey)
}

func samePick(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func tradePayload(folded map[string]int, want string) string {
	parts := make([]string, 0, len(folded))
	for key, count := range folded {
		parts = append(parts, fmt.Sprintf("%s:%d", key, count))
	}
	sort.Strings(parts)
	return want + "|" + strings.Join(parts, ",")
}

// beginIdempotent claims one key. The same payload replays. A different
// payload is a conflict. The caller must not have written yet.
func beginIdempotent(tx *gorm.DB, userID uuid.UUID, action, idem, payload string, now time.Time) error {
	sum := sha256.Sum256([]byte(payload))
	hash := hex.EncodeToString(sum[:])
	id := seedLedgerID("idem", action, userID.String(), idem)
	row := domain.UserSeedLedger{
		ID: id, UserID: userID, Kind: "idem", TypeKey: hash, Delta: 1, Source: action, CreatedAt: now.UTC(),
	}
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoNothing: true,
	}).Create(&row)
	if res.Error != nil && !IsUniqueConflict(res.Error) {
		return res.Error
	}
	if res.Error == nil && res.RowsAffected > 0 {
		return nil
	}
	var existing domain.UserSeedLedger
	if err := tx.Where("id = ?", id).Take(&existing).Error; err != nil {
		return err
	}
	if existing.TypeKey != hash {
		return ErrIdempotencyConflict
	}
	return errBagReplay
}

// revealPendingBlooms grants a shard for every leaf that was never flipped,
// then the caller may delete the draw rows. Each grant uses bloomLedgerID,
// so a second pass does not pay again.
func revealPendingBlooms(tx *gorm.DB, codes []string) error {
	if tx == nil || len(codes) == 0 || !tx.Migrator().HasTable(&domain.RoomBloomShard{}) {
		return nil
	}
	var rooms []domain.MascotRoom
	if err := tx.Where("code IN ?", codes).Find(&rooms).Error; err != nil {
		return err
	}
	byCode := make(map[string]domain.MascotRoom, len(rooms))
	for _, room := range rooms {
		byCode[room.Code] = room
	}
	var rows []domain.RoomBloomShard
	if err := tx.Where("room_code IN ? AND type_key = ?", codes, "").Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().UTC()
	bps, err := seedConfig(tx, domain.SeedConfigMysteryBps)
	if err != nil {
		return err
	}
	commons, err := commonShardKeys(tx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		room, ok := byCode[row.RoomCode]
		if !ok {
			continue
		}
		key := rollShardType(seedNextInt, bps, commons)
		res := tx.Model(&domain.RoomBloomShard{}).
			Where("room_code = ? AND user_id = ? AND type_key = ?", row.RoomCode, row.UserID, "").
			Updates(map[string]any{"type_key": key, "claimed_at": now, "idempotency_key": "cleanup"})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			continue
		}
		if _, err := addShard(tx, row.UserID, key, domain.SeedSourceBloom, bloomLedgerID(room, row.UserID), now); err != nil {
			return err
		}
	}
	return nil
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
		return ErrIdempotencyKey
	}
	if len(idem) > 80 {
		return ErrTreeBadIdempotency
	}
	if err := beginIdempotent(tx, userID, domain.SeedSourceBuy, idem, key, now); err != nil {
		return err
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
		return ErrIdempotencyKey
	}
	if len(idem) > 80 {
		return ErrTreeBadIdempotency
	}
	inID := seedLedgerID("trade-in", userID.String(), idem)
	need, err := seedConfig(tx, domain.SeedConfigTradeCount)
	if err != nil {
		return err
	}
	folded, total, err := foldOffer(tx, offer)
	if err != nil {
		return err
	}
	if len(folded) != 1 || total != need || need <= 0 {
		return tradeOfferError{need: need}
	}
	offerKey := ""
	for key := range folded {
		offerKey = key
	}
	if offerKey == want {
		return ErrSameShardType
	}
	if _, err := commonShardType(tx, want); err != nil {
		return err
	}
	if err := beginIdempotent(tx, userID, domain.SeedSourceTradeIn, idem, tradePayload(folded, want), now); err != nil {
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

// tradeOfferError carries the configured shard count into the user-facing text.
type tradeOfferError struct{ need int }

func (e tradeOfferError) Error() string {
	return fmt.Sprintf("trade must offer %d shards of one common type", e.need)
}

func (e tradeOfferError) Is(target error) bool { return target == ErrBadTrade }

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

// grantBloomShards records who may draw. The shard type stays hidden until
// POST draw, so the client can flip a leaf. The type is not added to the bag yet.
func grantBloomShards(tx *gorm.DB, room domain.MascotRoom, members []uuid.UUID, at time.Time) error {
	if tx == nil || !tx.Migrator().HasTable(&domain.RoomBloomShard{}) {
		return nil
	}
	for _, userID := range members {
		if userID == uuid.Nil {
			continue
		}
		row := domain.RoomBloomShard{
			RoomCode: room.Code, UserID: userID, TypeKey: "", CreatedAt: at.UTC(),
		}
		res := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "room_code"}, {Name: "user_id"}},
			DoNothing: true,
		}).Create(&row)
		if res.Error != nil {
			return res.Error
		}
	}
	return nil
}

// DrawShard reveals one bloom shard. pick is 0, 1, or 2 and does not change
// the roll. A second call returns the same shard and does not grant again.
func (r *RoomTreeRepository) DrawShard(ctx context.Context, userID uuid.UUID, rawCode, idem string, pick *int, now time.Time) (DrawResult, error) {
	var zero DrawResult
	if pick != nil && (*pick < 0 || *pick > 2) {
		return zero, ErrBadPick
	}
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	code, ok := lobby.NormalizeRoomCode(rawCode)
	if !ok {
		return zero, ErrMascotRoomBadCode
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("user required")
	}
	idem = strings.TrimSpace(idem)
	var result DrawResult
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		room, err := lockMascotRoom(tx, code)
		if err != nil {
			return err
		}
		var row domain.RoomBloomShard
		err = tx.Where("room_code = ? AND user_id = ?", code, userID).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			member, real, memErr := callerMember(tx, code, userID)
			if memErr != nil {
				return memErr
			}
			if member && !real {
				return ErrTreeNotReal
			}
			return ErrDrawUnavailable
		}
		if err != nil {
			return err
		}
		// The row was written for a real member at bloom. They may have left
		// the room since then and can still flip that leaf.
		if row.TypeKey != "" {
			if idem != "" && row.IdempotencyKey == idem && !samePick(row.Pick, pick) {
				return ErrIdempotencyConflict
			}
			view, err := shardView(tx, row.TypeKey)
			if err != nil {
				return err
			}
			result = DrawResult{Already: true, Pick: row.Pick, Shard: view}
			return nil
		}
		bps, err := seedConfig(tx, domain.SeedConfigMysteryBps)
		if err != nil {
			return err
		}
		commons, err := commonShardKeys(tx)
		if err != nil {
			return err
		}
		key := rollShardType(seedNextInt, bps, commons)
		claimed := now.UTC()
		res := tx.Model(&domain.RoomBloomShard{}).
			Where("room_code = ? AND user_id = ? AND type_key = ?", code, userID, "").
			Updates(map[string]any{
				"type_key":        key,
				"pick":            pick,
				"idempotency_key": idem,
				"claimed_at":      claimed,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Where("room_code = ? AND user_id = ?", code, userID).Take(&row).Error; err != nil {
				return err
			}
			if idem != "" && row.IdempotencyKey == idem && !samePick(row.Pick, pick) {
				return ErrIdempotencyConflict
			}
			view, err := shardView(tx, row.TypeKey)
			if err != nil {
				return err
			}
			result = DrawResult{Already: true, Pick: row.Pick, Shard: view}
			return nil
		}
		if _, err := addShard(tx, userID, key, domain.SeedSourceBloom, bloomLedgerID(room, userID), now); err != nil {
			return err
		}
		view, err := shardView(tx, key)
		if err != nil {
			return err
		}
		result = DrawResult{Pick: pick, Shard: view}
		return nil
	})
	if err != nil {
		return zero, err
	}
	return result, nil
}

// BloomBoard is who may draw and what they already revealed. Names only.
type BloomDraw struct {
	Name    string
	Pending bool
	Shard   *ShardView
}

func bloomBoard(tx *gorm.DB, code string, viewer uuid.UUID) (canDraw bool, mine *ShardView, draws []BloomDraw, err error) {
	draws = []BloomDraw{}
	if tx == nil || !tx.Migrator().HasTable(&domain.RoomBloomShard{}) {
		return false, nil, draws, nil
	}
	var rows []domain.RoomBloomShard
	if err := tx.Where("room_code = ?", code).Order("created_at ASC, user_id ASC").Find(&rows).Error; err != nil {
		return false, nil, nil, err
	}
	if len(rows) == 0 {
		return false, nil, draws, nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.UserID
	}
	names, err := waterNames(tx, code, watersFor(ids))
	if err != nil {
		return false, nil, nil, err
	}
	for i, row := range rows {
		name := "Bạn"
		if i < len(names) {
			name = names[i]
		}
		if row.TypeKey == "" {
			draws = append(draws, BloomDraw{Name: name, Pending: true})
			if row.UserID == viewer {
				canDraw = true
			}
			continue
		}
		view, err := shardView(tx, row.TypeKey)
		if err != nil {
			return false, nil, nil, err
		}
		copyView := view
		draws = append(draws, BloomDraw{Name: name, Shard: &copyView})
		if row.UserID == viewer {
			mine = &copyView
		}
	}
	return canDraw, mine, draws, nil
}

func watersFor(ids []uuid.UUID) []domain.MascotRoomTreeWater {
	out := make([]domain.MascotRoomTreeWater, len(ids))
	for i, id := range ids {
		out[i] = domain.MascotRoomTreeWater{UserID: id}
	}
	return out
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

func consumePlant(tx *gorm.DB, owner uuid.UUID, plan plantPlan, roomCode string, created, now time.Time) error {
	if plan.consumeKey == "" {
		return nil
	}
	id := plantLedgerID(roomCode, created, owner, plan.consumeKey)
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
	err := tx.Where("mystery_pool = ? AND plantable = ?", true, true).
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
	bag.Catalog = []BagCatalogItem{}
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
	catalog, slotCount, coming, err := bagCatalog(tx, userID, rows)
	if err != nil {
		return bag, err
	}
	bag.Catalog = catalog
	bag.SlotCount = slotCount
	bag.ComingSoonCount = coming
	return bag, nil
}

func bagCatalog(tx *gorm.DB, userID uuid.UUID, balances []domain.UserSeedBalance) ([]BagCatalogItem, int, int, error) {
	slotCount := 18
	if tx.Migrator().HasTable(&domain.SeedBagConfig{}) {
		if n, err := seedConfig(tx, domain.SeedConfigSlotCount); err == nil && n > 0 {
			slotCount = n
		}
	}
	if !tx.Migrator().HasTable(&domain.TreeType{}) {
		return []BagCatalogItem{}, slotCount, slotCount, nil
	}
	var types []domain.TreeType
	if err := tx.Where("key <> ?", domain.TreeTypeLeaf).Order("sort_order ASC, key ASC").Find(&types).Error; err != nil {
		return nil, 0, 0, err
	}
	revealed := map[string]struct{}{}
	if userID != uuid.Nil && tx.Migrator().HasTable(&domain.UserTreeReveal{}) {
		var rows []domain.UserTreeReveal
		if err := tx.Where("user_id = ?", userID).Find(&rows).Error; err != nil {
			return nil, 0, 0, err
		}
		for _, row := range rows {
			revealed[row.TypeKey] = struct{}{}
		}
	}
	shards := map[string]int{}
	seeds := map[string]int{}
	for _, row := range balances {
		if row.Kind == domain.SeedKindShard {
			shards[row.TypeKey] = row.Count
		}
		if row.Kind == domain.SeedKindSeed {
			seeds[row.TypeKey] = row.Count
		}
	}
	out := make([]BagCatalogItem, 0, len(types))
	for _, typ := range types {
		available := typ.MysteryPool || (typ.Rarity == domain.TreeRarityCommon && typ.Enabled && typ.Plantable)
		_, seen := revealed[typ.Key]
		out = append(out, BagCatalogItem{
			TypeKey: typ.Key, NameVI: typ.NameVI, Rarity: typ.Rarity, ImageKey: typ.ImageKey,
			Shards: shards[typ.Key], Seeds: seeds[typ.Key],
			Available: available, ComingSoon: !available, InMysteryPool: typ.MysteryPool,
			Revealed: typ.Rarity != domain.TreeRarityRare || seen,
		})
	}
	coming := slotCount - len(out)
	if coming < 0 {
		coming = 0
	}
	return out, slotCount, coming, nil
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

// SeedSeedBag inserts missing seed_bag_config rows. It does not rewrite
// plantable or mystery_pool. applyCatalogFlagsOnce does that a single time.
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
	return nil
}

// catalogFlagsKey marks that migration 042's plantable and mystery_pool
// values have been applied. Production boots through AutoMigrate, which
// adds those columns as false and does not run the SQL file.
const catalogFlagsKey = "catalog_flags_v1"

// applyCatalogFlagsOnce sets the shipped plantable and mystery_pool values
// a single time. A later boot leaves a hand edit alone.
func applyCatalogFlagsOnce(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable(&domain.TreeType{}) || !db.Migrator().HasTable(&domain.SeedBagConfig{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&domain.TreeType{}, "plantable") || !db.Migrator().HasColumn(&domain.TreeType{}, "mystery_pool") {
		return nil
	}
	var n int64
	if err := db.Model(&domain.SeedBagConfig{}).Where("key = ?", catalogFlagsKey).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	plantable := []string{"leaf", "cherry", "aloe", "sunflower", "dandelion", "quynh", "sen_hong"}
	if err := db.Model(&domain.TreeType{}).Where("key IN ?", plantable).Update("plantable", true).Error; err != nil {
		return err
	}
	if err := db.Model(&domain.TreeType{}).Where("key IN ?", []string{"dandelion", "quynh"}).Update("mystery_pool", true).Error; err != nil {
		return err
	}
	marker := domain.SeedBagConfig{Key: catalogFlagsKey, IntValue: 1}
	if err := db.Create(&marker).Error; err != nil && !IsUniqueConflict(err) {
		return err
	}
	return nil
}
