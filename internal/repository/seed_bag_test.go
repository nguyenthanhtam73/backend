package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/pkg/usermsg"
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
	if err != nil || state.Stage != 7 || !state.CanDraw || state.MyShard != nil {
		t.Fatalf("bloom %+v shard %+v draw %v err %v", state.Stage, state.MyShard, state.CanDraw, err)
	}
	if got := shardCount(t, db, owner.ID, domain.SeedKindShard, domain.SeedTypeMystery); got != 0 {
		t.Fatalf("shard granted before the draw %d", got)
	}
	pick := 2
	drawn, err := repo.DrawShard(context.Background(), owner.ID, code, "flip", &pick, when)
	if err != nil || drawn.Already || !drawn.Shard.Mystery || drawn.Pick == nil || *drawn.Pick != 2 {
		t.Fatalf("draw %+v %v", drawn, err)
	}
	otherPick := 0
	againDraw, err := repo.DrawShard(context.Background(), owner.ID, code, "flip-again", &otherPick, when)
	if err != nil || !againDraw.Already || !againDraw.Shard.Mystery {
		t.Fatalf("second draw %+v %v", againDraw, err)
	}
	if got := shardCount(t, db, owner.ID, domain.SeedKindShard, domain.SeedTypeMystery); got != 1 {
		t.Fatalf("owner shards %d", got)
	}
	if _, err := repo.DrawShard(context.Background(), buddy.ID, code, "ai", nil, when); !errors.Is(err, ErrTreeNotReal) {
		t.Fatalf("ai draw %v", err)
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
	if _, err := repo.DrawShard(context.Background(), late.ID, code, "late", nil, vnAt(2026, 11, 21, 13, 0)); !errors.Is(err, ErrDrawUnavailable) {
		t.Fatalf("late draw %v", err)
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
	giveShard(t, db, owner.ID, "cherry", 3)
	giveShard(t, db, owner.ID, domain.SeedTypeMystery, 1)
	_, err := repo.TradeShards(context.Background(), owner.ID, []BagStack{{TypeKey: domain.SeedTypeMystery, Count: 3}}, "cherry", "bad", now)
	if !errors.Is(err, ErrMysteryNotTradable) {
		t.Fatalf("mystery trade %v", err)
	}
	if got := shardCount(t, db, owner.ID, domain.SeedKindShard, domain.SeedTypeMystery); got != 1 {
		t.Fatalf("mystery changed %d", got)
	}
	_, err = repo.TradeShards(context.Background(), owner.ID, []BagStack{{TypeKey: "cherry", Count: 3}}, domain.SeedTypeMystery, "bad2", now)
	if !errors.Is(err, ErrMysteryNotTradable) {
		t.Fatalf("want mystery %v", err)
	}
	_, err = repo.TradeShards(context.Background(), owner.ID, []BagStack{
		{TypeKey: "cherry", Count: 2},
		{TypeKey: "aloe", Count: 1},
	}, "sunflower", "mixed", now)
	if !errors.Is(err, ErrBadTrade) {
		t.Fatalf("mixed %v", err)
	}
	_, err = repo.TradeShards(context.Background(), owner.ID, []BagStack{{TypeKey: "cherry", Count: 3}}, "cherry", "same", now)
	if !errors.Is(err, ErrSameShardType) {
		t.Fatalf("same type %v", err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindShard, "cherry") != 3 {
		t.Fatal("rejected trades spent shards")
	}
	bag, err := repo.TradeShards(context.Background(), owner.ID, []BagStack{
		{TypeKey: "cherry", Count: 3},
	}, "sunflower", "trade-1", now)
	if err != nil || bag.Already {
		t.Fatalf("trade %+v %v", bag, err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindShard, "cherry") != 0 ||
		shardCount(t, db, owner.ID, domain.SeedKindShard, "sunflower") != 1 {
		t.Fatal("trade counts")
	}
	again, err := repo.TradeShards(context.Background(), owner.ID, []BagStack{
		{TypeKey: "cherry", Count: 3},
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
	if err := credit(db, owner.ID, 5000); err != nil {
		t.Fatal(err)
	}
	second, err := repo.BuyShard(context.Background(), owner.ID, "aloe", "buy-2", now)
	if err != nil || second.Already || shardCount(t, db, owner.ID, domain.SeedKindShard, "aloe") != 1 {
		t.Fatalf("second buy %+v %v", second, err)
	}
	if bag.SlotCount != 18 || bag.ComingSoonCount != 12 {
		t.Fatalf("slots %d coming %d", bag.SlotCount, bag.ComingSoonCount)
	}
	var sen, dandelion bool
	for _, row := range bag.Catalog {
		if row.TypeKey == "sen_hong" {
			sen = row.ComingSoon && !row.Available && !row.InMysteryPool
		}
		if row.TypeKey == "dandelion" {
			dandelion = row.Available && row.InMysteryPool && !row.Revealed
		}
	}
	if !sen || !dandelion {
		t.Fatalf("catalog sen %v dandelion %v", sen, dandelion)
	}
}

func TestPlantSeedAndMysteryReveal(t *testing.T) {
	db := openTreeDB(t)
	nRoll := 0
	restore := SetSeedNextInt(func(max int) int {
		v := nRoll % max
		nRoll++
		return v
	})
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
	third := treeUser(t, db, "Na")
	giveSeed(t, db, third.ID, domain.SeedTypeMystery, 1)
	second, err := rooms.CreateWithTree(context.Background(), third.ID, domain.SeedTypeMystery)
	if err != nil || second.TreeType != "quynh" {
		t.Fatalf("second rare %+v %v", second, err)
	}
	if second.TreeType == "sen_hong" || revealed.TreeType == "sen_hong" {
		t.Fatal("sen hồng is not in the mystery pool yet")
	}
}

func TestBuyAndTradeRequireIdempotencyKey(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	now := time.Now()
	giveShard(t, db, owner.ID, "cherry", 1)
	giveShard(t, db, owner.ID, "cherry", 2)
	giveShard(t, db, owner.ID, "cherry", 3)
	if err := credit(db, owner.ID, 5000); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BuyShard(context.Background(), owner.ID, "cherry", "  ", now); !errors.Is(err, ErrIdempotencyKey) {
		t.Fatalf("empty buy key %v", err)
	}
	if _, err := repo.TradeShards(context.Background(), owner.ID, []BagStack{{TypeKey: "cherry", Count: 3}}, "aloe", "", now); !errors.Is(err, ErrIdempotencyKey) {
		t.Fatalf("empty trade key %v", err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindShard, "cherry") != 3 || coinOf(t, db, owner.ID) != 5000 {
		t.Fatal("missing key changed the bag or the wallet")
	}
	if _, err := repo.BuyShard(context.Background(), owner.ID, "cherry", "  ", now); !errors.Is(err, ErrIdempotencyKey) {
		t.Fatalf("second empty buy %v", err)
	}
	if coinOf(t, db, owner.ID) != 5000 {
		t.Fatal("second empty buy charged")
	}
}

func TestIdempotencyKeyRejectsADifferentPayload(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	now := time.Now()
	if err := credit(db, owner.ID, 10000); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BuyShard(context.Background(), owner.ID, "cherry", "one-key", now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BuyShard(context.Background(), owner.ID, "aloe", "one-key", now); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict %v", err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindShard, "cherry") != 1 ||
		shardCount(t, db, owner.ID, domain.SeedKindShard, "aloe") != 0 ||
		coinOf(t, db, owner.ID) != 5000 {
		t.Fatal("conflict charged or granted")
	}
	giveShard(t, db, owner.ID, "sunflower", 1)
	giveShard(t, db, owner.ID, "sunflower", 2)
	giveShard(t, db, owner.ID, "sunflower", 3)
	if _, err := repo.TradeShards(context.Background(), owner.ID, []BagStack{{TypeKey: "sunflower", Count: 3}}, "cherry", "trade-key", now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.TradeShards(context.Background(), owner.ID, []BagStack{{TypeKey: "sunflower", Count: 3}}, "aloe", "trade-key", now); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("trade conflict %v", err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindShard, "sunflower") != 0 ||
		shardCount(t, db, owner.ID, domain.SeedKindShard, "aloe") != 0 ||
		shardCount(t, db, owner.ID, domain.SeedKindShard, "cherry") != 2 {
		t.Fatal("trade conflict moved shards")
	}
	if usermsg.Plain((tradeOfferError{need: 3}).Error()) != "Bạn cần đúng 3 cánh cùng một loại thường." {
		t.Fatalf("trade copy %q", usermsg.Plain((tradeOfferError{need: 3}).Error()))
	}
}

func TestConcurrentBuyReturnsAlready(t *testing.T) {
	db := openTreeDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(2)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	now := time.Now()
	if err := credit(db, owner.ID, 5000); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	type result struct {
		already bool
		err     error
	}
	out := make([]result, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			bag, err := repo.BuyShard(context.Background(), owner.ID, "cherry", "tap", now)
			out[i] = result{already: bag.Already, err: err}
		}(i)
	}
	wg.Wait()
	already := 0
	for _, row := range out {
		if row.err != nil {
			t.Fatalf("concurrent buy %v", row.err)
		}
		if row.already {
			already++
		}
	}
	if already != 1 || shardCount(t, db, owner.ID, domain.SeedKindShard, "cherry") != 1 || coinOf(t, db, owner.ID) != 0 {
		t.Fatalf("results %+v shards %d balance %d", out, shardCount(t, db, owner.ID, domain.SeedKindShard, "cherry"), coinOf(t, db, owner.ID))
	}
}

func TestLeftMemberCanStillDraw(t *testing.T) {
	db := openTreeDB(t)
	restore := SetSeedNextInt(func(n int) int { return 0 })
	t.Cleanup(func() { SetSeedNextInt(restore) })
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	friend := treeUser(t, db, "Mây")
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	addTreeMember(t, db, code, friend.ID, vnAt(2026, 11, 19, 11, 0), domain.RoomMemberUser)
	if _, err := repo.State(context.Background(), owner.ID, code, vnAt(2026, 11, 20, 8, 0)); err != nil {
		t.Fatal(err)
	}
	if err := forceStage(t, db, code, 6, "2026-11-19"); err != nil {
		t.Fatal(err)
	}
	when := vnAt(2026, 11, 20, 10, 0)
	treeSkin(t, db, owner.ID, when)
	treeSkin(t, db, friend.ID, when)
	if _, err := repo.Water(context.Background(), owner.ID, code, "a", when); err != nil {
		t.Fatal(err)
	}
	state, err := repo.Water(context.Background(), friend.ID, code, "b", when.Add(time.Minute))
	if err != nil || state.Stage != 7 || !state.CanDraw {
		t.Fatalf("bloom %+v %v", state.Stage, err)
	}
	leaveTreeMember(t, db, code, friend.ID, when.Add(2*time.Minute))
	pick := 1
	drawn, err := repo.DrawShard(context.Background(), friend.ID, code, "flip", &pick, when.Add(3*time.Minute))
	if err != nil || drawn.Already || drawn.Shard.TypeKey == "" {
		t.Fatalf("left draw %+v %v", drawn, err)
	}
	if shardCount(t, db, friend.ID, domain.SeedKindShard, drawn.Shard.TypeKey) != 1 {
		t.Fatal("left member did not receive one shard")
	}
	other := 0
	if _, err := repo.DrawShard(context.Background(), friend.ID, code, "flip", &other, when.Add(4*time.Minute)); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("same key different pick %v", err)
	}
	if shardCount(t, db, friend.ID, domain.SeedKindShard, drawn.Shard.TypeKey) != 1 {
		t.Fatal("conflict granted again")
	}
}

func TestCleanupGrantsPendingDrawOnce(t *testing.T) {
	db := openTreeDB(t)
	restore := SetSeedNextInt(func(n int) int { return 0 })
	t.Cleanup(func() { SetSeedNextInt(restore) })
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	if _, err := repo.State(context.Background(), owner.ID, code, vnAt(2026, 11, 20, 8, 0)); err != nil {
		t.Fatal(err)
	}
	if err := forceStage(t, db, code, 6, "2026-11-19"); err != nil {
		t.Fatal(err)
	}
	when := vnAt(2026, 11, 20, 10, 0)
	treeSkin(t, db, owner.ID, when)
	state, err := repo.Water(context.Background(), owner.ID, code, "bloom", when)
	if err != nil || state.Stage != 7 || !state.CanDraw || state.MyShard != nil {
		t.Fatalf("pending bloom %+v %v", state, err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindShard, domain.SeedTypeMystery) != 0 {
		t.Fatal("shard before cleanup")
	}
	if err := deleteRoomSharedRows(db, []string{code}); err != nil {
		t.Fatal(err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindShard, domain.SeedTypeMystery) != 1 {
		t.Fatal("cleanup did not grant")
	}
	var blooms int64
	if err := db.Model(&domain.RoomBloomShard{}).Where("room_code = ?", code).Count(&blooms).Error; err != nil {
		t.Fatal(err)
	}
	if blooms != 0 {
		t.Fatalf("bloom rows left %d", blooms)
	}
	if err := deleteRoomSharedRows(db, []string{code}); err != nil {
		t.Fatal(err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindShard, domain.SeedTypeMystery) != 1 {
		t.Fatal("cleanup granted twice")
	}
	var grants int64
	if err := db.Model(&domain.UserSeedLedger{}).Where("user_id = ? AND source = ?", owner.ID, domain.SeedSourceBloom).Count(&grants).Error; err != nil {
		t.Fatal(err)
	}
	if grants != 1 {
		t.Fatalf("bloom ledger %d", grants)
	}
}

func TestSeedSeedBagKeepsEditedRows(t *testing.T) {
	db := openTreeDB(t)
	if err := db.Model(&domain.TreeType{}).Where("key = ?", "dandelion").Updates(map[string]any{
		"mystery_pool": false, "plantable": false,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.SeedBagConfig{}).Where("key = ?", domain.SeedConfigBuyPrice).Update("int_value", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("key = ?", domain.SeedConfigMysteryBps).Delete(&domain.SeedBagConfig{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := SeedSeedBag(db); err != nil {
		t.Fatal(err)
	}
	var typ domain.TreeType
	if err := db.Where("key = ?", "dandelion").Take(&typ).Error; err != nil {
		t.Fatal(err)
	}
	if typ.MysteryPool || typ.Plantable {
		t.Fatalf("boot rewrote flags %+v", typ)
	}
	var price domain.SeedBagConfig
	if err := db.Where("key = ?", domain.SeedConfigBuyPrice).Take(&price).Error; err != nil {
		t.Fatal(err)
	}
	if price.IntValue != 1 {
		t.Fatalf("price reset to %d", price.IntValue)
	}
	var bps domain.SeedBagConfig
	if err := db.Where("key = ?", domain.SeedConfigMysteryBps).Take(&bps).Error; err != nil {
		t.Fatal(err)
	}
	if bps.IntValue != 1000 {
		t.Fatalf("missing rule not restored %+v", bps)
	}
}

func TestBloomAndPlantLedgerIDsIncludeCreatedAt(t *testing.T) {
	user := uuid.New()
	first := domain.MascotRoom{Code: "AB23CD", CreatedAt: time.Unix(0, 10).UTC()}
	second := first
	second.CreatedAt = time.Unix(0, 11).UTC()
	if bloomLedgerID(first, user) == bloomLedgerID(second, user) {
		t.Fatal("bloom ledger id ignored created_at")
	}
	if plantLedgerID(first.Code, first.CreatedAt, user, "cherry") == plantLedgerID(second.Code, second.CreatedAt, user, "cherry") {
		t.Fatal("plant ledger id ignored created_at")
	}
}

func TestPlantFailureAfterConsumeReturnsTheSeed(t *testing.T) {
	db := openTreeDB(t)
	rooms := NewMascotRoomRepository(db)
	owner := treeUser(t, db, "Lan")
	giveSeed(t, db, owner.ID, "cherry", 1)
	plantFailAfterConsume = func() error { return errors.New("boom") }
	t.Cleanup(func() { plantFailAfterConsume = nil })
	if _, err := rooms.CreateWithTree(context.Background(), owner.ID, "cherry"); err == nil || err.Error() != "boom" {
		t.Fatalf("create %v", err)
	}
	if shardCount(t, db, owner.ID, domain.SeedKindSeed, "cherry") != 1 {
		t.Fatal("seed was not returned")
	}
	var roomsLeft int64
	if err := db.Model(&domain.MascotRoom{}).Count(&roomsLeft).Error; err != nil {
		t.Fatal(err)
	}
	if roomsLeft != 0 {
		t.Fatalf("room left behind %d", roomsLeft)
	}
}

func TestRollRareSkipsUnplantable(t *testing.T) {
	db := openTreeDB(t)
	if err := db.Model(&domain.TreeType{}).Where("key = ?", "dandelion").Update("plantable", false).Error; err != nil {
		t.Fatal(err)
	}
	restore := SetSeedNextInt(func(n int) int { return 0 })
	t.Cleanup(func() { SetSeedNextInt(restore) })
	var got domain.TreeType
	err := db.Transaction(func(tx *gorm.DB) error {
		row, err := rollRare(tx)
		got = row
		return err
	})
	if err != nil || got.Key != "quynh" {
		t.Fatalf("rolled %+v %v", got.Key, err)
	}
}

func TestLedgerAmountCheckAction(t *testing.T) {
	if ledgerAmountCheckAction("CHECK ((amount <> 0))") != "keep" {
		t.Fatal("already allows a debit")
	}
	if ledgerAmountCheckAction("CHECK ((amount != 0))") != "keep" {
		t.Fatal("!= form")
	}
	if ledgerAmountCheckAction("CHECK ((amount > 0))") != "replace" {
		t.Fatal("old check")
	}
	if ledgerAmountCheckAction("") != "add" {
		t.Fatal("missing check")
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
