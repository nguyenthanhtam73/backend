package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestRollShardRatio(t *testing.T) {
	commons := []string{"aloe", "cherry", "sunflower"}
	i := 0
	next := func(n int) int {
		if n == 10000 {
			v := i
			i++
			return v
		}
		return 0
	}
	mystery := 0
	cherry := 0
	for n := 0; n < 10000; n++ {
		got := rollShardType(next, 1000, commons)
		if got == domain.SeedTypeMystery {
			mystery++
			continue
		}
		if got != "aloe" {
			t.Fatalf("common pick %s", got)
		}
		cherry++
	}
	if mystery != 1000 || cherry != 9000 {
		t.Fatalf("mystery %d common %d", mystery, cherry)
	}
}

func TestBloomShardOnceAndLateJoinerNone(t *testing.T) {
	db := openTreeDB(t)
	restore := SetSeedNextInt(func(n int) int { return 0 })
	t.Cleanup(func() { SetSeedNextInt(restore) })
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	buddy := treeUser(t, db, "AI")
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	addTreeMember(t, db, code, buddy.ID, vnAt(2026, 11, 19, 11, 0), domain.RoomMemberAI)
	if _, err := repo.State(context.Background(), owner.ID, code, vnAt(2026, 11, 20, 8, 0)); err != nil {
		t.Fatal(err)
	}
	if err := forceStage(t, db, code, 6, "2026-11-19"); err != nil {
		t.Fatal(err)
	}
	when := vnAt(2026, 11, 20, 10, 0)
	treeSkin(t, db, owner.ID, when)
	state, err := repo.Water(context.Background(), owner.ID, code, "bloom", when)
	if err != nil || state.Stage != 7 || state.MyShard == nil || !state.MyShard.Mystery {
		t.Fatalf("bloom %+v shard %+v err %v", state.Stage, state.MyShard, err)
	}
	if got := shardCount(t, db, owner.ID, domain.SeedKindShard, domain.SeedTypeMystery); got != 1 {
		t.Fatalf("owner shards %d", got)
	}
	if got := shardCount(t, db, buddy.ID, domain.SeedKindShard, domain.SeedTypeMystery); got != 0 {
		t.Fatalf("ai shards %d", got)
	}
	var blooms int64
	if err := db.Model(&domain.RoomBloomShard{}).Where("room_code = ?", code).Count(&blooms).Error; err != nil {
		t.Fatal(err)
	}
	if blooms != 1 {
		t.Fatalf("bloom rows %d", blooms)
	}

	if err := forceStage(t, db, code, 6, "2026-11-20"); err != nil {
		t.Fatal(err)
	}
	next := vnAt(2026, 11, 21, 10, 0)
	treeSkin(t, db, owner.ID, next)
	again, err := repo.Water(context.Background(), owner.ID, code, "again", next)
	if err != nil || again.Stage != 7 {
		t.Fatalf("second bloom %+v %v", again.Stage, err)
	}
	if got := shardCount(t, db, owner.ID, domain.SeedKindShard, domain.SeedTypeMystery); got != 1 {
		t.Fatalf("double grant %d", got)
	}

	late := treeUser(t, db, "Na")
	addTreeMember(t, db, code, late.ID, vnAt(2026, 11, 21, 12, 0), domain.RoomMemberUser)
	if _, err := repo.State(context.Background(), late.ID, code, vnAt(2026, 11, 21, 13, 0)); err != nil {
		t.Fatal(err)
	}
	if got := shardCount(t, db, late.ID, domain.SeedKindShard, domain.SeedTypeMystery); got != 0 {
		t.Fatalf("late shards %d", got)
	}
	var lateBloom int64
	if err := db.Model(&domain.RoomBloomShard{}).Where("user_id = ?", late.ID).Count(&lateBloom).Error; err != nil {
		t.Fatal(err)
	}
	if lateBloom != 0 {
		t.Fatalf("late bloom rows %d", lateBloom)
	}
}

