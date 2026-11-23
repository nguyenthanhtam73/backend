package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGiftBoxMigrationIsData(t *testing.T) {
	upBytes, err := os.ReadFile("../../migrations/043_tree_gift_boxes.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	if strings.Contains(strings.ToLower(up), "gacha") {
		t.Fatal("migration must not say gacha")
	}
	if strings.Contains(strings.ToUpper(up), "CHECK (") {
		t.Fatal("gift box migration must not use a CHECK constraint")
	}
	for _, piece := range []string{
		"tree_gift_boxes", "UNIQUE (room_code, user_id)", "hair_acc", "top_la_non", "acc_vuong_mien_la_ngoc",
	} {
		if !strings.Contains(up, piece) {
			t.Fatalf("up migration missing %s", piece)
		}
	}
	down, err := os.ReadFile("../../migrations/043_tree_gift_boxes.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(down), "DROP TABLE IF EXISTS tree_gift_boxes") {
		t.Fatal("down migration")
	}
}

func TestLeafBloomBoxAndCherryStillPays(t *testing.T) {
	db := openTreeDB(t)
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
	if err != nil || !state.CanDraw || state.Balance != 0 {
		t.Fatalf("leaf bloom %+v %v", state, err)
	}
	var ownerBoxes, buddyBoxes int64
	if err := db.Model(&domain.TreeGiftBox{}).Where("user_id = ?", owner.ID).Count(&ownerBoxes).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.TreeGiftBox{}).Where("user_id = ?", buddy.ID).Count(&buddyBoxes).Error; err != nil {
		t.Fatal(err)
	}
	if ownerBoxes != 1 || buddyBoxes != 0 {
		t.Fatalf("boxes owner %d buddy %d", ownerBoxes, buddyBoxes)
	}
	var clip int64
	if err := db.Model(&domain.UserMascotItem{}).Where("user_id = ? AND item_id = ?", owner.ID, domain.TreeItemClipLeaf).Count(&clip).Error; err != nil {
		t.Fatal(err)
	}
	if clip != 0 {
		t.Fatal("leaf bloom granted the clip")
	}
	pick := 1
	if _, err := repo.DrawShard(context.Background(), owner.ID, code, "flip", &pick, when); err != nil {
		t.Fatal(err)
	}

	cherryOwner := treeUser(t, db, "Dao")
	cherry := treeRoom(t, db, cherryOwner, vnAt(2026, 11, 19, 10, 0))
	if err := db.Model(&domain.MascotRoom{}).Where("code = ?", cherry).Update("tree_type", "cherry").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.State(context.Background(), cherryOwner.ID, cherry, vnAt(2026, 11, 20, 8, 0)); err != nil {
		t.Fatal(err)
	}
	if err := forceStage(t, db, cherry, 6, "2026-11-19"); err != nil {
		t.Fatal(err)
	}
	treeSkin(t, db, cherryOwner.ID, when)
	cherryState, err := repo.Water(context.Background(), cherryOwner.ID, cherry, "bloom", when)
	if err != nil || cherryState.Balance != 150 || cherryState.TreeType != "cherry" {
		t.Fatalf("cherry %+v %v", cherryState, err)
	}
	var cherryClip int64
	if err := db.Model(&domain.UserMascotItem{}).Where("user_id = ? AND item_id = ?", cherryOwner.ID, domain.TreeItemClipCherry).Count(&cherryClip).Error; err != nil {
		t.Fatal(err)
	}
	if cherryClip != 1 {
		t.Fatalf("cherry clip %d", cherryClip)
	}
	var cherryBoxes int64
	if err := db.Model(&domain.TreeGiftBox{}).Where("room_code = ?", cherry).Count(&cherryBoxes).Error; err != nil {
		t.Fatal(err)
	}
	if cherryBoxes != 0 {
		t.Fatalf("cherry boxes %d", cherryBoxes)
	}
}

