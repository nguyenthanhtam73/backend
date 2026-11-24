package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ShopError is a purchase the client can show. Needed and Balance are set
// when the wallet is short. Streak and Need are set when Ngọc Trai is locked.
type ShopError struct {
	Err     error
	Code    string
	Needed  int
	Balance int64
	Streak  int
	Need    int
	Reason  string
}

func (e *ShopError) Error() string {
	if e == nil || e.Err == nil {
		return "shop"
	}
	return e.Err.Error()
}

func (e *ShopError) Unwrap() error { return e.Err }

var (
	errShopNotForSale = errors.New("that item is not sold")
	errShopOwned      = errors.New("you already own that item")
	errShopStreak     = errors.New("that set unlocks after a 30 day check-in streak")
)

// ShopReceipt is one purchase result, including a replay of the same key.
type ShopReceipt struct {
	Already  bool
	ItemID   string
	TreeType string
	Charged  int
	Balance  int64
	Granted  []domain.ShopPiece
	Bonus    []domain.ShopPiece
}

// ShopView is the catalog plus the caller's wallet and streak.
type ShopView struct {
	Balance       int64
	Streak        int
	PearlUnlocked bool
	Owned         map[string]struct{}
}

// XuShopRepository sells regular Bé Da pieces for xu.
type XuShopRepository struct {
	db *gorm.DB
}

func NewXuShopRepository(db *gorm.DB) *XuShopRepository {
	return &XuShopRepository{db: db}
}

func (r *XuShopRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Catalog is the shop for one account. A streak of 30 records the pearl unlock.
func (r *XuShopRepository) Catalog(ctx context.Context, userID uuid.UUID, now time.Time) (ShopView, error) {
	var zero ShopView
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("user required")
	}
	var view ShopView
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loaded, err := loadShopView(tx, userID, now)
		if err != nil {
			return err
		}
		view = loaded
		return nil
	})
	if err != nil {
		return zero, err
	}
	return view, nil
}

// Buy debits xu and grants wardrobe rows, or one regular seed piece, in one
// transaction. The same idempotency key returns the first result.
func (r *XuShopRepository) Buy(ctx context.Context, userID uuid.UUID, itemID, treeType, idem string, now time.Time) (ShopReceipt, error) {
	var zero ShopReceipt
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("user required")
	}
	itemID = strings.TrimSpace(itemID)
	treeType = strings.ToLower(strings.TrimSpace(treeType))
	idem = strings.TrimSpace(idem)
	if idem == "" {
		return zero, ErrIdempotencyKey
	}
	if len(idem) > 80 {
		return zero, ErrTreeBadIdempotency
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var receipt ShopReceipt
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockShopUser(tx, userID); err != nil {
			return err
		}
		var prior domain.UserShopOrder
		err := tx.Where("user_id = ? AND idem_key = ?", userID, idem).Take(&prior).Error
		if err == nil {
			if prior.ItemID != itemID || prior.TreeType != treeType {
				return ErrIdempotencyConflict
			}
			bal, err := coinBalance(tx, userID)
			if err != nil {
				return err
			}
			receipt = receiptFromOrder(prior, bal, true)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		view, err := loadShopView(tx, userID, now)
		if err != nil {
			return err
		}
		quote := domain.QuoteShop(itemID, treeType, view.Owned, view.Streak, view.PearlUnlocked)
		if quote.Code != "" {
			return shopQuoteError(quote, view.Balance)
		}
		if quote.Kind == domain.ShopGroupSeeds {
			if _, err := commonShardType(tx, quote.TreeType); err != nil {
				return &ShopError{Err: errShopNotForSale, Code: "not_for_sale", Reason: "tree_type"}
			}
		}
		charged := quote.Charge
		if charged > 0 {
			key := shopCoinKey(userID, idem)
			_, fresh, err := spendCoins(tx, userID, int64(charged), key, domain.CoinReasonShopBuy, true, now)
			if errors.Is(err, ErrNotEnoughCoins) {
				bal, _ := coinBalance(tx, userID)
				return &ShopError{Err: ErrNotEnoughCoins, Code: "not_enough_coins", Needed: charged, Balance: bal}
			}
			if err != nil {
				return err
			}
			if !fresh {
				return ErrIdempotencyConflict
			}
		}
		granted := make([]domain.ShopPiece, 0, len(quote.Grant)+2)
		for _, piece := range quote.Grant {
			if piece.Layer == "seed" {
				if _, err := addShard(tx, userID, quote.TreeType, domain.SeedSourceBuy, seedLedgerID("shop-shard", userID.String(), idem), now); err != nil {
					return err
				}
				granted = append(granted, piece)
				continue
			}
			if err := grantShopPiece(tx, userID, piece, now); err != nil {
				return err
			}
			granted = append(granted, piece)
			for _, extra := range piece.Companions {
				if err := grantShopPiece(tx, userID, domain.ShopPiece{ID: extra, Name: piece.Name, Layer: piece.Layer}, now); err != nil {
					return err
				}
			}
		}
		var bonus []domain.ShopPiece
		for _, piece := range quote.Bonus {
			if err := grantShopPiece(tx, userID, piece, now); err != nil {
				return err
			}
			bonus = append(bonus, piece)
		}
		ids := shopGrantedIDs(granted, bonus)
		order := domain.UserShopOrder{
			UserID: userID, IdemKey: idem, ItemID: itemID, TreeType: treeType,
			Charged: charged, GrantedIDs: strings.Join(ids, ","), CreatedAt: now.UTC(),
		}
		if err := tx.Omit("User").Create(&order).Error; err != nil {
			return err
		}
		bal, err := coinBalance(tx, userID)
		if err != nil {
			return err
		}
		receipt = ShopReceipt{
			ItemID: itemID, TreeType: treeType, Charged: charged, Balance: bal,
			Granted: granted, Bonus: bonus,
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	return receipt, nil
}

func loadShopView(tx *gorm.DB, userID uuid.UUID, now time.Time) (ShopView, error) {
	owned, err := shopOwned(tx, userID, now)
	if err != nil {
		return ShopView{}, err
	}
	streak := shopStreak(tx, userID, now)
	unlocked, err := shopPearlUnlocked(tx, userID)
	if err != nil {
		return ShopView{}, err
	}
	if !unlocked && streak >= domain.ShopPearlStreak {
		row := domain.UserShopUnlock{UserID: userID, UnlockKey: domain.ShopUnlockPearl, UnlockedAt: now.UTC()}
		if err := tx.Omit("User").Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "unlock_key"}},
			DoNothing: true,
		}).Create(&row).Error; err != nil && !IsUniqueConflict(err) {
			return ShopView{}, err
		}
		unlocked = true
	}
	bal, err := coinBalance(tx, userID)
	if err != nil {
		return ShopView{}, err
	}
	if owned == nil {
		owned = map[string]struct{}{}
	}
	return ShopView{Balance: bal, Streak: streak, PearlUnlocked: unlocked, Owned: owned}, nil
}

