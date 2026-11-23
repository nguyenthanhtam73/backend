package repository

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTreeMigrationIsDataNotEnum(t *testing.T) {
	upBytes, err := os.ReadFile("../../migrations/041_mascot_room_tree.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	if strings.Contains(strings.ToUpper(up), "CHECK (") {
		t.Fatal("tree migration must not use a CHECK constraint")
	}
	for _, piece := range []string{
		"tree_types", "mascot_rooms_tree_type_fkey", "DEFAULT 'leaf'",
		"mascot_room_tree_waters", "mascot_room_tree_rewards", "preview_only",
		"PRIMARY KEY (room_code, user_id, vn_date)",
		"PRIMARY KEY (room_code, user_id, stage)",
		"seen_at",
	} {
		if !strings.Contains(up, piece) {
			t.Fatalf("up migration missing %s", piece)
		}
	}
	for _, row := range domain.TreeTypeSeed() {
		if !strings.Contains(up, "'"+row.Key+"'") || !strings.Contains(up, row.NameVI) || !strings.Contains(up, row.ExclusiveItemKey) {
			t.Fatalf("seed missing %+v", row)
		}
	}
	for _, key := range []string{"dandelion", "quynh", "sen_hong"} {
		if !strings.Contains(up, "'"+key+"'") {
			t.Fatalf("rare %s missing", key)
		}
	}
	down, err := os.ReadFile("../../migrations/041_mascot_room_tree.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(down), "DROP TABLE IF EXISTS tree_types") {
		t.Fatal("down migration")
	}
}

func TestTreeRequiredLockedAtVietnamMidnight(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	early := treeUser(t, db, "Mây")
	late := treeUser(t, db, "Na")
	day := vnAt(2026, 11, 20, 10, 0)
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	addTreeMember(t, db, code, early.ID, vnAt(2026, 11, 19, 11, 0), domain.RoomMemberUser)

	state, err := repo.State(context.Background(), owner.ID, code, day)
	if err != nil {
		t.Fatal(err)
	}
	if state.Required != 2 || state.Waters != 0 || state.TreeType != domain.TreeTypeLeaf {
		t.Fatalf("morning %+v", state)
	}

	addTreeMember(t, db, code, late.ID, vnAt(2026, 11, 20, 11, 0), domain.RoomMemberUser)
	treeSkin(t, db, late.ID, vnAt(2026, 11, 20, 11, 30))
	state, err = repo.Water(context.Background(), late.ID, code, "late", vnAt(2026, 11, 20, 11, 30))
	if err != nil {
		t.Fatal(err)
	}
	if state.Required != 2 || state.Waters != 1 || !state.FreshWater || state.Stage != 1 {
		t.Fatalf("late water %+v", state)
	}
	if len(state.Watered) != 1 || state.Watered[0] != "Na" {
		t.Fatalf("names %+v", state.Watered)
	}
}

func TestTreeLeaveDropsRequired(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	a := treeUser(t, db, "Mây")
	b := treeUser(t, db, "Na")
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	addTreeMember(t, db, code, a.ID, vnAt(2026, 11, 19, 11, 0), domain.RoomMemberUser)
	addTreeMember(t, db, code, b.ID, vnAt(2026, 11, 19, 12, 0), domain.RoomMemberUser)
	morning := vnAt(2026, 11, 20, 9, 0)
	state, err := repo.State(context.Background(), owner.ID, code, morning)
	if err != nil || state.Required != 3 {
		t.Fatalf("before leave required %+v %v", state.Required, err)
	}
	leaveTreeMember(t, db, code, b.ID, vnAt(2026, 11, 20, 11, 0))
	state, err = repo.State(context.Background(), owner.ID, code, vnAt(2026, 11, 20, 12, 0))
	if err != nil || state.Required != 2 || state.Stage != 1 {
		t.Fatalf("after leave %+v %v", state, err)
	}
}

func TestTreeLeaveHookDropsRequired(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	rooms := NewMascotRoomRepository(db)
	owner := treeUser(t, db, "Lan")
	guest := treeUser(t, db, "Mây")
	view, err := rooms.Create(context.Background(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rooms.Join(context.Background(), guest.ID, view.Code); err != nil {
		t.Fatal(err)
	}
	yesterday := vnMidnight(time.Now()).Add(-2 * time.Hour)
	if err := db.Model(&domain.MascotRoom{}).Where("code = ?", view.Code).Update("created_at", yesterday).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.MascotRoomMember{}).Where("room_code = ?", view.Code).Update("joined_at", yesterday).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	state, err := repo.State(context.Background(), owner.ID, view.Code, now)
	if err != nil || state.Required != 2 {
		t.Fatalf("before %+v %v", state.Required, err)
	}
	if err := rooms.Leave(context.Background(), guest.ID, view.Code); err != nil {
		t.Fatal(err)
	}
	var leaves int64
	if err := db.Model(&domain.MascotRoomMemberLeave{}).Where("user_id = ?", guest.ID).Count(&leaves).Error; err != nil {
		t.Fatal(err)
	}
	if leaves != 1 {
		t.Fatalf("leave rows %d", leaves)
	}
	state, err = repo.Sync(context.Background(), view.Code, time.Now())
	if err != nil || state.Required != 1 {
		t.Fatalf("after hook %+v %v", state.Required, err)
	}
}

func TestTreePartialProgressAcrossDays(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	a := treeUser(t, db, "Mây")
	b := treeUser(t, db, "Na")
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	addTreeMember(t, db, code, a.ID, vnAt(2026, 11, 19, 11, 0), domain.RoomMemberUser)
	addTreeMember(t, db, code, b.ID, vnAt(2026, 11, 19, 12, 0), domain.RoomMemberUser)
	day1 := vnAt(2026, 11, 20, 10, 0)
	treeSkin(t, db, owner.ID, day1)
	treeSkin(t, db, a.ID, day1)
	if _, err := repo.Water(context.Background(), owner.ID, code, "a", day1); err != nil {
		t.Fatal(err)
	}
	state, err := repo.Water(context.Background(), a.ID, code, "b", day1.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if state.Stage != 1 || state.Waters != 2 || state.Required != 3 || state.Balance != 0 {
		t.Fatalf("day1 %+v", state)
	}
	near(t, state.Progress, 2.0/3.0)

	day2early := vnAt(2026, 11, 21, 0, 30)
	state, err = repo.State(context.Background(), owner.ID, code, day2early)
	if err != nil || state.Stage != 1 || state.Waters != 0 || state.Required != 3 {
		t.Fatalf("day2 open %+v %v", state, err)
	}
	near(t, state.Progress, 2.0/3.0)

	day2 := vnAt(2026, 11, 21, 10, 0)
	treeSkin(t, db, b.ID, day2)
	state, err = repo.Water(context.Background(), b.ID, code, "c", day2)
	if err != nil || state.Stage != 2 || state.Waters != 1 || state.Required != 3 {
		t.Fatalf("day2 water %+v %v", state, err)
	}
	near(t, state.Progress, 0)
	if len(state.JustGranted) != 1 || state.JustGranted[0].Coins != 20 {
		t.Fatalf("grant %+v", state.JustGranted)
	}

	day3 := vnAt(2026, 11, 22, 0, 30)
	state, err = repo.State(context.Background(), owner.ID, code, day3)
	if err != nil || state.Stage != 2 || state.Required != 3 || len(state.JustGranted) != 0 {
		t.Fatalf("day3 %+v %v", state, err)
	}
	near(t, state.Progress, 0)
	for _, user := range []*domain.User{owner, a, b} {
		var balance int64
		if err := db.Model(&domain.UserCoinWallet{}).Where("user_id = ?", user.ID).Pluck("balance", &balance).Error; err != nil {
			t.Fatal(err)
		}
		if balance != 20 {
			t.Fatalf("%s balance %d", user.DisplayName, balance)
		}
	}
}

func TestTreeDoubleWaterAndReplay(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	when := vnAt(2026, 11, 20, 10, 0)
	treeSkin(t, db, owner.ID, when)
	first, err := repo.Water(context.Background(), owner.ID, code, "tap-1", when)
	if err != nil || first.Stage != 2 || first.Balance != 20 || !first.FreshWater {
		t.Fatalf("first %+v %v", first, err)
	}
	if _, err := repo.Water(context.Background(), owner.ID, code, "", when.Add(time.Second)); !errors.Is(err, ErrTreeAlreadyWatered) {
		t.Fatalf("second %v", err)
	}
	replay, err := repo.Water(context.Background(), owner.ID, code, "tap-1", when.Add(2*time.Second))
	if err != nil || !replay.Already || replay.Balance != 20 || replay.FreshWater {
		t.Fatalf("replay %+v %v", replay, err)
	}
	var waters int64
	if err := db.Model(&domain.MascotRoomTreeWater{}).Count(&waters).Error; err != nil {
		t.Fatal(err)
	}
	if waters != 1 {
		t.Fatalf("waters %d", waters)
	}
	var ledger int64
	if err := db.Model(&domain.UserCoinLedger{}).Where("reason = ?", domain.CoinReasonTreeStage).Count(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	if ledger != 1 {
		t.Fatalf("ledger %d", ledger)
	}
}

func TestTreeMaxOneLevelPerDay(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	friend := treeUser(t, db, "Mây")
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	addTreeMember(t, db, code, friend.ID, vnAt(2026, 11, 19, 11, 0), domain.RoomMemberUser)
	day1 := vnAt(2026, 11, 20, 10, 0)
	treeSkin(t, db, owner.ID, day1)
	state, err := repo.Water(context.Background(), owner.ID, code, "d1", day1)
	if err != nil || state.Stage != 1 {
		t.Fatalf("partial %+v %v", state, err)
	}
	day2 := vnAt(2026, 11, 21, 10, 0)
	treeSkin(t, db, owner.ID, day2)
	treeSkin(t, db, friend.ID, day2)
	if _, err := repo.Water(context.Background(), owner.ID, code, "d2a", day2); err != nil {
		t.Fatal(err)
	}
	state, err = repo.Water(context.Background(), friend.ID, code, "d2b", day2.Add(time.Minute))
	if err != nil || state.Stage != 2 || state.Balance != 20 {
		t.Fatalf("full day %+v %v", state, err)
	}
	near(t, state.Progress, 0.5)
	if _, err := repo.Water(context.Background(), owner.ID, code, "again", day2.Add(2*time.Minute)); !errors.Is(err, ErrTreeAlreadyWatered) {
		t.Fatalf("same day %v", err)
	}
	var rewards int64
	if err := db.Model(&domain.MascotRoomTreeReward{}).Count(&rewards).Error; err != nil {
		t.Fatal(err)
	}
	if rewards != 2 {
		t.Fatalf("reward rows %d, want one stage for two members", rewards)
	}
}

func TestTreeRewardsKeptAfterRoomExpiry(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	start := vnAt(2026, 11, 9, 10, 0)
	code := treeRoom(t, db, owner, start)
	for i := 0; i < 6; i++ {
		when := vnAt(2026, 11, 10+i, 10, 0)
		treeSkin(t, db, owner.ID, when)
		state, err := repo.Water(context.Background(), owner.ID, code, "day", when)
		if err != nil {
			t.Fatalf("day %d %v", i, err)
		}
		if state.Stage != i+2 {
			t.Fatalf("day %d stage %d", i, state.Stage)
		}
	}
	done, err := repo.State(context.Background(), owner.ID, code, vnAt(2026, 11, 15, 12, 0))
	if err != nil || !done.Bloomed || done.Balance != 420 || done.CanWater || done.Reason != TreeReasonBloomed {
		t.Fatalf("bloomed %+v %v", done, err)
	}
	if _, err := repo.Water(context.Background(), owner.ID, code, "", vnAt(2026, 11, 16, 10, 0)); !errors.Is(err, ErrTreeBloomed) {
		t.Fatalf("after bloom %v", err)
	}
	var owned []domain.UserMascotItem
	if err := db.Where("user_id = ?", owner.ID).Find(&owned).Error; err != nil {
		t.Fatal(err)
	}
	got := map[string]domain.UserMascotItem{}
	for _, row := range owned {
		got[row.ItemID] = row
	}
	for _, id := range []string{domain.TreeItemSticker, domain.TreeItemNameFrame, domain.TreeItemClipLeaf} {
		row, ok := got[id]
		if !ok || row.Source != domain.MascotSourceRoomReward {
			t.Fatalf("missing %s %+v", id, row)
		}
	}
	if !got[domain.TreeItemSticker].PreviewOnly || !got[domain.TreeItemNameFrame].PreviewOnly || got[domain.TreeItemClipLeaf].PreviewOnly {
		t.Fatalf("preview flags %+v", got)
	}
	var clip domain.MascotItem
	if err := db.Where("id = ?", domain.TreeItemClipLeaf).Take(&clip).Error; err != nil {
		t.Fatal(err)
	}
	if clip.IsFree || clip.Price != nil {
		t.Fatalf("clip is sold %+v", clip)
	}
	past := vnAt(2026, 11, 16, 8, 0)
	if err := db.Model(&domain.MascotRoom{}).Where("code = ?", code).Update("ends_at", past).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Water(context.Background(), owner.ID, code, "", vnAt(2026, 11, 17, 10, 0)); !errors.Is(err, ErrMascotRoomEnded) {
		t.Fatalf("ended %v", err)
	}
	var balance int64
	if err := db.Model(&domain.UserCoinWallet{}).Where("user_id = ?", owner.ID).Pluck("balance", &balance).Error; err != nil {
		t.Fatal(err)
	}
	if balance != 420 {
		t.Fatalf("balance after end %d", balance)
	}
	var still int64
	if err := db.Model(&domain.UserMascotItem{}).Where("user_id = ?", owner.ID).Count(&still).Error; err != nil {
		t.Fatal(err)
	}
	if still != 3 {
		t.Fatalf("items after end %d", still)
	}
}

func TestTreeLateJoinerNoBackPay(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	late := treeUser(t, db, "Na")
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	when := vnAt(2026, 11, 20, 10, 0)
	treeSkin(t, db, owner.ID, when)
	if _, err := repo.Water(context.Background(), owner.ID, code, "a", when); err != nil {
		t.Fatal(err)
	}
	addTreeMember(t, db, code, late.ID, vnAt(2026, 11, 21, 10, 0), domain.RoomMemberUser)
	state, err := repo.State(context.Background(), late.ID, code, vnAt(2026, 11, 21, 11, 0))
	if err != nil {
		t.Fatal(err)
	}
	if state.Stage != 2 || state.Balance != 0 || len(state.MyRewards) != 0 {
		t.Fatalf("late %+v", state)
	}
	var rewards int64
	if err := db.Model(&domain.MascotRoomTreeReward{}).Where("user_id = ?", late.ID).Count(&rewards).Error; err != nil {
		t.Fatal(err)
	}
	if rewards != 0 {
		t.Fatalf("back-pay rows %d", rewards)
	}
}

func TestTreeNeedsSkinCheck(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	when := vnAt(2026, 11, 20, 10, 0)
	state, err := repo.State(context.Background(), owner.ID, code, when)
	if err != nil || state.CanWater || state.Reason != TreeReasonNeedsSkinCheck {
		t.Fatalf("gate %+v %v", state, err)
	}
	if _, err := repo.Water(context.Background(), owner.ID, code, "", when); !errors.Is(err, ErrTreeNeedsSkinCheck) {
		t.Fatalf("water %v", err)
	}
	var waters int64
	if err := db.Model(&domain.MascotRoomTreeWater{}).Count(&waters).Error; err != nil {
		t.Fatal(err)
	}
	if waters != 0 {
		t.Fatalf("waters %d", waters)
	}
	treeSkin(t, db, owner.ID, when)
	if _, err := repo.Water(context.Background(), owner.ID, code, "", when); err != nil {
		t.Fatal(err)
	}
}

func TestTreeCatalogCreateAllowsLeafOnly(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	rooms := NewMascotRoomRepository(db)
	rows, err := repo.ListEnabled(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("enabled %d", len(rows))
	}
	for _, row := range rows {
		if row.Key == "sen_hong" || row.Key == "dandelion" || row.Key == "quynh" || row.ImageKey == "" {
			t.Fatalf("row %+v", row)
		}
	}
	var rares int64
	if err := db.Model(&domain.TreeType{}).Where("enabled = ? AND rarity = ?", false, domain.TreeRarityRare).Count(&rares).Error; err != nil {
		t.Fatal(err)
	}
	if rares != 3 {
		t.Fatalf("rares %d", rares)
	}
	owner := treeUser(t, db, "Lan")
	if _, err := rooms.CreateWithTree(context.Background(), owner.ID, "cherry"); !errors.Is(err, ErrTreeTypeUnavailable) {
		t.Fatalf("cherry %v", err)
	}
	if _, err := rooms.CreateWithTree(context.Background(), owner.ID, "dandelion"); !errors.Is(err, ErrTreeTypeUnavailable) {
		t.Fatalf("dandelion %v", err)
	}
	if _, err := rooms.CreateWithTree(context.Background(), owner.ID, "oak"); !errors.Is(err, ErrTreeTypeUnknown) {
		t.Fatalf("oak %v", err)
	}
	view, err := rooms.CreateWithTree(context.Background(), owner.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var stored domain.MascotRoom
	if err := db.Where("code = ?", view.Code).Take(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TreeType != domain.TreeTypeLeaf {
		t.Fatalf("type %s", stored.TreeType)
	}
}

func TestTreeAIDoesNotCountOrWater(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	buddy := treeUser(t, db, "Bé AI")
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	addTreeMember(t, db, code, buddy.ID, vnAt(2026, 11, 19, 11, 0), domain.RoomMemberAI)
	when := vnAt(2026, 11, 20, 10, 0)
	state, err := repo.State(context.Background(), owner.ID, code, when)
	if err != nil || state.Required != 1 {
		t.Fatalf("required %+v %v", state.Required, err)
	}
	treeSkin(t, db, buddy.ID, when)
	if _, err := repo.Water(context.Background(), buddy.ID, code, "", when); !errors.Is(err, ErrTreeNotReal) {
		t.Fatalf("ai water %v", err)
	}
	treeSkin(t, db, owner.ID, when)
	state, err = repo.Water(context.Background(), owner.ID, code, "", when)
	if err != nil || state.Stage != 2 || state.Required != 1 {
		t.Fatalf("real water %+v %v", state, err)
	}
}

func TestTreeLeaveCanCompleteTheDay(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	a := treeUser(t, db, "Mây")
	b := treeUser(t, db, "Na")
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	addTreeMember(t, db, code, a.ID, vnAt(2026, 11, 19, 11, 0), domain.RoomMemberUser)
	addTreeMember(t, db, code, b.ID, vnAt(2026, 11, 19, 12, 0), domain.RoomMemberUser)
	when := vnAt(2026, 11, 20, 10, 0)
	treeSkin(t, db, owner.ID, when)
	treeSkin(t, db, a.ID, when)
	if _, err := repo.Water(context.Background(), owner.ID, code, "a", when); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Water(context.Background(), a.ID, code, "b", when.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	leaveTreeMember(t, db, code, b.ID, vnAt(2026, 11, 20, 11, 0))
	state, err := repo.Sync(context.Background(), code, vnAt(2026, 11, 20, 12, 0))
	if err != nil || state.Stage != 2 || state.Required != 2 || state.Waters != 2 {
		t.Fatalf("level on leave %+v %v", state, err)
	}
}

func TestTreeCreationDayCountsMembersThen(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	founder := treeUser(t, db, "Mây")
	late := treeUser(t, db, "Na")
	buddy := treeUser(t, db, "Bé")
	created := vnAt(2026, 11, 23, 10, 0)
	code := treeRoom(t, db, owner, created)
	addTreeMember(t, db, code, founder.ID, created, domain.RoomMemberUser)
	addTreeMember(t, db, code, buddy.ID, created, domain.RoomMemberAI)
	addTreeMember(t, db, code, late.ID, vnAt(2026, 11, 23, 15, 0), domain.RoomMemberUser)

	var trees int64
	if err := db.Model(&domain.MascotRoomTree{}).Count(&trees).Error; err != nil {
		t.Fatal(err)
	}
	state, err := repo.State(context.Background(), owner.ID, code, vnAt(2026, 11, 23, 16, 0))
	if err != nil {
		t.Fatal(err)
	}
	if state.Required != 2 || state.Stage != 1 {
		t.Fatalf("creation afternoon %+v", state)
	}
	var after int64
	if err := db.Model(&domain.MascotRoomTree{}).Count(&after).Error; err != nil {
		t.Fatal(err)
	}
	if after != trees {
		t.Fatalf("creation-day read wrote a tree row (%d -> %d)", trees, after)
	}

	treeSkin(t, db, owner.ID, created)
	treeSkin(t, db, founder.ID, created)
	if _, err := repo.Water(context.Background(), owner.ID, code, "a", vnAt(2026, 11, 23, 17, 0)); err != nil {
		t.Fatal(err)
	}
	state, err = repo.Water(context.Background(), founder.ID, code, "b", vnAt(2026, 11, 23, 18, 0))
	if err != nil || state.Stage != 2 || state.Required != 2 || state.Waters != 2 {
		t.Fatalf("creation day filled %+v %v", state, err)
	}

	next, err := repo.State(context.Background(), owner.ID, code, vnAt(2026, 11, 24, 8, 0))
	if err != nil || next.Stage != 2 {
		t.Fatalf("next day %+v %v", next, err)
	}
}

func TestTreeHalfPlusHalfLevelsNowAndCarries(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	friend := treeUser(t, db, "Mây")
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	addTreeMember(t, db, code, friend.ID, vnAt(2026, 11, 19, 11, 0), domain.RoomMemberUser)
	day1 := vnAt(2026, 11, 20, 10, 0)
	treeSkin(t, db, owner.ID, day1)
	state, err := repo.Water(context.Background(), owner.ID, code, "d1", day1)
	if err != nil || state.Stage != 1 {
		t.Fatalf("half %+v %v", state, err)
	}
	near(t, state.Progress, 0.5)

	day2 := vnAt(2026, 11, 21, 10, 0)
	treeSkin(t, db, friend.ID, day2)
	state, err = repo.Water(context.Background(), friend.ID, code, "d2", day2)
	if err != nil || state.Stage != 2 || len(state.JustGranted) != 1 {
		t.Fatalf("half+half %+v %v", state, err)
	}
	near(t, state.Progress, 0)

	treeSkin(t, db, owner.ID, day2)
	state, err = repo.Water(context.Background(), owner.ID, code, "d2b", day2.Add(time.Minute))
	if err != nil || state.Stage != 2 {
		t.Fatalf("second water same day %+v %v", state, err)
	}
	near(t, state.Progress, 0.5)
	var rewards int64
	if err := db.Model(&domain.MascotRoomTreeReward{}).Count(&rewards).Error; err != nil {
		t.Fatal(err)
	}
	if rewards != 2 {
		t.Fatalf("stages paid %d, want one stage", rewards)
	}

	day3, err := repo.State(context.Background(), owner.ID, code, vnAt(2026, 11, 22, 0, 30))
	if err != nil || day3.Stage != 2 || len(day3.JustGranted) != 0 {
		t.Fatalf("carry into next day %+v %v", day3, err)
	}
	near(t, day3.Progress, 0.5)
}

func TestTreeRolloverRewardIsUnseenForEachMember(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	owner := treeUser(t, db, "Lan")
	friend := treeUser(t, db, "Mây")
	created := vnAt(2026, 11, 19, 10, 0)
	code := treeRoom(t, db, owner, created)
	addTreeMember(t, db, code, friend.ID, created.Add(time.Hour), domain.RoomMemberUser)
	day := "2026-11-20"
	for _, id := range []uuid.UUID{owner.ID, friend.ID} {
		if err := db.Create(&domain.MascotRoomTreeWater{
			RoomCode: code, UserID: id, VNDate: day, CreatedAt: vnAt(2026, 11, 20, 11, 0),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	opened := vnAt(2026, 11, 21, 8, 0)
	first, err := repo.State(context.Background(), owner.ID, code, opened)
	if err != nil || first.Stage != 2 || len(first.JustGranted) != 1 || len(first.UnseenRewards) != 1 {
		t.Fatalf("first open %+v %v", first, err)
	}
	second, err := repo.State(context.Background(), friend.ID, code, opened.Add(time.Minute))
	if err != nil || second.Stage != 2 || len(second.JustGranted) != 0 || len(second.UnseenRewards) != 1 {
		t.Fatalf("second open %+v %v", second, err)
	}
	if second.UnseenRewards[0].Coins != 20 || second.UnseenRewards[0].Stage != 2 {
		t.Fatalf("unseen %+v", second.UnseenRewards)
	}
	var updated domain.MascotRoomTree
	if err := db.Where("room_code = ?", code).Take(&updated).Error; err != nil {
		t.Fatal(err)
	}
	stamp := updated.UpdatedAt
	again, err := repo.State(context.Background(), friend.ID, code, opened.Add(2*time.Minute))
	if err != nil || len(again.UnseenRewards) != 1 {
		t.Fatalf("reread %+v %v", again, err)
	}
	var after domain.MascotRoomTree
	if err := db.Where("room_code = ?", code).Take(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !after.UpdatedAt.Equal(stamp) || after.SettledThrough != "2026-11-20" {
		t.Fatalf("settled read wrote %+v", after)
	}

	seen, err := repo.MarkRewardsSeen(context.Background(), owner.ID, code, nil, opened.Add(3*time.Minute))
	if err != nil || len(seen.UnseenRewards) != 0 || len(seen.MyRewards) != 1 {
		t.Fatalf("owner seen %+v %v", seen, err)
	}
	still, err := repo.State(context.Background(), friend.ID, code, opened.Add(4*time.Minute))
	if err != nil || len(still.UnseenRewards) != 1 {
		t.Fatalf("friend still unseen %+v %v", still, err)
	}
	seen, err = repo.MarkRewardsSeen(context.Background(), friend.ID, code, []int{2}, opened.Add(5*time.Minute))
	if err != nil || len(seen.UnseenRewards) != 0 {
		t.Fatalf("friend seen %+v %v", seen, err)
	}
	var ledger int64
	if err := db.Model(&domain.UserCoinLedger{}).Where("reason = ?", domain.CoinReasonTreeStage).Count(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	if ledger != 2 {
		t.Fatalf("ledger %d", ledger)
	}
}

func openTreeDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:tree_" + uuid.NewString() + "?mode=memory&cache=shared&_pragma=busy_timeout(5000)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.MascotRoom{},
		&domain.MascotRoomMember{},
		&domain.MascotRoomActiveSeat{},
		&domain.MascotItem{},
		&domain.UserMascotItem{},
		&domain.TreeType{},
		&domain.MascotRoomTree{},
		&domain.MascotRoomTreeWater{},
		&domain.MascotRoomTreeReward{},
		&domain.MascotRoomMemberLeave{},
		&domain.UserCoinWallet{},
		&domain.UserCoinLedger{},
		&domain.SkinCheck{},
	); err != nil {
		t.Fatal(err)
	}
	if err := SeedTreeCatalog(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func treeUser(t *testing.T, db *gorm.DB, name string) *domain.User {
	t.Helper()
	user := &domain.User{
		Email: name + "-" + uuid.NewString() + "@example.com", Username: "t-" + uuid.NewString(),
		DisplayName: name, IsActive: true,
	}
	if err := NewUserRepository(db).Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	return user
}

func treeRoom(t *testing.T, db *gorm.DB, owner *domain.User, joined time.Time) string {
	t.Helper()
	view, err := NewMascotRoomRepository(db).Create(context.Background(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	ends := joined.Add(30 * 24 * time.Hour)
	if err := db.Model(&domain.MascotRoom{}).Where("code = ?", view.Code).Updates(map[string]any{
		"created_at": joined, "ends_at": ends,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.MascotRoomMember{}).Where("room_code = ?", view.Code).Update("joined_at", joined).Error; err != nil {
		t.Fatal(err)
	}
	return view.Code
}

func addTreeMember(t *testing.T, db *gorm.DB, code string, userID uuid.UUID, joined time.Time, kind string) {
	t.Helper()
	if err := db.Create(&domain.MascotRoomMember{
		RoomCode: code, UserID: userID, JoinedAt: joined, Kind: kind,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func leaveTreeMember(t *testing.T, db *gorm.DB, code string, userID uuid.UUID, left time.Time) {
	t.Helper()
	var member domain.MascotRoomMember
	if err := db.Where("room_code = ? AND user_id = ?", code, userID).Take(&member).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.MascotRoomMemberLeave{
		RoomCode: code, UserID: userID, JoinedAt: member.JoinedAt, LeftAt: left, Kind: member.Kind,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&member).Error; err != nil {
		t.Fatal(err)
	}
}

func treeSkin(t *testing.T, db *gorm.DB, userID uuid.UUID, when time.Time) {
	t.Helper()
	if err := db.Create(&domain.SkinCheck{
		UserID: userID, ImageURLs: json.RawMessage(`[]`), CheckDate: streaktime.DateOf(when),
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func vnAt(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, streaktime.Location)
}

func near(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %v want %v", got, want)
	}
}
