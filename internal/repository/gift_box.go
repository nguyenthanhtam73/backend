package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrGiftBoxNotFound = errors.New("that gift box was not found")
	ErrGiftBoxTree     = errors.New("this tree has no gift box")
	ErrGiftBoxID       = errors.New("gift box id is not valid")
)

// errGiftOpenConflict rolls the transaction back when the box was opened
// by a request that committed first. The caller reads that result.
var errGiftOpenConflict = errors.New("gift box was opened concurrently")

// giftRoll draws the prize. Tests replace it and restore the previous function.
var giftRoll = domain.CryptoGiftRoll

// SetGiftRoll replaces the box roll. The returned function is the previous one.
func SetGiftRoll(fn func() (int, error)) func() (int, error) {
	prev := giftRoll
	if fn == nil {
		giftRoll = domain.CryptoGiftRoll
	} else {
		giftRoll = fn
	}
	return prev
}

// GiftOpen is the stored result of opening one box.
type GiftOpen struct {
	Already   bool
	Box       domain.TreeGiftBox
	Prize     domain.GiftPrize
	Granted   []domain.GiftItem
	Duplicate bool
	Partial   bool
	DupeCoins int
	Balance   int64
}

// ListUnopenedGiftBoxes returns the caller's boxes that have not been opened,
// oldest first.
func (r *RoomTreeRepository) ListUnopenedGiftBoxes(ctx context.Context, userID uuid.UUID) ([]domain.TreeGiftBox, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user required")
	}
	var rows []domain.TreeGiftBox
	err = db.WithContext(ctx).
		Where("user_id = ? AND opened_at IS NULL", userID).
		Order("created_at ASC, id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []domain.TreeGiftBox{}
	}
	return rows, nil
}

// OpenGiftBox rolls one prize the first time and returns that same result
// on every later call. The box id is the idempotency key.
func (r *RoomTreeRepository) OpenGiftBox(ctx context.Context, userID, boxID uuid.UUID, now time.Time) (GiftOpen, error) {
	var zero GiftOpen
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	if userID == uuid.Nil || boxID == uuid.Nil {
		return zero, ErrGiftBoxNotFound
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var result GiftOpen
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		box, err := lockGiftBox(tx, userID, boxID)
		if err != nil {
			return err
		}
		if box.OpenedAt != nil {
			opened, err := giftOpenFromBox(tx, box, true)
			if err != nil {
				return err
			}
			result = opened
			return nil
		}
		opened, err := rollGiftBox(tx, box, now)
		if err != nil {
			return err
		}
		result = opened
		return nil
	})
	if errors.Is(err, errGiftOpenConflict) {
		return r.readOpenedGiftBox(ctx, userID, boxID)
	}
	if err != nil {
		return zero, err
	}
	return result, nil
}

func (r *RoomTreeRepository) readOpenedGiftBox(ctx context.Context, userID, boxID uuid.UUID) (GiftOpen, error) {
	var zero GiftOpen
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	var box domain.TreeGiftBox
	err = db.WithContext(ctx).Where("id = ? AND user_id = ?", boxID, userID).Take(&box).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return zero, ErrGiftBoxNotFound
	}
	if err != nil {
		return zero, err
	}
	if box.OpenedAt == nil {
		return zero, errGiftOpenConflict
	}
	return giftOpenFromBox(db.WithContext(ctx), box, true)
}

func lockGiftBox(tx *gorm.DB, userID, boxID uuid.UUID) (domain.TreeGiftBox, error) {
	q := tx
	if tx.Dialector != nil && tx.Dialector.Name() == "postgres" {
		q = tx.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var box domain.TreeGiftBox
	err := q.Where("id = ? AND user_id = ?", boxID, userID).Take(&box).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.TreeGiftBox{}, ErrGiftBoxNotFound
	}
	if err != nil {
		return domain.TreeGiftBox{}, err
	}
	return box, nil
}