func shopOwned(tx *gorm.DB, userID uuid.UUID, now time.Time) (map[string]struct{}, error) {
	var rows []domain.UserMascotItem
	err := tx.Select("item_id").
		Where("user_id = ? AND (expires_at IS NULL OR expires_at > ?)", userID, now.UTC()).
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

func shopStreak(tx *gorm.DB, userID uuid.UUID, now time.Time) int {
	var row domain.Streak
	err := tx.Where("user_id = ?", userID).Take(&row).Error
	if err != nil {
		return 0
	}
	return dto.EvaluateStreakView(&row, now).EffectiveStreak
}

func shopPearlUnlocked(tx *gorm.DB, userID uuid.UUID) (bool, error) {
	var n int64
	err := tx.Model(&domain.UserShopUnlock{}).
		Where("user_id = ? AND unlock_key = ?", userID, domain.ShopUnlockPearl).
		Count(&n).Error
	return n > 0, err
}

func shopQuoteError(quote domain.ShopQuote, balance int64) error {
	switch quote.Code {
	case "already_owned":
		return &ShopError{Err: errShopOwned, Code: quote.Code}
	case "streak_locked":
		return &ShopError{Err: errShopStreak, Code: quote.Code, Streak: quote.Streak, Need: quote.Need}
	default:
		return &ShopError{Err: errShopNotForSale, Code: "not_for_sale", Reason: quote.TreeType, Balance: balance}
	}
}

func shopCoinKey(userID uuid.UUID, idem string) string {
	sum := sha256.Sum256([]byte("shop|" + userID.String() + "|" + idem))
	return hex.EncodeToString(sum[:])
}

func grantShopPiece(tx *gorm.DB, userID uuid.UUID, piece domain.ShopPiece, now time.Time) error {
	if piece.Layer == "seed" || piece.ID == "" {
		return nil
	}
	var n int64
	if err := tx.Model(&domain.MascotItem{}).Where("id = ?", piece.ID).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("shop item %s is not in the catalog", piece.ID)
	}
	row := domain.UserMascotItem{
		UserID: userID, ItemID: piece.ID, Source: domain.MascotSourceShop, AcquiredAt: now.UTC(),
	}
	return tx.Omit("User", "Item").Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "item_id"}},
		DoNothing: true,
	}).Create(&row).Error
}

func shopGrantedIDs(granted, bonus []domain.ShopPiece) []string {
	out := make([]string, 0, len(granted)+len(bonus))
	for _, piece := range granted {
		out = append(out, piece.ID)
		for _, extra := range piece.Companions {
			out = append(out, extra)
		}
	}
	for _, piece := range bonus {
		out = append(out, piece.ID)
	}
	return out
}

func receiptFromOrder(order domain.UserShopOrder, balance int64, already bool) ShopReceipt {
	ids := strings.Split(order.GrantedIDs, ",")
	granted := make([]domain.ShopPiece, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		granted = append(granted, domain.ShopPiece{ID: id})
	}
	return ShopReceipt{
		Already: already, ItemID: order.ItemID, TreeType: order.TreeType,
		Charged: order.Charged, Balance: balance, Granted: granted,
	}
}

func lockShopUser(tx *gorm.DB, userID uuid.UUID) error {
	if tx.Dialector == nil || tx.Dialector.Name() != "postgres" {
		return nil
	}
	return tx.Exec("SELECT id FROM users WHERE id = ? FOR UPDATE", userID).Error
}

// SeedShopCatalog inserts wardrobe rows the xu shop grants. Existing rows stay.
func SeedShopCatalog(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("nil db")
	}
	if !db.Migrator().HasTable(&domain.MascotItem{}) {
		return nil
	}
	for _, item := range domain.ShopMascotItems() {
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
	return nil
}