func TestMergeShardsAtFive(t *testing.T) {
	db := openTreeDB(t)
	owner := treeUser(t, db, "Lan")
	for i := 0; i < 4; i++ {
		giveShard(t, db, owner.ID, "cherry", i)
	}
	if got := shardCount(t, db, owner.ID, domain.SeedKindShard, "cherry"); got != 4 {
		t.Fatalf("four %d", got)
	}
	if got := shardCount(t, db, owner.ID, domain.SeedKindSeed, "cherry"); got != 0 {
		t.Fatalf("seed early %d", got)
	}
	giveShard(t, db, owner.ID, "cherry", 4)
	if got := shardCount(t, db, owner.ID, domain.SeedKindShard, "cherry"); got != 0 {
		t.Fatalf("shards after merge %d", got)
	}
	if got := shardCount(t, db, owner.ID, domain.SeedKindSeed, "cherry"); got != 1 {
		t.Fatalf("seeds %d", got)
	}
	var merges int64
	if err := db.Model(&domain.UserSeedLedger{}).Where("user_id = ? AND source = ?", owner.ID, domain.SeedSourceMerge).Count(&merges).Error; err != nil {
		t.Fatal(err)
	}
	if merges != 2 {
		t.Fatalf("merge ledger rows %d", merges)
	}
}