func rollGiftBox(tx *gorm.DB, box domain.TreeGiftBox, now time.Time) (GiftOpen, error) {
	def, ok := domain.GiftBoxByTree(box.TreeType)
	if !ok {
		return GiftOpen{}, ErrGiftBoxTree
	}
	prizes, err := domain.WeightedPrizes(def)
	if err != nil {
		return GiftOpen{}, err
	}
	roll, err := giftRoll()
	if err != nil {
		return GiftOpen{}, err
	}
	prize, err := domain.PickGiftPrize(prizes, roll)
	if err != nil {
		return GiftOpen{}, err
	}
	owned, err := ownedGiftItems(tx, box.UserID, prize, now)
	if err != nil {
		return GiftOpen{}, err
	}
	grant := domain.ResolveGiftGrant(prize, owned)
	for _, id := range grant.GrantIDs {
		if err := grantGiftItem(tx, box.UserID, id, box.RoomCode, now); err != nil {
			return GiftOpen{}, err
		}
	}
	if grant.DupeCoins > 0 {
		key := "giftbox:" + box.ID.String()
		if _, err := creditCoins(tx, box.UserID, int64(grant.DupeCoins), key, domain.CoinReasonGiftBoxDupe, now); err != nil {
			return GiftOpen{}, err
		}
	}
	openedAt := now.UTC()
	res := tx.Model(&domain.TreeGiftBox{}).
		Where("id = ? AND opened_at IS NULL", box.ID).
		Updates(map[string]any{
			"opened_at":        openedAt,
			"prize_id":         prize.ID,
			"granted_item_ids": strings.Join(grant.GrantIDs, ","),
			"dupe_coins":       grant.DupeCoins,
		})
	if res.Error != nil {
		return GiftOpen{}, res.Error
	}
	if res.RowsAffected == 0 {
		return GiftOpen{}, errGiftOpenConflict
	}
	box.OpenedAt = &openedAt
	box.PrizeID = prize.ID
	box.GrantedItemIDs = strings.Join(grant.GrantIDs, ",")
	box.DupeCoins = grant.DupeCoins
	balance, err := coinBalance(tx, box.UserID)
	if err != nil {
		return GiftOpen{}, err
	}
	return GiftOpen{
		Box: box, Prize: prize, Granted: grantedGiftItems(prize, grant.GrantIDs),
		Duplicate: grant.Duplicate, Partial: grant.Partial, DupeCoins: grant.DupeCoins,
		Balance: balance,
	}, nil
}

func giftOpenFromBox(tx *gorm.DB, box domain.TreeGiftBox, already bool) (GiftOpen, error) {
	def, ok := domain.GiftBoxByTree(box.TreeType)
	if !ok {
		return GiftOpen{}, ErrGiftBoxTree
	}
	prizes, err := domain.WeightedPrizes(def)
	if err != nil {
		return GiftOpen{}, err
	}
	var prize domain.GiftPrize
	found := false
	for _, row := range prizes {
		if row.ID == box.PrizeID {
			prize = row
			found = true
			break
		}
	}
	if !found {
		return GiftOpen{}, fmt.Errorf("gift prize %s is not in the catalog", box.PrizeID)
	}
	ids := splitGiftIDs(box.GrantedItemIDs)
	balance, err := coinBalance(tx, box.UserID)
	if err != nil {
		return GiftOpen{}, err
	}
	itemCount := len(prize.Items)
	return GiftOpen{
		Already: already, Box: box, Prize: prize, Granted: grantedGiftItems(prize, ids),
		Duplicate: len(ids) == 0, Partial: len(ids) > 0 && len(ids) < itemCount,
		DupeCoins: box.DupeCoins, Balance: balance,
	}, nil
}

func grantedGiftItems(prize domain.GiftPrize, ids []string) []domain.GiftItem {
	want := map[string]struct{}{}
	for _, id := range ids {
		want[id] = struct{}{}
	}
	out := make([]domain.GiftItem, 0, len(ids))
	for _, item := range prize.Items {
		if _, ok := want[item.ID]; ok {
			out = append(out, item)
		}
	}
	return out
}

func splitGiftIDs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func ownedGiftItems(tx *gorm.DB, userID uuid.UUID, prize domain.GiftPrize, now time.Time) (map[string]struct{}, error) {
	ids := make([]string, len(prize.Items))
	for i, item := range prize.Items {
		ids[i] = item.ID
	}
	var rows []domain.UserMascotItem
	err := tx.Select("item_id").
		Where("user_id = ? AND item_id IN ? AND (expires_at IS NULL OR expires_at > ?)", userID, ids, now.UTC()).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		out[row.ItemID] = struct{}{}
	}
	return out, nil
}

func grantGiftItem(tx *gorm.DB, userID uuid.UUID, itemID, roomCode string, now time.Time) error {
	var item domain.MascotItem
	if err := tx.Where("id = ?", itemID).Take(&item).Error; err != nil {
		return fmt.Errorf("gift item %s is not in the catalog", itemID)
	}
	code := roomCode
	row := domain.UserMascotItem{
		UserID: userID, ItemID: item.ID, Source: domain.MascotSourceGiftBox,
		SourceRoomID: &code, AcquiredAt: now.UTC(), PreviewOnly: item.PreviewOnly,
	}
	return tx.Omit("User", "Item").Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "item_id"}},
		DoNothing: true,
	}).Create(&row).Error
}

