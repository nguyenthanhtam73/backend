package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func openShopDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openTreeDB(t)
	if err := db.AutoMigrate(&domain.Streak{}, &domain.UserShopOrder{}, &domain.UserShopUnlock{}); err != nil {
		t.Fatal(err)
	}
	if err := SeedShopCatalog(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestShopBuyOnceAndReplay(t *testing.T) {
	db := openShopDB(t)
	user := treeUser(t, db, "shop")
	if err := credit(db, user.ID, 9000); err != nil {
		t.Fatal(err)
	}
	repo := NewXuShopRepository(db)
	now := time.Now().UTC()
	first, err := repo.Buy(context.Background(), user.ID, "spa_01", "", "same", now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Charged != 9000 || first.Balance != 0 || first.Already {
		t.Fatalf("first %+v", first)
	}
	again, err := repo.Buy(context.Background(), user.ID, "spa_01", "", "same", now)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Already || again.Charged != 9000 || again.Balance != 0 {
		t.Fatalf("replay %+v", again)
	}
	if coinOf(t, db, user.ID) != 0 {
		t.Fatal("charged twice")
	}
	var item domain.UserMascotItem
	if err := db.Where("user_id = ? AND item_id = ?", user.ID, "spa_01").Take(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.Source != domain.MascotSourceShop {
		t.Fatalf("source %s", item.Source)
	}
	if _, err := repo.Buy(context.Background(), user.ID, "spa_04", "", "same", now); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict %v", err)
	}
}

func TestShopBuyRejectsShortBalanceOwnedAndBlocked(t *testing.T) {
	db := openShopDB(t)
	user := treeUser(t, db, "short")
	if err := credit(db, user.ID, 100); err != nil {
		t.Fatal(err)
	}
	repo := NewXuShopRepository(db)
	now := time.Now().UTC()
	_, err := repo.Buy(context.Background(), user.ID, "shop_sticker_may", "", "poor", now)
	var shopErr *ShopError
	if !errors.As(err, &shopErr) || shopErr.Code != "not_enough_coins" || shopErr.Needed != 500 || shopErr.Balance != 100 {
		t.Fatalf("short %+v %v", shopErr, err)
	}
	if err := credit(db, user.ID, 400); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Buy(context.Background(), user.ID, "shop_sticker_may", "", "got", now); err != nil {
		t.Fatal(err)
	}
	_, err = repo.Buy(context.Background(), user.ID, "shop_sticker_may", "", "again", now)
	if !errors.As(err, &shopErr) || shopErr.Code != "already_owned" {
		t.Fatalf("owned %v", err)
	}
	for _, id := range []string{domain.TreeItemSticker, domain.TreeItemClipLeaf, "acc_no_la_xanh", "spa_bonus", "spa_02_boy"} {
		_, err = repo.Buy(context.Background(), user.ID, id, "", "block-"+id, now)
		if !errors.As(err, &shopErr) || shopErr.Code != "not_for_sale" {
			t.Fatalf("%s %v", id, err)
		}
	}
	if coinOf(t, db, user.ID) != 0 {
		t.Fatal("blocked buy moved xu")
	}
}

func TestShopPearlUnlockSurvivesBrokenStreak(t *testing.T) {
	db := openShopDB(t)
	user := treeUser(t, db, "pearl")
	if err := credit(db, user.ID, 20000); err != nil {
		t.Fatal(err)
	}
	repo := NewXuShopRepository(db)
	now := time.Now().UTC()
	setStreak(t, db, user.ID, 12, now)
	_, err := repo.Buy(context.Background(), user.ID, "ngoctrai_05", "", "locked", now)
	var shopErr *ShopError
	if !errors.As(err, &shopErr) || shopErr.Code != "streak_locked" || shopErr.Streak != 12 || shopErr.Need != 30 {
		t.Fatalf("locked %+v %v", shopErr, err)
	}
	setStreak(t, db, user.ID, 30, now)
	got, err := repo.Buy(context.Background(), user.ID, "ngoctrai_05", "", "open", now)
	if err != nil || got.Charged != 8500 {
		t.Fatalf("open %+v %v", got, err)
	}
	var n int64
	if err := db.Model(&domain.UserShopUnlock{}).Where("user_id = ? AND unlock_key = ?", user.ID, domain.ShopUnlockPearl).Count(&n).Error; err != nil || n != 1 {
		t.Fatalf("unlock %d %v", n, err)
	}
	setStreak(t, db, user.ID, 30, now.AddDate(0, 0, -10))
	view, err := repo.Catalog(context.Background(), user.ID, now)
	if err != nil || view.Streak != 0 || !view.PearlUnlocked {
		t.Fatalf("broken view %+v %v", view, err)
	}
	next, err := repo.Buy(context.Background(), user.ID, "ngoctrai_04", "", "still", now)
	if err != nil || next.Charged != 9500 {
		t.Fatalf("after break %+v %v", next, err)
	}
}

func TestShopSeedChoiceAndSetGrant(t *testing.T) {
	db := openShopDB(t)
	user := treeUser(t, db, "seed")
	if err := credit(db, user.ID, 30000); err != nil {
		t.Fatal(err)
	}
	repo := NewXuShopRepository(db)
	now := time.Now().UTC()
	for _, key := range []string{domain.SeedTypeMystery, "dandelion", domain.TreeTypeLeaf, ""} {
		_, err := repo.Buy(context.Background(), user.ID, domain.ShopSeedItemID, key, "bad-"+key, now)
		var shopErr *ShopError
		if !errors.As(err, &shopErr) || shopErr.Code != "not_for_sale" {
			t.Fatalf("%q %v", key, err)
		}
	}
	got, err := repo.Buy(context.Background(), user.ID, domain.ShopSeedItemID, "cherry", "cherry", now)
	if err != nil || got.Charged != domain.ShopSeedPrice {
		t.Fatalf("seed %+v %v", got, err)
	}
	if shardCount(t, db, user.ID, domain.SeedKindShard, "cherry") != 1 {
		t.Fatal("cherry shard missing")
	}
	piece, err := repo.Buy(context.Background(), user.ID, "spa_01", "", "robe", now)
	if err != nil || piece.Charged != 9000 {
		t.Fatalf("piece %+v %v", piece, err)
	}
	rest, err := repo.Buy(context.Background(), user.ID, "set_spa", "", "set", now)
	if err != nil || rest.Charged != 16000 || len(rest.Bonus) != 1 || rest.Bonus[0].ID != "spa_bonus" {
		t.Fatalf("set %+v %v", rest, err)
	}
	var boy int64
	if err := db.Model(&domain.UserMascotItem{}).Where("user_id = ? AND item_id = ?", user.ID, "spa_02_boy").Count(&boy).Error; err != nil || boy != 1 {
		t.Fatalf("companion %d %v", boy, err)
	}
	if coinOf(t, db, user.ID) != 30000-5000-9000-16000 {
		t.Fatalf("balance %d", coinOf(t, db, user.ID))
	}
}

func TestShopBuyConcurrencyChargesOnce(t *testing.T) {
	db := openShopDB(t)
	user := treeUser(t, db, "race")
	if err := credit(db, user.ID, 9000); err != nil {
		t.Fatal(err)
	}
	repo := NewXuShopRepository(db)
	now := time.Now().UTC()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, key := range []string{"left", "right"} {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			_, errs[i] = repo.Buy(context.Background(), user.ID, "spa_01", "", key, now)
		}(i, key)
	}
	wg.Wait()
	wins, owned := 0, 0
	for _, err := range errs {
		if err == nil {
			wins++
			continue
		}
		var shopErr *ShopError
		if errors.As(err, &shopErr) && shopErr.Code == "already_owned" {
			owned++
			continue
		}
		t.Fatalf("race %v", err)
	}
	if wins != 1 || owned != 1 || coinOf(t, db, user.ID) != 0 {
		t.Fatalf("wins %d owned %d balance %d", wins, owned, coinOf(t, db, user.ID))
	}
}

func TestSpendCoinsKeepsPaidOffRandomDebits(t *testing.T) {
	db := openShopDB(t)
	user := treeUser(t, db, "paid")
	if err := credit(db, user.ID, 300); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.UserCoinWallet{}).Where("user_id = ?", user.ID).Updates(map[string]any{
		"balance": 300, "earned_balance": 100, "paid_balance": 200,
	}).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewXuShopRepository(db)
	now := time.Now().UTC()
	got, err := repo.Buy(context.Background(), user.ID, "shop_sticker_hoa", "", "sticker", now)
	if err != nil || got.Charged != 300 || got.Balance != 0 {
		t.Fatalf("shop %+v %v", got, err)
	}
	var rows []domain.UserCoinLedger
	if err := db.Where("user_id = ? AND amount < 0", user.ID).Order("bucket").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Bucket != domain.CoinBucketEarned || rows[0].Amount != -100 || rows[1].Bucket != domain.CoinBucketPaid || rows[1].Amount != -200 {
		t.Fatalf("ledger %+v", rows)
	}
	other := treeUser(t, db, "paid-only")
	if err := db.Create(&domain.UserCoinWallet{
		UserID: other.ID, Balance: 5000, PaidBalance: 5000, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		_, _, err := debitCoins(tx, other.ID, 100, uuid.NewString(), "gift_box", now)
		return err
	})
	if !errors.Is(err, ErrNotEnoughCoins) {
		t.Fatalf("paid debit %v", err)
	}
	var wallet domain.UserCoinWallet
	if err := db.Where("user_id = ?", other.ID).Take(&wallet).Error; err != nil {
		t.Fatal(err)
	}
	if wallet.Balance != 5000 || wallet.PaidBalance != 5000 || wallet.EarnedBalance != 0 {
		t.Fatalf("wallet %+v", wallet)
	}
}

func setStreak(t *testing.T, db *gorm.DB, userID uuid.UUID, days int, last time.Time) {
	t.Helper()
	day := time.Date(last.UTC().Year(), last.UTC().Month(), last.UTC().Day(), 0, 0, 0, 0, time.UTC)
	row := domain.Streak{
		UserID: userID, CurrentStreak: days, LongestStreak: days, LastCheckInDate: &day,
		FreezesAvailable: 0, CreatedAt: last, UpdatedAt: last,
	}
	err := db.Where("user_id = ?", userID).Delete(&domain.Streak{}).Error
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Omit("User").Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}