func TestTradeAndBuyShards(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	now := time.Now()
	giveShard(t, db, owner.ID, "cherry", 1)
	giveShard(t, db, owner.ID, "cherry", 2)
	giveShard(t, db, owner.ID, "aloe", 1)
	giveShard(t, db, owner.ID, domain.SeedTypeMystery, 1)
	_, err := repo.TradeShards(context.Background(), owner.ID, []BagStack{{TypeKey: domain.SeedTypeMystery, Count: 3}}, "cherry", "bad", now)
	if !errors.Is(err, ErrMysteryNotTradable) {
		t.Fatalf("mystery trade %v", err)
	}
	if got := shardCount(t, db, owner.ID, domain.SeedKindShard, domain.SeedTypeMystery); got != 1 {
		t.Fatalf("mystery changed %d", got)
	}
	_, err = repo.TradeShards(context.Background(), owner.ID, []BagStack{{TypeKey: "cherry", Count: 1}}, domain.SeedTypeMystery, "bad2", now)
	if !errors.Is(err, ErrMysteryNotTradable) {
		t.Fatalf("want mystery %v", err)
	}
	bag, err := repo.TradeShards(context.Background(), owner.ID, []BagStack{
		{TypeKey: "cherry", Count: 2},
		{TypeKey: "aloe", Count: 1},
	}, "sunflower", "trade-1", now)
	if err != nil || bag.Already {
		t.Fatalf("trade %+v %v", bag, err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindShard, "cherry") != 0 ||
		shardCount(t, db, owner.ID, domain.SeedKindShard, "aloe") != 0 ||
		shardCount(t, db, owner.ID, domain.SeedKindShard, "sunflower") != 1 {
		t.Fatal("trade counts")
	}
	again, err := repo.TradeShards(context.Background(), owner.ID, []BagStack{
		{TypeKey: "cherry", Count: 2},
		{TypeKey: "aloe", Count: 1},
	}, "sunflower", "trade-1", now)
	if err != nil || !again.Already || shardCount(t, db, owner.ID, domain.SeedKindShard, "sunflower") != 1 {
		t.Fatalf("replay %+v %v", again.Already, err)
	}

	if err := credit(db, owner.ID, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BuyShard(context.Background(), owner.ID, "cherry", "poor", now); !errors.Is(err, ErrNotEnoughCoins) {
		t.Fatalf("poor %v", err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindShard, "cherry") != 0 {
		t.Fatal("poor buy changed shards")
	}
	if bal := coinOf(t, db, owner.ID); bal != 100 {
		t.Fatalf("poor balance %d", bal)
	}
	if err := credit(db, owner.ID, 4900); err != nil {
		t.Fatal(err)
	}
	bought, err := repo.BuyShard(context.Background(), owner.ID, "cherry", "buy-1", now)
	if err != nil || bought.Already || bought.Balance != 0 {
		t.Fatalf("buy %+v %v", bought, err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindShard, "cherry") != 1 {
		t.Fatal("bought shard")
	}
	replay, err := repo.BuyShard(context.Background(), owner.ID, "cherry", "buy-1", now)
	if err != nil || !replay.Already || replay.Balance != 0 || shardCount(t, db, owner.ID, domain.SeedKindShard, "cherry") != 1 {
		t.Fatalf("buy replay %+v %v", replay, err)
	}
	if _, err := repo.BuyShard(context.Background(), owner.ID, domain.SeedTypeMystery, "nope", now); !errors.Is(err, ErrMysteryNotForSale) {
		t.Fatalf("mystery sale %v", err)
	}
}

func TestPlantSeedAndMysteryReveal(t *testing.T) {
	db := openTreeDB(t)
	restore := SetSeedNextInt(func(int) int { return 0 })
	t.Cleanup(func() { SetSeedNextInt(restore) })
	rooms := NewMascotRoomRepository(db)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	giveSeed(t, db, owner.ID, "cherry", 1)
	view, err := rooms.CreateWithTree(context.Background(), owner.ID, "cherry")
	if err != nil || view.TreeType != "cherry" || view.Revealed {
		t.Fatalf("cherry plant %+v %v", view, err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindSeed, "cherry") != 0 {
		t.Fatal("seed not spent")
	}
	giveSeed(t, db, owner.ID, "aloe", 1)
	if _, err := rooms.CreateWithTree(context.Background(), owner.ID, "aloe"); !errors.As(err, new(*AlreadyInRoomError)) {
		t.Fatalf("second room %v", err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindSeed, "aloe") != 1 {
		t.Fatal("failed create spent the seed")
	}

	other := treeUser(t, db, "Mây")
	giveSeed(t, db, other.ID, domain.SeedTypeMystery, 1)
	revealed, err := rooms.CreateWithTree(context.Background(), other.ID, domain.SeedTypeMystery)
	if err != nil || !revealed.Revealed || revealed.TreeType != "dandelion" {
		t.Fatalf("mystery %+v %v", revealed, err)
	}
	if shardCount(t, db, other.ID, domain.SeedKindSeed, domain.SeedTypeMystery) != 0 {
		t.Fatal("mystery seed remains")
	}
	var room domain.MascotRoom
	if err := db.Where("code = ?", revealed.Code).Take(&room).Error; err != nil {
		t.Fatal(err)
	}
	if room.TreeType != "dandelion" {
		t.Fatalf("stored %s", room.TreeType)
	}
	rows, err := repo.ListForUser(context.Background(), other.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.Key == "dandelion" {
			found = row.Revealed && !row.Creatable
		}
		if row.Key == "quynh" || row.Key == "sen_hong" {
			t.Fatalf("unrevealed rare listed %s", row.Key)
		}
	}
	if !found {
		t.Fatal("revealed dandelion missing")
	}
	leafOK := false
	for _, row := range rows {
		if row.Key == domain.TreeTypeLeaf && row.Creatable {
			leafOK = true
		}
	}
	if !leafOK {
		t.Fatal("leaf should stay creatable")
	}
}

func giveShard(t *testing.T, db *gorm.DB, user uuid.UUID, key string, n int) {
	t.Helper()
	err := db.Transaction(func(tx *gorm.DB) error {
		_, err := addShard(tx, user, key, domain.SeedSourceBuy, seedLedgerID("setup", user.String(), key, fmtID(n)), time.Now())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func giveSeed(t *testing.T, db *gorm.DB, user uuid.UUID, key string, n int) {
	t.Helper()
	err := db.Transaction(func(tx *gorm.DB) error {
		return adjustBalance(tx, user, domain.SeedKindSeed, key, n)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func forceStage(t *testing.T, db *gorm.DB, code string, stage int, leveled string) error {
	t.Helper()
	return db.Model(&domain.MascotRoomTree{}).Where("room_code = ?", code).Updates(map[string]any{
		"stage": stage, "leveled_on": leveled, "progress_num": 0, "progress_den": 1,
	}).Error
}

func credit(db *gorm.DB, user uuid.UUID, amount int64) error {
	return db.Transaction(func(tx *gorm.DB) error {
		_, err := creditCoins(tx, user, amount, seedLedgerID("credit", user.String(), fmtID(int(amount)), uuid.NewString()), "test", time.Now())
		return err
	})
}

func coinOf(t *testing.T, db *gorm.DB, user uuid.UUID) int64 {
	t.Helper()
	var bal int64
	if err := db.Model(&domain.UserCoinWallet{}).Where("user_id = ?", user).Pluck("balance", &bal).Error; err != nil {
		t.Fatal(err)
	}
	return bal
}

func shardCount(t *testing.T, db *gorm.DB, user uuid.UUID, kind, key string) int {
	t.Helper()
	var row domain.UserSeedBalance
	err := db.Where("user_id = ? AND kind = ? AND type_key = ?", user, kind, key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return row.Count
}

func fmtID(n int) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte{byte(n)}).String()
}
