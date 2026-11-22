package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestQuestClaimPaysOnceUnderConcurrency(t *testing.T) {
	db := openQuestDB(t)
	owner := questUser(t, db, "Lan")
	repo := NewRoomQuestRepository(db)
	rooms := NewMascotRoomRepository(db)
	view, err := rooms.Create(context.Background(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC) // 10:00 Asia/Ho_Chi_Minh
	end := start.Add(8 * 24 * time.Hour)
	if err := db.Model(&domain.MascotRoom{}).Where("code = ?", view.Code).Updates(map[string]any{
		"created_at": start,
		"ends_at":    end,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.MascotRoomMember{}).Where("room_code = ?", view.Code).Update("joined_at", start).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 6, 5, 0, 0, 0, time.UTC) // 12:00 VN, inside the room
	for i := 0; i < 5; i++ {
		day := time.Date(2026, 11, 1+i, 0, 0, 0, 0, time.UTC)
		if err := db.Create(&domain.SkinCheck{
			UserID:    owner.ID,
			ImageURLs: json.RawMessage(`["https://cdn.example/secret-face.jpg"]`),
			UserNote:  "SECRET_NOTE_LEAK",
			CheckDate: day,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A half-ticked session is not a care day, so it must not add a sixth day.
	if err := db.Create(&domain.RoutineEntry{
		UserID:      owner.ID,
		RoutineDate: time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC),
		Morning:     json.RawMessage(`[{"id":"wash","title":"Rửa mặt","completed":true},{"id":"spf","title":"Chống nắng","completed":false}]`),
	}).Error; err != nil {
		t.Fatal(err)
	}

	const n = 12
	var wg sync.WaitGroup
	type result struct {
		already bool
		balance int64
		coins   int
		err     error
	}
	out := make([]result, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := repo.Claim(context.Background(), owner.ID, view.Code, 5, now)
			out[i] = result{already: got.Already, balance: got.Balance, coins: got.Coins, err: err}
		}(i)
	}
	wg.Wait()
	paid := 0
	for _, row := range out {
		if row.err != nil {
			t.Fatalf("claim: %v", row.err)
		}
		if row.coins != 40 || row.balance != 40 {
			t.Fatalf("coins %d balance %d", row.coins, row.balance)
		}
		if !row.already {
			paid++
		}
	}
	if paid != 1 {
		t.Fatalf("paid %d times", paid)
	}
	var claims int64
	if err := db.Model(&domain.MascotRoomQuestClaim{}).Where("user_id = ?", owner.ID).Count(&claims).Error; err != nil {
		t.Fatal(err)
	}
	if claims != 1 {
		t.Fatalf("claim rows %d", claims)
	}
	var ledger int64
	if err := db.Model(&domain.UserCoinLedger{}).Where("user_id = ?", owner.ID).Count(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	if ledger != 1 {
		t.Fatalf("ledger rows %d", ledger)
	}

	again, err := repo.Claim(context.Background(), owner.ID, view.Code, 5, now)
	if err != nil || !again.Already || again.Balance != 40 {
		t.Fatalf("repeat %+v %v", again, err)
	}
}

func TestNudgeOncePerVietnamDay(t *testing.T) {
	db := openQuestDB(t)
	owner := questUser(t, db, "Lan")
	guest := questUser(t, db, "Mây")
	repo := NewRoomQuestRepository(db)
	rooms := NewMascotRoomRepository(db)
	view, err := rooms.Create(context.Background(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rooms.Join(context.Background(), guest.ID, view.Code); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := repo.SaveNudge(context.Background(), owner.ID, view.Code, owner.ID, "nope", now); err != ErrNudgeSelf {
		t.Fatalf("self %v", err)
	}
	if _, err := repo.SaveNudge(context.Background(), owner.ID, view.Code, guest.ID, "Lan đang chờ bạn chăm da hôm nay đó 💕", now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveNudge(context.Background(), owner.ID, view.Code, guest.ID, "second", now); err != ErrNudgeAlready {
		t.Fatalf("second %v", err)
	}
	if err := db.Create(&domain.RoutineEntry{
		UserID:      guest.ID,
		RoutineDate: streaktime.DateOf(now),
		Morning:     json.RawMessage(`[{"id":"wash","title":"Rửa mặt","completed":true}]`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	be := questUser(t, db, "Be")
	if _, err := rooms.Join(context.Background(), be.ID, view.Code); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveNudge(context.Background(), be.ID, view.Code, guest.ID, "late", now); err != ErrNudgeDone {
		t.Fatalf("done today %v", err)
	}
	other := questUser(t, db, "An")
	// other is not in the room
	if _, err := repo.SaveNudge(context.Background(), owner.ID, view.Code, other.ID, "x", now); err != ErrNudgeTarget {
		t.Fatalf("stranger %v", err)
	}
}

func TestCareDayUsesVietnamDate(t *testing.T) {
	db := openQuestDB(t)
	owner := questUser(t, db, "Lan")
	repo := NewRoomQuestRepository(db)
	rooms := NewMascotRoomRepository(db)
	view, err := rooms.Create(context.Background(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	end := start.Add(8 * 24 * time.Hour)
	if err := db.Model(&domain.MascotRoom{}).Where("code = ?", view.Code).Updates(map[string]any{
		"created_at": start, "ends_at": end,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.MascotRoomMember{}).Where("user_id = ?", owner.ID).Update("joined_at", start).Error; err != nil {
		t.Fatal(err)
	}
	lateUTC := time.Date(2026, 11, 2, 16, 30, 0, 0, time.UTC)   // still 2 Nov in Vietnam
	earlyNext := time.Date(2026, 11, 2, 17, 30, 0, 0, time.UTC) // 3 Nov 00:30 in Vietnam
	if err := db.Create(&domain.RoutineEntry{
		UserID:      owner.ID,
		RoutineDate: streaktime.DateOf(lateUTC),
		Morning:     json.RawMessage(`[{"id":"wash","title":"Rửa mặt","completed":true}]`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.SkinCheck{
		UserID:    owner.ID,
		ImageURLs: json.RawMessage(`["https://cdn.example/secret-face.jpg"]`),
		UserNote:  "SECRET_NOTE_LEAK",
		CheckDate: streaktime.DateOf(earlyNext),
	}).Error; err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.Load(context.Background(), owner.ID, view.Code, earlyNext)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Cared[owner.ID]) != 1 || len(loaded.Checked[owner.ID]) != 1 {
		t.Fatalf("days cared %d checked %d", len(loaded.Cared[owner.ID]), len(loaded.Checked[owner.ID]))
	}
	if streaktime.DateOf(loaded.Cared[owner.ID][0]).Format("2006-01-02") != "2026-11-02" {
		t.Fatalf("cared day %s", loaded.Cared[owner.ID][0])
	}
	if streaktime.DateOf(loaded.Checked[owner.ID][0]).Format("2006-01-02") != "2026-11-03" {
		t.Fatalf("checked day %s", loaded.Checked[owner.ID][0])
	}
}

func TestClaimRequiresOwnDay(t *testing.T) {
	db := openQuestDB(t)
	owner := questUser(t, db, "Lan")
	late := questUser(t, db, "Mây")
	repo := NewRoomQuestRepository(db)
	rooms := NewMascotRoomRepository(db)
	view, err := rooms.Create(context.Background(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	end := start.Add(8 * 24 * time.Hour)
	if err := db.Model(&domain.MascotRoom{}).Where("code = ?", view.Code).Updates(map[string]any{
		"created_at": start, "ends_at": end,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.MascotRoomMember{}).Where("user_id = ?", owner.ID).Update("joined_at", start).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := db.Create(&domain.SkinCheck{
			UserID: owner.ID, ImageURLs: json.RawMessage(`[]`),
			CheckDate: time.Date(2026, 11, 1+i, 0, 0, 0, 0, time.UTC),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&domain.MascotRoomMember{
		RoomCode: view.Code,
		UserID:   late.ID,
		JoinedAt: time.Date(2026, 11, 6, 5, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 6, 6, 0, 0, 0, time.UTC)
	if _, err := repo.Claim(context.Background(), late.ID, view.Code, 5, now); !errors.Is(err, ErrQuestChestClosed) {
		t.Fatalf("late joiner %v", err)
	}
	got, err := repo.Claim(context.Background(), owner.ID, view.Code, 5, now)
	if err != nil || got.Already || got.Balance != 40 {
		t.Fatalf("owner %+v %v", got, err)
	}
	var room domain.MascotRoom
	if err := db.Where("code = ?", view.Code).Take(&room).Error; err != nil {
		t.Fatal(err)
	}
	var ledger domain.UserCoinLedger
	if err := db.Where("user_id = ?", owner.ID).Take(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf(":%d", room.CreatedAt.UTC().UnixNano())
	if !strings.HasSuffix(ledger.ID, suffix) || !strings.Contains(ledger.ID, view.Code) {
		t.Fatalf("ledger id %s, want suffix %s", ledger.ID, suffix)
	}
}

func TestClaimGraceAfterRoomEnds(t *testing.T) {
	db := openQuestDB(t)
	owner := questUser(t, db, "Lan")
	repo := NewRoomQuestRepository(db)
	rooms := NewMascotRoomRepository(db)
	view, err := rooms.Create(context.Background(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	end := start.Add(8 * 24 * time.Hour)
	if err := db.Model(&domain.MascotRoom{}).Where("code = ?", view.Code).Updates(map[string]any{
		"created_at": start, "ends_at": end,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.MascotRoomMember{}).Where("user_id = ?", owner.ID).Update("joined_at", start).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := db.Create(&domain.SkinCheck{
			UserID: owner.ID, ImageURLs: json.RawMessage(`[]`),
			CheckDate: time.Date(2026, 11, 1+i, 0, 0, 0, 0, time.UTC),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	inside := end.Add(3 * 24 * time.Hour)
	if _, err := repo.Load(context.Background(), owner.ID, view.Code, inside); !errors.Is(err, ErrMascotRoomEnded) {
		t.Fatalf("board after end %v", err)
	}
	got, err := repo.Claim(context.Background(), owner.ID, view.Code, 5, inside)
	if err != nil || got.Already || got.Coins != 40 {
		t.Fatalf("grace claim %+v %v", got, err)
	}
	past := inside.Add(time.Second)
	if _, err := repo.Claim(context.Background(), owner.ID, view.Code, 15, past); !errors.Is(err, ErrQuestChestsExpired) || err.Error() != "Phòng đã kết thúc, rương hết hạn" {
		t.Fatalf("expired %v", err)
	}
}

func openQuestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:quest_" + uuid.NewString() + "?mode=memory&cache=shared&_pragma=busy_timeout(5000)"
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
		&domain.MascotRoomQuestClaim{},
		&domain.MascotRoomNudge{},
		&domain.UserCoinWallet{},
		&domain.UserCoinLedger{},
		&domain.RoutineEntry{},
		&domain.SkinCheck{},
		&domain.UserMascot{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func questUser(t *testing.T, db *gorm.DB, name string) *domain.User {
	t.Helper()
	user := &domain.User{
		Email:       name + "-" + uuid.NewString() + "@example.com",
		Username:    "q-" + uuid.NewString(),
		DisplayName: name,
		IsActive:    true,
	}
	if err := NewUserRepository(db).Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	return user
}
