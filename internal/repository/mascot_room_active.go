package repository

import (
	"log/slog"
	"sort"
	"time"

	"fmt"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// mascotRoomEndsUnsetBefore marks an ends_at that was never chosen.
// Year 1 (the Go zero time) and the unix epoch both fall before it.
// A real room end does not.
var mascotRoomEndsUnsetBefore = time.Date(1971, 1, 1, 0, 0, 0, 0, time.UTC)

// AlreadyInRoomError is returned when create or join would put an account
// into a second active room. Code is the invite code of one current room
// (rooms have no separate display name). When the account is in several,
// Code is the one that ends last, and Count is how many are still active.
// The account stays in all of them.
type AlreadyInRoomError struct {
	Code   string
	EndsAt time.Time
	Count  int
}

func (e *AlreadyInRoomError) Error() string { return "already in a room" }

// CountUsersInMultipleActiveRooms counts accounts that belong to more than
// one room that is still active. It does not insert, update, or delete.
func CountUsersInMultipleActiveRooms(db *gorm.DB, now time.Time) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database not configured")
	}
	per, err := activeMemberCounts(db, now)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, count := range per {
		if count > 1 {
			n++
		}
	}
	return n, nil
}

// effectiveEndsAt is the end stored on the room.
// A missing end means the previous 21-day length. It is not capped here:
// capping is written once by backfill, so a later read cannot slide the end
// forward by another 8 days.
func effectiveEndsAt(room domain.MascotRoom) time.Time {
	if !roomEndStored(room) {
		if room.CreatedAt.IsZero() {
			return time.Time{}
		}
		return room.CreatedAt.UTC().Add(domain.LegacyMascotRoomDuration)
	}
	return room.EndsAt.UTC()
}

func roomEndStored(room domain.MascotRoom) bool {
	return !room.EndsAt.IsZero() && !room.EndsAt.Before(mascotRoomEndsUnsetBefore)
}

// cappedRoomEnd is min(current end, now + 8 days).
// The current end is the stored end, or created_at + 21 days when none is
// stored. A current end that is already sooner is kept, including one that
// is already in the past.
func cappedRoomEnd(room domain.MascotRoom, now time.Time) time.Time {
	end := effectiveEndsAt(room)
	if end.IsZero() {
		return time.Time{}
	}
	end = end.UTC().Truncate(time.Second)
	cap := now.UTC().Truncate(time.Second).Add(domain.MascotRoomDuration)
	if end.After(cap) {
		return cap
	}
	return end
}

func roomIsActive(room domain.MascotRoom, now time.Time) bool {
	end := effectiveEndsAt(room)
	return !end.IsZero() && end.After(now)
}

// prepareMascotRoomEnds adds ends_at before AutoMigrate marks it NOT NULL.
// A missing end is LEAST(created_at + 21 days, now + 8 days). SQLite tests
// create the column with the table, so they skip this and use
// backfillLegacyRoomEnds.
func prepareMascotRoomEnds(db *gorm.DB) error {
	if db == nil || db.Dialector == nil || db.Dialector.Name() != "postgres" {
		return nil
	}
	if !db.Migrator().HasTable(&domain.MascotRoom{}) {
		return nil
	}
	if !db.Migrator().HasColumn(&domain.MascotRoom{}, "ends_at") {
		if err := db.Exec(`ALTER TABLE mascot_rooms ADD COLUMN ends_at TIMESTAMPTZ`).Error; err != nil {
			return err
		}
	}
	return db.Exec(`
		UPDATE mascot_rooms
		SET ends_at = LEAST(created_at + INTERVAL '21 days', NOW() + INTERVAL '8 days')
		WHERE ends_at IS NULL OR ends_at < TIMESTAMPTZ '1971-01-01'
	`).Error
}

// applyMascotRoomTerm fills any end that is still unset and seats accounts
// that have exactly one active room. Accounts in several active rooms are
// not seated and are not removed. Safe to run on every boot.
func applyMascotRoomTerm(db *gorm.DB) error {
	if db == nil || db.Dialector == nil || db.Dialector.Name() == "" {
		return nil
	}
	if !db.Migrator().HasTable(&domain.MascotRoom{}) {
		return nil
	}
	now := time.Now().UTC()
	if _, err := backfillLegacyRoomEnds(db, now); err != nil {
		return err
	}
	if err := backfillSingleActiveSeats(db, now); err != nil {
		return err
	}
	n, err := CountUsersInMultipleActiveRooms(db, now)
	if err != nil {
		return err
	}
	slog.Info("mascot rooms: one active room", "users_in_multiple_active_rooms", n)
	return nil
}

