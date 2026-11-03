package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/lobby"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMascotRoomMigrationShape(t *testing.T) {
	upBytes, err := os.ReadFile("../../migrations/032_mascot_rooms.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	downBytes, err := os.ReadFile("../../migrations/032_mascot_rooms.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	down := string(downBytes)
	for _, piece := range []string{
		"mascot_rooms", "mascot_room_members", "owner_user_id", "created_at",
		"max_members", "room_code", "joined_at", "ABCDEFGHJKLMNPQRSTUVWXYZ23456789",
	} {
		if !strings.Contains(up, piece) {
			t.Fatalf("up migration missing %s", piece)
		}
	}
	if strings.Contains(up, "0O1I") {
		t.Fatal("migration documents the ambiguous characters as allowed")
	}
	if !strings.Contains(down, "mascot_room_members") || !strings.Contains(down, "mascot_rooms") {
		t.Fatal("down migration")
	}
	if _, ok := lobby.NormalizeRoomCode("AB23CD"); !ok {
		t.Fatal("sample code")
	}
}

func TestMascotRoomCapJoinLeaveAndDelete(t *testing.T) {
	db := openRoomDB(t)
	repo := NewMascotRoomRepository(db)
	ctx := context.Background()
	owner := insertRoomUser(t, db, "owner@example.com")
	view, err := repo.Create(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.MemberCount != 1 || !view.Owner || !view.Joined || view.Cap != lobby.RoomCap {
		t.Fatalf("create %+v", view)
	}
	if _, ok := lobby.NormalizeRoomCode(view.Code); !ok {
		t.Fatalf("code %s", view.Code)
	}
	got, err := repo.Get(ctx, owner.ID, strings.ToLower(view.Code))
	if err != nil || got.Code != view.Code || got.MemberCount != 1 {
		t.Fatalf("get %+v %v", got, err)
	}
	if _, err := repo.Join(ctx, owner.ID, view.Code); err != nil {
		t.Fatal(err)
	}
	again, err := repo.Get(ctx, owner.ID, view.Code)
	if err != nil || again.MemberCount != 1 {
		t.Fatalf("second join grew the room %+v %v", again, err)
	}

	var others []uuid.UUID
	for i := 0; i < lobby.RoomCap-1; i++ {
		u := insertRoomUser(t, db, "m"+uuid.NewString()+"@example.com")
		others = append(others, u.ID)
		joined, err := repo.Join(ctx, u.ID, view.Code)
		if err != nil {
			t.Fatal(err)
		}
		if joined.MemberCount != i+2 {
			t.Fatalf("count %d", joined.MemberCount)
		}
	}
	extra := insertRoomUser(t, db, "extra@example.com")
	if _, err := repo.Join(ctx, extra.ID, view.Code); !errors.Is(err, ErrMascotRoomFull) {
		t.Fatalf("9th join %v", err)
	}
	if _, err := repo.Get(ctx, extra.ID, "OOOOOO"); !errors.Is(err, ErrMascotRoomBadCode) {
		t.Fatalf("bad code %v", err)
	}
	if err := repo.CanEnter(ctx, extra.ID, view.Code); !errors.Is(err, ErrMascotRoomNotMember) {
		t.Fatalf("can enter %v", err)
	}
	if err := repo.CanEnter(ctx, owner.ID, view.Code); err != nil {
		t.Fatal(err)
	}

	guest := others[0]
	if err := repo.Leave(ctx, guest, view.Code); err != nil {
		t.Fatal(err)
	}
	after, err := repo.Get(ctx, owner.ID, view.Code)
	if err != nil || after.MemberCount != lobby.RoomCap-1 {
		t.Fatalf("after leave %+v %v", after, err)
	}
	if _, err := repo.Join(ctx, extra.ID, view.Code); err != nil {
		t.Fatalf("join after a seat opened: %v", err)
	}

	if err := repo.Leave(ctx, owner.ID, view.Code); err != nil {
		t.Fatal(err)
	}
	for _, id := range append(others[1:], extra.ID) {
		if err := repo.Leave(ctx, id, view.Code); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.Get(ctx, owner.ID, view.Code); !errors.Is(err, ErrMascotRoomNotFound) {
		t.Fatalf("empty room remained: %v", err)
	}

	owned, err := repo.Create(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	friend := insertRoomUser(t, db, "friend@example.com")
	if _, err := repo.Join(ctx, friend.ID, owned.Code); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return deleteOwnedMascotRooms(tx, owner.ID)
	}); err != nil {
		t.Fatal(err)
	}
	var rooms, members int64
	if err := db.Model(&domain.MascotRoom{}).Where("code = ?", owned.Code).Count(&rooms).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.MascotRoomMember{}).Where("room_code = ?", owned.Code).Count(&members).Error; err != nil {
		t.Fatal(err)
	}
	if rooms != 0 || members != 0 {
		t.Fatalf("account delete left rooms=%d members=%d", rooms, members)
	}
}

func openRoomDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:mascot_room_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.MascotRoom{}, &domain.MascotRoomMember{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func insertRoomUser(t *testing.T, db *gorm.DB, email string) *domain.User {
	t.Helper()
	u := &domain.User{
		Email:    email,
		Username: "room-" + uuid.NewString(),
		IsActive: true,
	}
	if err := NewUserRepository(db).Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}
