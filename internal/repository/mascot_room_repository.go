package repository

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/lobby"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Membership errors for Bé Da invite rooms. Messages stay generic so a
// handler can map them without logging the account or the code.
var (
	ErrMascotRoomNotFound  = errors.New("mascot room not found")
	ErrMascotRoomFull      = errors.New("mascot room full")
	ErrMascotRoomNotMember = errors.New("mascot room not a member")
	ErrMascotRoomBadCode   = errors.New("mascot room bad code")
	ErrMascotRoomTooMany   = errors.New("too many open rooms")
)

// MaxOpenMascotRooms is how many rooms one account may own at the same time.
// A room counts until its last member leaves and the row is deleted.
const MaxOpenMascotRooms = 3

// MascotRoomView is a room the caller is allowed to see.
// It has no account ids and no emails.
type MascotRoomView struct {
	Code        string
	CreatedAt   time.Time
	Cap         int
	MemberCount int
	Joined      bool
	Owner       bool
}

// MascotRoomRepository stores invite rooms and their members.
type MascotRoomRepository struct {
	db *gorm.DB
}

// NewMascotRoomRepository returns a room repository.
func NewMascotRoomRepository(db *gorm.DB) *MascotRoomRepository {
	return &MascotRoomRepository{db: db}
}

// Create opens a room and adds the owner as the first member.
func (r *MascotRoomRepository) Create(ctx context.Context, owner uuid.UUID) (MascotRoomView, error) {
	var zero MascotRoomView
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	if owner == uuid.Nil {
		return zero, fmt.Errorf("owner required")
	}
	var view MascotRoomView
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRoomOwner(tx, owner); err != nil {
			return err
		}
		var owned int64
		if err := tx.Model(&domain.MascotRoom{}).Where("owner_user_id = ?", owner).Count(&owned).Error; err != nil {
			return err
		}
		if owned >= MaxOpenMascotRooms {
			return ErrMascotRoomTooMany
		}
		for i := 0; i < 8; i++ {
			code, err := lobby.NewRoomCode()
			if err != nil {
				return err
			}
			now := time.Now().UTC()
			room := domain.MascotRoom{
				Code:        code,
				OwnerUserID: owner,
				CreatedAt:   now,
				MaxMembers:  lobby.RoomCap,
			}
			if err := tx.SavePoint("mascot_room_code").Error; err != nil {
				return err
			}
			if err := tx.Create(&room).Error; err != nil {
				if IsUniqueConflict(err) {
					if rb := tx.RollbackTo("mascot_room_code"); rb.Error != nil {
						return rb.Error
					}
					continue
				}
				return err
			}
			member := domain.MascotRoomMember{RoomCode: code, UserID: owner, JoinedAt: now}
			if err := tx.Create(&member).Error; err != nil {
				return err
			}
			view = MascotRoomView{
				Code: code, CreatedAt: now, Cap: lobby.RoomCap,
				MemberCount: 1, Joined: true, Owner: true,
			}
			return nil
		}
		return fmt.Errorf("could not allocate room code")
	})
	if err != nil {
		return zero, err
	}
	return view, nil
}

// Get loads a room for userID. Joined and Owner describe that caller.
func (r *MascotRoomRepository) Get(ctx context.Context, userID uuid.UUID, rawCode string) (MascotRoomView, error) {
	return r.load(ctx, userID, rawCode, false)
}

// Join adds userID when the room has a free seat. Joining twice is a no-op.
func (r *MascotRoomRepository) Join(ctx context.Context, userID uuid.UUID, rawCode string) (MascotRoomView, error) {
	var zero MascotRoomView
	code, ok := lobby.NormalizeRoomCode(rawCode)
	if !ok {
		return zero, ErrMascotRoomBadCode
	}
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	var view MascotRoomView
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		room, err := lockMascotRoom(tx, code)
		if err != nil {
			return err
		}
		n, member, err := memberState(tx, code, userID)
		if err != nil {
			return err
		}
		cap := roomCap(room)
		if !member {
			if n >= cap {
				return ErrMascotRoomFull
			}
			if err := tx.Create(&domain.MascotRoomMember{
				RoomCode: code,
				UserID:   userID,
				JoinedAt: time.Now().UTC(),
			}).Error; err != nil {
				if !IsUniqueConflict(err) {
					return err
				}
			} else {
				n++
			}
		}
		view = roomView(room, userID, n, true)
		return nil
	})
	if err != nil {
		return zero, err
	}
	return view, nil
}

// Leave removes userID only. The room creator cannot remove anyone else.
// There is no kick or ban. The room row is deleted when it has no members left.
func (r *MascotRoomRepository) Leave(ctx context.Context, userID uuid.UUID, rawCode string) error {
	code, ok := lobby.NormalizeRoomCode(rawCode)
	if !ok {
		return ErrMascotRoomBadCode
	}
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockMascotRoom(tx, code); err != nil {
			return err
		}
		if err := tx.Where("room_code = ? AND user_id = ?", code, userID).Delete(&domain.MascotRoomMember{}).Error; err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&domain.MascotRoomMember{}).Where("room_code = ?", code).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return tx.Where("code = ?", code).Delete(&domain.MascotRoom{}).Error
		}
		return nil
	})
}