// backfillLegacyRoomEnds writes min(current end, now + 8 days) onto every
// room that is still past that cap, and fills a missing end the same way.
// The current end is the stored end, or created_at + 21 days when none is
// stored. An end that is already sooner is left alone, so a room that has
// ended stays ended. It returns how many rows changed. Safe to run again:
// a cap written last time is not later than now + 8 days.
func backfillLegacyRoomEnds(db *gorm.DB, now time.Time) (int, error) {
	var rooms []domain.MascotRoom
	if err := db.Find(&rooms).Error; err != nil {
		return 0, err
	}
	n := 0
	for _, room := range rooms {
		end := cappedRoomEnd(room, now)
		if end.IsZero() {
			continue
		}
		if roomEndStored(room) && room.EndsAt.UTC().Truncate(time.Second).Equal(end) {
			continue
		}
		if err := db.Model(&domain.MascotRoom{}).Where("code = ?", room.Code).Update("ends_at", end).Error; err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// backfillSingleActiveSeats inserts a seat for an account whose only active
// room has none. An account in two or more active rooms is skipped.
func backfillSingleActiveSeats(db *gorm.DB, now time.Time) error {
	if !db.Migrator().HasTable(&domain.MascotRoomActiveSeat{}) {
		return nil
	}
	rooms, err := roomsByMember(db)
	if err != nil {
		return err
	}
	var members []domain.MascotRoomMember
	if err := db.Find(&members).Error; err != nil {
		return err
	}
	type hit struct {
		code string
		ends time.Time
	}
	per := map[uuid.UUID][]hit{}
	for _, member := range members {
		room, ok := rooms[member.RoomCode]
		if !ok || !roomIsActive(room, now) {
			continue
		}
		per[member.UserID] = append(per[member.UserID], hit{
			code: room.Code,
			ends: effectiveEndsAt(room),
		})
	}
	for userID, hits := range per {
		if len(hits) != 1 {
			continue
		}
		var n int64
		if err := db.Model(&domain.MascotRoomActiveSeat{}).Where("user_id = ?", userID).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		seat := domain.MascotRoomActiveSeat{UserID: userID, RoomCode: hits[0].code, EndsAt: hits[0].ends}
		if err := db.Create(&seat).Error; err != nil {
			if IsUniqueConflict(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func activeMemberCounts(db *gorm.DB, now time.Time) (map[uuid.UUID]int, error) {
	rooms, err := roomsByMember(db)
	if err != nil {
		return nil, err
	}
	var members []domain.MascotRoomMember
	if err := db.Find(&members).Error; err != nil {
		return nil, err
	}
	per := map[uuid.UUID]int{}
	for _, member := range members {
		room, ok := rooms[member.RoomCode]
		if !ok || !roomIsActive(room, now) {
			continue
		}
		per[member.UserID]++
	}
	return per, nil
}

func roomsByMember(db *gorm.DB) (map[string]domain.MascotRoom, error) {
	var rooms []domain.MascotRoom
	if err := db.Find(&rooms).Error; err != nil {
		return nil, err
	}
	out := make(map[string]domain.MascotRoom, len(rooms))
	for _, room := range rooms {
		out[room.Code] = room
	}
	return out, nil
}

// activeRoomsFor returns the caller's still-active rooms, latest end first.
func activeRoomsFor(tx *gorm.DB, userID uuid.UUID, now time.Time) ([]domain.MascotRoom, error) {
	var codes []string
	if err := tx.Model(&domain.MascotRoomMember{}).Where("user_id = ?", userID).Pluck("room_code", &codes).Error; err != nil {
		return nil, err
	}
	if len(codes) == 0 {
		return nil, nil
	}
	var rooms []domain.MascotRoom
	if err := tx.Where("code IN ?", codes).Find(&rooms).Error; err != nil {
		return nil, err
	}
	out := make([]domain.MascotRoom, 0, len(rooms))
	for _, room := range rooms {
		if !roomIsActive(room, now) {
			continue
		}
		room.EndsAt = effectiveEndsAt(room)
		out = append(out, room)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].EndsAt.Equal(out[j].EndsAt) {
			return out[i].EndsAt.After(out[j].EndsAt)
		}
		return out[i].Code < out[j].Code
	})
	return out, nil
}

// rejectIfInActiveRoom refuses a new membership while any active room remains.
func rejectIfInActiveRoom(tx *gorm.DB, userID uuid.UUID, now time.Time) error {
	rooms, err := activeRoomsFor(tx, userID, now)
	if err != nil {
		return err
	}
	if len(rooms) == 0 {
		return nil
	}
	return &AlreadyInRoomError{Code: rooms[0].Code, EndsAt: rooms[0].EndsAt, Count: len(rooms)}
}

// releaseEndedSeat drops a seat whose room is already over.
// A seat that still has a future end is left alone. Deleting that row from
// a second transaction would let two joins both commit.
func releaseEndedSeat(tx *gorm.DB, userID uuid.UUID, now time.Time) error {
	var codes []string
	if err := tx.Model(&domain.MascotRoomMember{}).Where("user_id = ?", userID).Pluck("room_code", &codes).Error; err != nil {
		return err
	}
	ended := []string{}
	if len(codes) > 0 {
		var rooms []domain.MascotRoom
		if err := tx.Where("code IN ?", codes).Find(&rooms).Error; err != nil {
			return err
		}
		for _, room := range rooms {
			if !roomIsActive(room, now) {
				ended = append(ended, room.Code)
			}
		}
	}
	q := tx.Where("user_id = ? AND ends_at <= ?", userID, now)
	if len(ended) > 0 {
		q = tx.Where("user_id = ? AND (ends_at <= ? OR room_code IN ?)", userID, now, ended)
	}
	return q.Delete(&domain.MascotRoomActiveSeat{}).Error
}

// claimActiveSeat inserts the one-room slot. A conflicting seat rolls the
// caller's new membership back with the transaction. exceptCode is the room
// just inserted in this transaction, so the error names the other room.
func claimActiveSeat(tx *gorm.DB, userID uuid.UUID, code string, endsAt, now time.Time) error {
	if err := releaseEndedSeat(tx, userID, now); err != nil {
		return err
	}
	if err := tx.SavePoint("active_seat").Error; err != nil {
		return err
	}
	seat := domain.MascotRoomActiveSeat{UserID: userID, RoomCode: code, EndsAt: endsAt}
	err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}},
		DoNothing: true,
	}).Create(&seat).Error
	if err != nil {
		if !IsUniqueConflict(err) {
			return err
		}
		if rb := tx.RollbackTo("active_seat"); rb.Error != nil {
			return rb.Error
		}
	}
	var stored domain.MascotRoomActiveSeat
	if err := tx.Where("user_id = ?", userID).First(&stored).Error; err != nil {
		return err
	}
	if stored.RoomCode == code {
		return nil
	}
	return alreadyInRoomErr(tx, userID, code, now)
}

