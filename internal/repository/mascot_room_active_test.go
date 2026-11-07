package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMascotRoomTermMigrationKeepsRows(t *testing.T) {
	upBytes, err := os.ReadFile("../../migrations/038_mascot_room_one_active.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	downBytes, err := os.ReadFile("../../migrations/038_mascot_room_one_active.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	down := string(downBytes)
	const cappedLegacyEnd = "LEAST(created_at + INTERVAL '21 days', NOW() + INTERVAL '8 days')"
	for _, piece := range []string{
		"ends_at", cappedLegacyEnd, "mascot_room_active_seats",
		"HAVING COUNT(*) = 1", "1971-01-01",
	} {
		if !strings.Contains(up, piece) {
			t.Fatalf("up migration missing %s", piece)
		}
	}
	if strings.Contains(up, "SET ends_at = created_at + INTERVAL '21 days'") {
		t.Fatal("migration still assigns a bare 21-day end")
	}
	prep, err := os.ReadFile("mascot_room_active.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prep), cappedLegacyEnd) {
		t.Fatal("prepareMascotRoomEnds missing the capped 21-day end")
	}
	if strings.Contains(strings.ToUpper(up), "DELETE FROM") {
		t.Fatal("up migration deletes rows")
	}
	if strings.Contains(strings.ToUpper(down), "DELETE FROM") {
		t.Fatal("down migration deletes rows")
	}
	if !strings.Contains(down, "mascot_room_active_seats") || !strings.Contains(down, "ends_at") {
		t.Fatal("down migration")
	}
}

func TestBackfillCapsExistingRoomEnds(t *testing.T) {
	db := openRoomDB(t)
	owner := insertRoomUser(t, db, "ends-owner@example.com")
	endedUser := insertRoomUser(t, db, "ends-ended@example.com")
	now := time.Now().UTC().Truncate(time.Second)
	cap := now.Add(domain.MascotRoomDuration)
	created := now.Add(-10 * 24 * time.Hour)
	unset := insertFixedRoom(t, db, "AAAAAA", owner.ID, created, time.Unix(0, 0).UTC())
	longer := insertFixedRoom(t, db, "BBBBBB", owner.ID, created, created.Add(40*24*time.Hour))
	alreadyEight := insertFixedRoom(t, db, "CCCCCC", owner.ID, created, created.Add(domain.MascotRoomDuration))
	legacy := insertFixedRoom(t, db, "DDDDDD", owner.ID, created, created.Add(domain.LegacyMascotRoomDuration))
	// created_at + 21 days is 3 days from now, sooner than the 8-day cap.
	soonCreated := now.Add(-18 * 24 * time.Hour)
	soonEnd := soonCreated.Add(domain.LegacyMascotRoomDuration)
	soon := insertFixedRoom(t, db, "EEEEEE", owner.ID, soonCreated, soonEnd)
	// created_at + 21 days is already past. The cap must not reopen it.
	overCreated := now.Add(-30 * 24 * time.Hour)
	overEnd := overCreated.Add(domain.LegacyMascotRoomDuration)
	over := insertFixedRoom(t, db, "FFFFFF", endedUser.ID, overCreated, overEnd)
	overUnset := insertFixedRoom(t, db, "GGGGGG", endedUser.ID, overCreated, time.Unix(0, 0).UTC())
	fresh := insertFixedRoom(t, db, "HHHHHH", owner.ID, now, cap)

	n, err := backfillLegacyRoomEnds(db, now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("backfill changed %d rows", n)
	}
	got := roomEnds(t, db)
	if !got[unset].Equal(cap) {
		t.Fatalf("unset end %s, want cap %s", got[unset], cap)
	}
	if !got[longer].Equal(cap) || !got[legacy].Equal(cap) {
		t.Fatalf("long ends longer=%s legacy=%s cap=%s", got[longer], got[legacy], cap)
	}
	if !got[alreadyEight].Equal(created.Add(domain.MascotRoomDuration)) {
		t.Fatalf("ended 8-day room reopened to %s", got[alreadyEight])
	}
	if !got[soon].Equal(soonEnd) {
		t.Fatalf("sooner end extended to %s", got[soon])
	}
	if !got[over].Equal(overEnd) || !overEnd.Before(now) {
		t.Fatalf("past end moved to %s", got[over])
	}
	if !got[overUnset].Equal(overEnd) || !got[overUnset].Before(now) {
		t.Fatalf("unset past room reopened to %s", got[overUnset])
	}
	if !got[fresh].Equal(cap) {
		t.Fatalf("new 8-day room moved to %s", got[fresh])
	}
	again, err := backfillLegacyRoomEnds(db, now.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if again != 0 {
		t.Fatalf("second backfill moved %d rows", again)
	}
	if gotAgain := roomEnds(t, db); !gotAgain[unset].Equal(cap) || !gotAgain[over].Equal(overEnd) {
		t.Fatalf("second pass %+v", gotAgain)
	}

	// The capped room is still active, so a second room is refused.
	if err := db.Create(&domain.MascotRoomMember{RoomCode: unset, UserID: owner.ID, JoinedAt: created}).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewMascotRoomRepository(db)
	if _, err := repo.Create(context.Background(), owner.ID); !errors.As(err, new(*AlreadyInRoomError)) {
		t.Fatalf("capped room was treated as over: %v", err)
	}
	// A room already past created_at+21 stays ended, so this account can open one.
	if err := db.Create(&domain.MascotRoomMember{RoomCode: over, UserID: endedUser.ID, JoinedAt: overCreated}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(context.Background(), endedUser.ID); err != nil {
		t.Fatalf("ended room still blocked create: %v", err)
	}
	var kept domain.MascotRoom
	if err := db.Where("code = ?", over).First(&kept).Error; err != nil {
		t.Fatal(err)
	}
	if !kept.EndsAt.UTC().Truncate(time.Second).Equal(overEnd) {
		t.Fatalf("ended room end changed to %s", kept.EndsAt)
	}
}

func TestLegacyMultiRoomIsKeptAndBlocksUntilNone(t *testing.T) {
	db := openRoomDB(t)
	repo := NewMascotRoomRepository(db)
	ctx := context.Background()
	user := insertRoomUser(t, db, "multi@example.com")
	hostLate := insertRoomUser(t, db, "multi-late@example.com")
	hostSoon := insertRoomUser(t, db, "multi-soon@example.com")
	now := time.Now().UTC().Truncate(time.Second)
	late := insertFixedRoom(t, db, "EEEEEE", hostLate.ID, now.Add(-48*time.Hour), now.Add(10*24*time.Hour))
	soon := insertFixedRoom(t, db, "FFFFFF", hostSoon.ID, now.Add(-48*time.Hour), now.Add(3*24*time.Hour))
	if err := db.Create(&domain.MascotRoomMember{RoomCode: late, UserID: user.ID, JoinedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.MascotRoomMember{RoomCode: late, UserID: hostLate.ID, JoinedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.MascotRoomMember{RoomCode: soon, UserID: user.ID, JoinedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.MascotRoomMember{RoomCode: soon, UserID: hostSoon.ID, JoinedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	single := insertRoomUser(t, db, "single@example.com")
	only := insertFixedRoom(t, db, "GGGGGG", single.ID, now, now.Add(domain.MascotRoomDuration))
	if err := db.Create(&domain.MascotRoomMember{RoomCode: only, UserID: single.ID, JoinedAt: now}).Error; err != nil {
		t.Fatal(err)
	}

	roomsBefore, membersBefore := roomRowCounts(t, db)
	if err := backfillSingleActiveSeats(db, now); err != nil {
		t.Fatal(err)
	}
	n, err := CountUsersInMultipleActiveRooms(db, now)
	if err != nil {
		t.Fatal(err)
	}
	roomsAfter, membersAfter := roomRowCounts(t, db)
	if n != 1 || roomsAfter != roomsBefore || membersAfter != membersBefore {
		t.Fatalf("count=%d rooms %d->%d members %d->%d", n, roomsBefore, roomsAfter, membersBefore, membersAfter)
	}
	var multiSeats, singleSeats int64
	if err := db.Model(&domain.MascotRoomActiveSeat{}).Where("user_id = ?", user.ID).Count(&multiSeats).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.MascotRoomActiveSeat{}).Where("user_id = ?", single.ID).Count(&singleSeats).Error; err != nil {
		t.Fatal(err)
	}
	if multiSeats != 0 || singleSeats != 1 {
		t.Fatalf("seats multi=%d single=%d", multiSeats, singleSeats)
	}

	maker := insertRoomUser(t, db, "multi-maker@example.com")
	third, err := repo.Create(ctx, maker.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Join(ctx, user.ID, third.Code)
	var taken *AlreadyInRoomError
	if !errors.As(err, &taken) {
		t.Fatalf("join %v", err)
	}
	if taken.Code != late || taken.Count != 2 || !taken.EndsAt.UTC().Truncate(time.Second).Equal(now.Add(10*24*time.Hour)) {
		t.Fatalf("blocking room %+v", taken)
	}
	var still int64
	if err := db.Model(&domain.MascotRoomMember{}).Where("user_id = ? AND room_code IN ?", user.ID, []string{late, soon}).Count(&still).Error; err != nil {
		t.Fatal(err)
	}
	if still != 2 {
		t.Fatalf("memberships after a refused join: %d", still)
	}

	if err := repo.Leave(ctx, user.ID, soon); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Join(ctx, user.ID, third.Code); !errors.As(err, new(*AlreadyInRoomError)) {
		t.Fatalf("one active room still blocks: %v", err)
	}
	if err := repo.Leave(ctx, user.ID, late); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Join(ctx, user.ID, third.Code); err != nil {
		t.Fatalf("join after zero active rooms: %v", err)
	}
	var lateRoom domain.MascotRoom
	if err := db.Where("code = ?", late).First(&lateRoom).Error; err != nil {
		t.Fatalf("leaving did not have to delete the room the host still occupies: %v", err)
	}
}

func TestEndedRoomAllowsANewOneAndKeepsTheOldRow(t *testing.T) {
	db := openRoomDB(t)
	repo := NewMascotRoomRepository(db)
	ctx := context.Background()
	owner := insertRoomUser(t, db, "ended@example.com")
	view, err := repo.Create(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	if err := db.Model(&domain.MascotRoom{}).Where("code = ?", view.Code).Update("ends_at", past).Error; err != nil {
		t.Fatal(err)
	}
	open, err := repo.ListOpenForUser(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("ended room still listed %+v", open)
	}
	got, err := repo.Get(ctx, owner.ID, view.Code)
	if err != nil || !got.EndsAt.Equal(past) {
		t.Fatalf("get ended %+v %v", got, err)
	}
	if _, err := repo.Join(ctx, owner.ID, view.Code); !errors.Is(err, ErrMascotRoomEnded) {
		t.Fatalf("rejoin ended: %v", err)
	}
	if err := repo.CanEnter(ctx, owner.ID, view.Code); !errors.Is(err, ErrMascotRoomEnded) {
		t.Fatalf("enter ended: %v", err)
	}
	next, err := repo.Create(ctx, owner.ID)
	if err != nil {
		t.Fatalf("create after end: %v", err)
	}
	var old domain.MascotRoom
	if err := db.Where("code = ?", view.Code).First(&old).Error; err != nil {
		t.Fatal(err)
	}
	var oldMember int64
	if err := db.Model(&domain.MascotRoomMember{}).Where("room_code = ? AND user_id = ?", view.Code, owner.ID).Count(&oldMember).Error; err != nil {
		t.Fatal(err)
	}
	if oldMember != 1 || next.Code == view.Code {
		t.Fatalf("old membership %d next %s", oldMember, next.Code)
	}
	if next.EndsAt.Sub(next.CreatedAt) != domain.MascotRoomDuration {
		t.Fatalf("new length %s", next.EndsAt.Sub(next.CreatedAt))
	}
}

func TestActiveSeatRejectsASecondRoom(t *testing.T) {
	db := openRoomDB(t)
	user := insertRoomUser(t, db, "seat@example.com")
	now := time.Now().UTC()
	first := domain.MascotRoomActiveSeat{UserID: user.ID, RoomCode: "AAAAAA", EndsAt: now.Add(domain.MascotRoomDuration)}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	second := domain.MascotRoomActiveSeat{UserID: user.ID, RoomCode: "BBBBBB", EndsAt: now.Add(domain.MascotRoomDuration)}
	if err := db.Create(&second).Error; err == nil || !IsUniqueConflict(err) {
		t.Fatalf("second seat %v", err)
	}
}

func TestConcurrentJoinCommitsOneRoom(t *testing.T) {
	db := openConcurrentRoomDB(t)
	repo := NewMascotRoomRepository(db)
	ctx := context.Background()
	for n := 0; n < 20; n++ {
		hostA := insertRoomUser(t, db, "host-a-"+uuid.NewString()+"@example.com")
		hostB := insertRoomUser(t, db, "host-b-"+uuid.NewString()+"@example.com")
		roomA, err := repo.Create(ctx, hostA.ID)
		if err != nil {
			t.Fatal(err)
		}
		roomB, err := repo.Create(ctx, hostB.ID)
		if err != nil {
			t.Fatal(err)
		}
		user := insertRoomUser(t, db, "racer-"+uuid.NewString()+"@example.com")
		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		codes := []string{roomA.Code, roomB.Code}
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				joinCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				_, errs[i] = repo.Join(joinCtx, user.ID, codes[i])
			}(i)
		}
		close(start)
		wg.Wait()
		wins, blocks := 0, 0
		for _, err := range errs {
			if err == nil {
				wins++
				continue
			}
			if errors.As(err, new(*AlreadyInRoomError)) {
				blocks++
				continue
			}
			t.Fatalf("iter %d join error %v", n, err)
		}
		if wins != 1 || blocks != 1 {
			t.Fatalf("iter %d wins=%d blocks=%d errs=%v", n, wins, blocks, errs)
		}
		var members, seats int64
		if err := db.Model(&domain.MascotRoomMember{}).Where("user_id = ?", user.ID).Count(&members).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&domain.MascotRoomActiveSeat{}).Where("user_id = ?", user.ID).Count(&seats).Error; err != nil {
			t.Fatal(err)
		}
		if members != 1 || seats != 1 {
			t.Fatalf("iter %d members=%d seats=%d", n, members, seats)
		}
	}
}

func TestConcurrentCreateCommitsOneRoom(t *testing.T) {
	db := openConcurrentRoomDB(t)
	repo := NewMascotRoomRepository(db)
	user := insertRoomUser(t, db, "create-racer@example.com")
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			createCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_, errs[i] = repo.Create(createCtx, user.ID)
		}(i)
	}
	close(start)
	wg.Wait()
	wins, blocks := 0, 0
	for _, err := range errs {
		if err == nil {
			wins++
			continue
		}
		if errors.As(err, new(*AlreadyInRoomError)) {
			blocks++
			continue
		}
		t.Fatalf("create error %v", err)
	}
	if wins != 1 || blocks != 1 {
		t.Fatalf("wins=%d blocks=%d errs=%v", wins, blocks, errs)
	}
	var rooms int64
	if err := db.Model(&domain.MascotRoom{}).Where("owner_user_id = ?", user.ID).Count(&rooms).Error; err != nil {
		t.Fatal(err)
	}
	if rooms != 1 {
		t.Fatalf("rooms %d", rooms)
	}
}

func insertFixedRoom(t *testing.T, db *gorm.DB, code string, owner uuid.UUID, created, ends time.Time) string {
	t.Helper()
	room := domain.MascotRoom{
		Code: code, OwnerUserID: owner, CreatedAt: created, EndsAt: ends, MaxMembers: 8,
	}
	if err := db.Create(&room).Error; err != nil {
		t.Fatal(err)
	}
	return code
}

func roomEnds(t *testing.T, db *gorm.DB) map[string]time.Time {
	t.Helper()
	var rooms []domain.MascotRoom
	if err := db.Find(&rooms).Error; err != nil {
		t.Fatal(err)
	}
	out := map[string]time.Time{}
	for _, room := range rooms {
		out[room.Code] = room.EndsAt.UTC().Truncate(time.Second)
	}
	return out
}

func roomRowCounts(t *testing.T, db *gorm.DB) (rooms, members int64) {
	t.Helper()
	if err := db.Model(&domain.MascotRoom{}).Count(&rooms).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.MascotRoomMember{}).Count(&members).Error; err != nil {
		t.Fatal(err)
	}
	return rooms, members
}

func openConcurrentRoomDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rooms.db")
	// immediate takes the write lock at BEGIN so two deferred transactions
	// cannot deadlock and return SQLITE_BUSY. The loser waits, then sees the
	// winner's membership.
	dsn := "file:" + path + "?_txlock=immediate&_pragma=busy_timeout(8000)&_pragma=journal_mode(WAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(4)
	if err := db.AutoMigrate(&domain.User{}, &domain.MascotRoom{}, &domain.MascotRoomMember{}, &domain.MascotRoomActiveSeat{}); err != nil {
		t.Fatal(err)
	}
	return db
}