// RoomMember is one roster row. UserID stays inside the server.
type RoomMember struct {
	UserID   uuid.UUID
	Nickname string
	JoinedAt time.Time
}

// ListMembers returns the roster in join order. The code must already be canonical.
func (r *MascotRoomRepository) ListMembers(ctx context.Context, code string) ([]RoomMember, error) {
	db, err := r.dbOrErr()
	if err != nil {
		return nil, err
	}
	code, ok := lobby.NormalizeRoomCode(code)
	if !ok {
		return nil, ErrMascotRoomBadCode
	}
	var rows []domain.MascotRoomMember
	err = db.WithContext(ctx).
		Where("room_code = ?", code).
		Order("joined_at ASC, user_id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]RoomMember, len(rows))
	for i, row := range rows {
		out[i] = RoomMember{UserID: row.UserID, Nickname: row.Nickname, JoinedAt: row.JoinedAt}
	}
	return out, nil
}

// SaveNickname stores the live room persona for this member.
// A name the lobby would reject is ignored. Missing membership is not an error.
func (r *MascotRoomRepository) SaveNickname(ctx context.Context, userID uuid.UUID, rawCode, nickname string) error {
	code, ok := lobby.NormalizeRoomCode(rawCode)
	if !ok {
		return ErrMascotRoomBadCode
	}
	if userID == uuid.Nil {
		return nil
	}
	nick, ok := lobby.CleanNickname(nickname)
	if !ok || utf8.RuneCountInString(nick) > lobby.MaxNicknameRunes {
		return nil
	}
	db, err := r.dbOrErr()
	if err != nil {
		return err
	}
	return db.WithContext(ctx).
		Model(&domain.MascotRoomMember{}).
		Where("room_code = ? AND user_id = ?", code, userID).
		Update("nickname", nick).Error
}

// CanEnter reports whether userID is a member of an existing room.
// The live seat cap is enforced by the hub, not here.
func (r *MascotRoomRepository) CanEnter(ctx context.Context, userID uuid.UUID, rawCode string) error {
	_, err := r.load(ctx, userID, rawCode, true)
	return err
}

func (r *MascotRoomRepository) load(ctx context.Context, userID uuid.UUID, rawCode string, membersOnly bool) (MascotRoomView, error) {
	var zero MascotRoomView
	code, ok := lobby.NormalizeRoomCode(rawCode)
	if !ok {
		return zero, ErrMascotRoomBadCode
	}
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	var room domain.MascotRoom
	err = db.WithContext(ctx).Where("code = ?", code).First(&room).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return zero, ErrMascotRoomNotFound
	}
	if err != nil {
		return zero, err
	}
	n, member, err := memberState(db.WithContext(ctx), code, userID)
	if err != nil {
		return zero, err
	}
	if membersOnly && !member {
		return zero, ErrMascotRoomNotMember
	}
	return roomView(room, userID, n, member), nil
}

func (r *MascotRoomRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// lockRoomOwner serializes creates for one account on Postgres so two
// requests cannot both pass the open-room cap. The id is not scanned.
// SQLite tests run one at a time and skip the lock.
func lockRoomOwner(tx *gorm.DB, owner uuid.UUID) error {
	if tx.Dialector == nil || tx.Dialector.Name() != "postgres" {
		return nil
	}
	return tx.Exec("SELECT id FROM users WHERE id = ? FOR UPDATE", owner).Error
}

func lockMascotRoom(tx *gorm.DB, code string) (domain.MascotRoom, error) {
	q := tx
	if tx.Dialector != nil && tx.Dialector.Name() == "postgres" {
		q = tx.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var room domain.MascotRoom
	err := q.Where("code = ?", code).First(&room).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.MascotRoom{}, ErrMascotRoomNotFound
	}
	if err != nil {
		return domain.MascotRoom{}, err
	}
	return room, nil
}

func memberState(tx *gorm.DB, code string, userID uuid.UUID) (count int, member bool, err error) {
	var n int64
	if err = tx.Model(&domain.MascotRoomMember{}).Where("room_code = ?", code).Count(&n).Error; err != nil {
		return 0, false, err
	}
	if userID == uuid.Nil {
		return int(n), false, nil
	}
	var mine int64
	if err = tx.Model(&domain.MascotRoomMember{}).Where("room_code = ? AND user_id = ?", code, userID).Count(&mine).Error; err != nil {
		return 0, false, err
	}
	return int(n), mine > 0, nil
}

func roomCap(room domain.MascotRoom) int {
	if room.MaxMembers > 0 && room.MaxMembers < lobby.RoomCap {
		return room.MaxMembers
	}
	return lobby.RoomCap
}

func roomView(room domain.MascotRoom, userID uuid.UUID, count int, member bool) MascotRoomView {
	return MascotRoomView{
		Code:        room.Code,
		CreatedAt:   room.CreatedAt,
		Cap:         roomCap(room),
		MemberCount: count,
		Joined:      member,
		Owner:       userID != uuid.Nil && room.OwnerUserID == userID,
	}
}