func alreadyInRoomErr(tx *gorm.DB, userID uuid.UUID, exceptCode string, now time.Time) error {
	rooms, err := activeRoomsFor(tx, userID, now)
	if err != nil {
		return err
	}
	blocking := make([]domain.MascotRoom, 0, len(rooms))
	for _, room := range rooms {
		if room.Code == exceptCode {
			continue
		}
		blocking = append(blocking, room)
	}
	if len(blocking) == 0 {
		var seat domain.MascotRoomActiveSeat
		q := tx.Where("user_id = ?", userID)
		if exceptCode != "" {
			q = q.Where("room_code <> ?", exceptCode)
		}
		if err := q.First(&seat).Error; err == nil {
			return &AlreadyInRoomError{Code: seat.RoomCode, EndsAt: seat.EndsAt.UTC(), Count: 1}
		}
		return &AlreadyInRoomError{Count: 1}
	}
	return &AlreadyInRoomError{Code: blocking[0].Code, EndsAt: blocking[0].EndsAt, Count: len(blocking)}
}

func releaseSeat(tx *gorm.DB, userID uuid.UUID, code string) error {
	if !tx.Migrator().HasTable(&domain.MascotRoomActiveSeat{}) {
		return nil
	}
	q := tx.Where("user_id = ?", userID)
	if code != "" {
		q = q.Where("room_code = ?", code)
	}
	return q.Delete(&domain.MascotRoomActiveSeat{}).Error
}