// insertGiftBox records one unopened box. A second insert for the same
// room and member does nothing.
func insertGiftBox(tx *gorm.DB, room domain.MascotRoom, userID uuid.UUID, at time.Time) error {
	if tx == nil || userID == uuid.Nil {
		return fmt.Errorf("gift box requires a user")
	}
	if _, ok := domain.GiftBoxByTree(room.TreeType); !ok {
		return nil
	}
	row := domain.TreeGiftBox{
		ID: uuid.New(), UserID: userID, RoomCode: room.Code,
		TreeType: room.TreeType, CreatedAt: at.UTC(),
	}
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "room_code"}, {Name: "user_id"}},
		DoNothing: true,
	}).Create(&row)
	return res.Error
}

// SeedGiftBoxCatalog inserts wardrobe rows for every shipped box.
// The legacy leaf clip stays in the tree catalog. Existing rows are left alone.
func SeedGiftBoxCatalog(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	if !db.Migrator().HasTable(&domain.MascotItem{}) {
		return nil
	}
	for _, box := range domain.GiftBoxes() {
		for _, item := range domain.GiftWardrobeItems(box) {
			var n int64
			if err := db.Model(&domain.MascotItem{}).Where("id = ?", item.ID).Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			row := item
			if err := db.Create(&row).Error; err != nil && !IsUniqueConflict(err) {
				return err
			}
		}
	}
	return nil
}

const leafClipSlotKey = "leaf_clip_hair_acc_v1"

// migrateLeafClipSlot moves the old leaf clip onto the hair slot once.
// People who already own tree_clip_la_xanh keep that row. A loadout that
// wore it as an accessory moves it to hair_acc. A later boot leaves edits alone.
func migrateLeafClipSlot(db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable(&domain.MascotItem{}) || !db.Migrator().HasTable(&domain.SeedBagConfig{}) {
		return nil
	}
	if !db.Migrator().HasTable(&domain.UserMascot{}) {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		marker := domain.SeedBagConfig{Key: leafClipSlotKey, IntValue: 1}
		res := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoNothing: true,
		}).Create(&marker)
		if res.Error != nil {
			if IsUniqueConflict(res.Error) {
				return nil
			}
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		if err := tx.Model(&domain.MascotItem{}).
			Where("id = ? AND layer = ?", domain.TreeItemClipLeaf, domain.MascotLayerAccessory).
			Update("layer", domain.MascotLayerHairAcc).Error; err != nil {
			return err
		}
		// A fresh session: the item update above can leave a WHERE on this
		// transaction, and jsonb LIKE is not portable. One row per account.
		var rows []domain.UserMascot
		if err := tx.Session(&gorm.Session{}).Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			next, changed, err := moveLeafClipToHair(row.Loadout)
			if err != nil {
				return err
			}
			if !changed {
				continue
			}
			if err := tx.Session(&gorm.Session{}).Model(&domain.UserMascot{}).Where("user_id = ?", row.UserID).Update("loadout", next).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func moveLeafClipToHair(raw json.RawMessage) (json.RawMessage, bool, error) {
	if len(raw) == 0 {
		return raw, false, nil
	}
	var loadout domain.MascotLoadout
	if err := json.Unmarshal(raw, &loadout); err != nil {
		return nil, false, err
	}
	worn := loadout.Accessory == domain.TreeItemClipLeaf
	kept := make([]string, 0, len(loadout.Accessories))
	for _, id := range loadout.Accessories {
		if id == domain.TreeItemClipLeaf {
			worn = true
			continue
		}
		kept = append(kept, id)
	}
	if !worn {
		return raw, false, nil
	}
	loadout.Accessories = kept
	if loadout.Accessory == domain.TreeItemClipLeaf || !containsID(kept, loadout.Accessory) {
		if len(kept) > 0 {
			loadout.Accessory = kept[0]
		} else {
			loadout.Accessory = ""
		}
	}
	if strings.TrimSpace(loadout.HairAcc) == "" {
		loadout.HairAcc = domain.TreeItemClipLeaf
	}
	out, err := json.Marshal(loadout)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func containsID(ids []string, id string) bool {
	if id == "" {
		return true
	}
	for _, row := range ids {
		if row == id {
			return true
		}
	}
	return false
}