func TestOpenGiftBoxIsIdempotent(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	restore := SetGiftRoll(func() (int, error) { return 0, nil })
	t.Cleanup(func() { SetGiftRoll(restore) })
	owner := treeUser(t, db, "Lan")
	other := treeUser(t, db, "Mây")
	box := bloomOneBox(t, db, repo, owner)
	when := vnAt(2026, 11, 20, 11, 0)
	opened, err := repo.OpenGiftBox(context.Background(), owner.ID, box.ID, when)
	if err != nil || opened.Already || opened.Duplicate || opened.Partial || opened.Prize.ID != "set_la_non" || len(opened.Granted) != 2 || opened.DupeCoins != 0 {
		t.Fatalf("open %+v %v", opened, err)
	}
	var grants int64
	if err := db.Model(&domain.UserMascotItem{}).Where("user_id = ? AND source = ?", owner.ID, domain.MascotSourceGiftBox).Count(&grants).Error; err != nil {
		t.Fatal(err)
	}
	if grants != 2 {
		t.Fatalf("grants %d", grants)
	}
	again, err := repo.OpenGiftBox(context.Background(), owner.ID, box.ID, when.Add(time.Minute))
	if err != nil || !again.Already || again.Prize.ID != "set_la_non" || again.DupeCoins != 0 {
		t.Fatalf("again %+v %v", again, err)
	}
	if err := db.Model(&domain.UserMascotItem{}).Where("user_id = ? AND source = ?", owner.ID, domain.MascotSourceGiftBox).Count(&grants).Error; err != nil {
		t.Fatal(err)
	}
	if grants != 2 {
		t.Fatalf("grants after second open %d", grants)
	}
	if _, err := repo.OpenGiftBox(context.Background(), other.ID, box.ID, when); !errors.Is(err, ErrGiftBoxNotFound) {
		t.Fatalf("other user %v", err)
	}
	rows, err := repo.ListUnopenedGiftBoxes(context.Background(), owner.ID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("unopened %+v %v", rows, err)
	}
}

func TestOpenGiftBoxDuplicateAndPartial(t *testing.T) {
	db := openTreeDB(t)
	repo := NewRoomTreeRepository(db)
	def, _ := domain.GiftBoxByTree(domain.TreeTypeLeaf)
	prizes, err := domain.WeightedPrizes(def)
	if err != nil {
		t.Fatal(err)
	}
	clipAt, ok := domain.GiftRollStart(prizes, domain.GiftPrizeClipLeaf)
	if !ok {
		t.Fatal("clip prize")
	}
	when := vnAt(2026, 11, 20, 11, 0)

	t.Run("partial set grants the missing piece", func(t *testing.T) {
		restore := SetGiftRoll(func() (int, error) { return 0, nil })
		t.Cleanup(func() { SetGiftRoll(restore) })
		owner := treeUser(t, db, "Lan")
		grantItem(t, db, owner.ID, "top_la_non", domain.MascotLayerTop)
		box := bloomOneBox(t, db, repo, owner)
		opened, err := repo.OpenGiftBox(context.Background(), owner.ID, box.ID, when)
		if err != nil || opened.Duplicate || !opened.Partial || opened.DupeCoins != 0 || len(opened.Granted) != 1 || opened.Granted[0].ID != "bottom_quan_short_kem" {
			t.Fatalf("partial %+v %v", opened, err)
		}
		if opened.Balance != 0 {
			t.Fatalf("balance %d", opened.Balance)
		}
	})

	t.Run("owned set pays xu", func(t *testing.T) {
		restore := SetGiftRoll(func() (int, error) { return 0, nil })
		t.Cleanup(func() { SetGiftRoll(restore) })
		owner := treeUser(t, db, "Na")
		grantItem(t, db, owner.ID, "top_la_non", domain.MascotLayerTop)
		grantItem(t, db, owner.ID, "bottom_quan_short_kem", domain.MascotLayerBottom)
		box := bloomOneBox(t, db, repo, owner)
		opened, err := repo.OpenGiftBox(context.Background(), owner.ID, box.ID, when)
		if err != nil || !opened.Duplicate || opened.Partial || opened.DupeCoins != 8 || len(opened.Granted) != 0 || opened.Balance != 8 {
			t.Fatalf("dupe %+v %v", opened, err)
		}
		again, err := repo.OpenGiftBox(context.Background(), owner.ID, box.ID, when.Add(time.Minute))
		if err != nil || !again.Already || again.Balance != 8 || again.DupeCoins != 8 {
			t.Fatalf("replay %+v %v", again, err)
		}
	})

	t.Run("owned leaf clip pays the common amount", func(t *testing.T) {
		restore := SetGiftRoll(func() (int, error) { return clipAt, nil })
		t.Cleanup(func() { SetGiftRoll(restore) })
		owner := treeUser(t, db, "Hoa")
		if err := db.Omit("User", "Item").Create(&domain.UserMascotItem{
			UserID: owner.ID, ItemID: domain.TreeItemClipLeaf, Source: domain.MascotSourceRoomReward,
			AcquiredAt: when,
		}).Error; err != nil {
			t.Fatal(err)
		}
		box := bloomOneBox(t, db, repo, owner)
		opened, err := repo.OpenGiftBox(context.Background(), owner.ID, box.ID, when)
		if err != nil || !opened.Duplicate || opened.DupeCoins != 8 || opened.Prize.ID != domain.GiftPrizeClipLeaf {
			t.Fatalf("clip %+v %v", opened, err)
		}
		var row domain.UserMascotItem
		if err := db.Where("user_id = ? AND item_id = ?", owner.ID, domain.TreeItemClipLeaf).Take(&row).Error; err != nil {
			t.Fatal(err)
		}
		if row.Source != domain.MascotSourceRoomReward {
			t.Fatalf("existing owner source %s", row.Source)
		}
	})
}

func TestLeafClipSlotMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:clip_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&domain.User{}, &domain.MascotItem{}, &domain.UserMascot{}, &domain.UserMascotItem{}, &domain.SeedBagConfig{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.MascotItem{ID: domain.TreeItemClipLeaf, Layer: domain.MascotLayerAccessory, Name: "Kẹp tóc lá xanh"}).Error; err != nil {
		t.Fatal(err)
	}
	user := &domain.User{Email: uuid.NewString() + "@example.com", Username: "c-" + uuid.NewString(), DisplayName: "Lan", IsActive: true}
	if err := NewUserRepository(db).Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(domain.MascotLoadout{
		Body: domain.MascotItemBodyChibi, Hair: domain.MascotItemHairSleepBuns,
		Outfit: domain.MascotItemOutfitCozyPajamas, Accessory: domain.TreeItemClipLeaf,
		Accessories: []string{domain.TreeItemClipLeaf, domain.MascotItemAccessoryStarClip},
	})
	if err := db.Create(&domain.UserMascot{UserID: user.ID, Loadout: raw}).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateLeafClipSlot(db); err != nil {
		t.Fatal(err)
	}
	var item domain.MascotItem
	if err := db.Where("id = ?", domain.TreeItemClipLeaf).Take(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.Layer != domain.MascotLayerHairAcc {
		t.Fatalf("layer %s", item.Layer)
	}
	var row domain.UserMascot
	if err := db.Where("user_id = ?", user.ID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	var look domain.MascotLoadout
	if err := json.Unmarshal(row.Loadout, &look); err != nil {
		t.Fatal(err)
	}
	if look.HairAcc != domain.TreeItemClipLeaf || look.Accessory == domain.TreeItemClipLeaf {
		t.Fatalf("loadout %+v", look)
	}
	for _, id := range look.Accessories {
		if id == domain.TreeItemClipLeaf {
			t.Fatalf("clip still in accessories %+v", look.Accessories)
		}
	}
	if err := db.Model(&domain.MascotItem{}).Where("id = ?", domain.TreeItemClipLeaf).Update("layer", domain.MascotLayerAccessory).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateLeafClipSlot(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", domain.TreeItemClipLeaf).Take(&item).Error; err != nil {
		t.Fatal(err)
	}
	if item.Layer != domain.MascotLayerAccessory {
		t.Fatal("second boot rewrote the layer")
	}
}

func bloomOneBox(t *testing.T, db *gorm.DB, repo *RoomTreeRepository, owner *domain.User) domain.TreeGiftBox {
	t.Helper()
	code := treeRoom(t, db, owner, vnAt(2026, 11, 19, 10, 0))
	if _, err := repo.State(context.Background(), owner.ID, code, vnAt(2026, 11, 20, 8, 0)); err != nil {
		t.Fatal(err)
	}
	if err := forceStage(t, db, code, 6, "2026-11-19"); err != nil {
		t.Fatal(err)
	}
	when := vnAt(2026, 11, 20, 10, 0)
	treeSkin(t, db, owner.ID, when)
	if _, err := repo.Water(context.Background(), owner.ID, code, "bloom", when); err != nil {
		t.Fatal(err)
	}
	var box domain.TreeGiftBox
	if err := db.Where("user_id = ? AND room_code = ?", owner.ID, code).Take(&box).Error; err != nil {
		t.Fatal(err)
	}
	return box
}

func grantItem(t *testing.T, db *gorm.DB, userID uuid.UUID, id, layer string) {
	t.Helper()
	var n int64
	if err := db.Model(&domain.MascotItem{}).Where("id = ?", id).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		if err := db.Create(&domain.MascotItem{ID: id, Layer: layer, Name: id}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Omit("User", "Item").Create(&domain.UserMascotItem{
		UserID: userID, ItemID: id, Source: domain.MascotSourceRoomReward, AcquiredAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
}
