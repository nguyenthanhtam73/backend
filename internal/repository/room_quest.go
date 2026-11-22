package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/lobby"
	"github.com/dadiary/backend/internal/roomday"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Quest and nudge errors. Handlers map these to HTTP codes.
var (
	ErrQuestChestClosed   = errors.New("chest is not open yet")
	ErrQuestUnknownChest  = errors.New("unknown chest")
	ErrQuestChestsExpired = errors.New("Phòng đã kết thúc, rương hết hạn")
	ErrNudgeSelf          = errors.New("you cannot nudge yourself")
	ErrNudgeDone          = errors.New("this friend already cared or checked in today")
	ErrNudgeAlready       = errors.New("you already nudged this friend today")
	ErrNudgeTarget        = errors.New("that person is not in this room")
)

// questClaimGrace is how long a member who already earned a day may still
// open a chest after the room's ends_at.
const questClaimGrace = 3 * 24 * time.Hour

// SharedMember is one current member plus the look the today board may show.
// Cared and Checked on SharedRoom are Vietnam civil dates, not photo times.
type SharedMember struct {
	UserID      uuid.UUID
	JoinedAt    time.Time
	DisplayName string
	Nickname    string
	Look        domain.MascotLoadout
	Gender      string
}

// SharedRoom is the active room, its members, and the days that count.
type SharedRoom struct {
	Code      string
	CreatedAt time.Time
	EndsAt    time.Time
	Members   []SharedMember
	Cared     map[uuid.UUID][]time.Time
	Checked   map[uuid.UUID][]time.Time
	Claimed   []int
}

// QuestClaimResult is one chest open, including a repeat that did not pay again.
type QuestClaimResult struct {
	MemberDays int
	Coins      int
	Tier       string
	Balance    int64
	Already    bool
	Progress   int
}

// RoomQuestRepository reads care days and writes claims, nudges, and coins.
type RoomQuestRepository struct {
	db *gorm.DB
}

// NewRoomQuestRepository returns the shared-room store.
func NewRoomQuestRepository(db *gorm.DB) *RoomQuestRepository {
	return &RoomQuestRepository{db: db}
}

func (r *RoomQuestRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Load reads a room the caller still belongs to.
// grace is true for the quest bar and the today board: they stay readable
// for three days after ends_at, then return ErrMascotRoomEnded.
// grace is false for actions that stop when the room ends.
func (r *RoomQuestRepository) Load(ctx context.Context, userID uuid.UUID, rawCode string, now time.Time, grace bool) (SharedRoom, error) {
	var zero SharedRoom
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	access := accessLive
	if grace {
		access = accessRead
	}
	var out SharedRoom
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loaded, err := loadSharedRoom(tx, userID, rawCode, now, access)
		if err != nil {
			return err
		}
		out = loaded
		return nil
	})
	if err != nil {
		return zero, err
	}
	return out, nil
}

// Claim opens one chest for userID. A concurrent or repeated call for the
// same room, milestone, and user returns Already true and the same balance.
func (r *RoomQuestRepository) Claim(ctx context.Context, userID uuid.UUID, rawCode string, memberDays int, now time.Time) (QuestClaimResult, error) {
	var zero QuestClaimResult
	chest, ok := domain.RoomQuestMilestoneByDays(memberDays)
	if !ok {
		return zero, ErrQuestUnknownChest
	}
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	var out QuestClaimResult
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loaded, err := loadSharedRoom(tx, userID, rawCode, now, accessClaim)
		if err != nil {
			return err
		}
		progress := roomday.CountMemberDays(loaded.CreatedAt, loaded.EndsAt, spansOf(loaded.Members), loaded.Cared, loaded.Checked)
		out.Progress = progress
		out.MemberDays = chest.MemberDays
		out.Tier = chest.Tier
		own := roomday.CountMemberDays(loaded.CreatedAt, loaded.EndsAt, []roomday.MemberSpan{{
			UserID:   userID,
			JoinedAt: joinedAtOf(loaded.Members, userID),
		}}, loaded.Cared, loaded.Checked)
		if own < 1 || progress < chest.MemberDays {
			return ErrQuestChestClosed
		}
		claim := domain.MascotRoomQuestClaim{
			RoomCode:   loaded.Code,
			MemberDays: chest.MemberDays,
			UserID:     userID,
			Coins:      chest.Coins,
			CreatedAt:  now.UTC(),
		}
		res := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "room_code"},
				{Name: "member_days"},
				{Name: "user_id"},
			},
			DoNothing: true,
		}).Create(&claim)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var existing domain.MascotRoomQuestClaim
			if err := tx.Where("room_code = ? AND member_days = ? AND user_id = ?", loaded.Code, chest.MemberDays, userID).
				Take(&existing).Error; err != nil {
				return err
			}
			balance, err := coinBalance(tx, userID)
			if err != nil {
				return err
			}
			out.Coins = existing.Coins
			out.Balance = balance
			out.Already = true
			return nil
		}
		key := fmt.Sprintf("roomquest:%s:%d:%s:%d", loaded.Code, chest.MemberDays, userID.String(), loaded.CreatedAt.UTC().UnixNano())
		balance, err := creditCoins(tx, userID, int64(chest.Coins), key, domain.CoinReasonRoomQuestChest, now)
		if err != nil {
			return err
		}
		out.Coins = chest.Coins
		out.Balance = balance
		return nil
	})
	if err != nil {
		return zero, err
	}
	return out, nil
}

// SaveNudge stores one nudge after a single room read.
// text builds the server sentence from the sender's name. A second nudge for
// the same sender, target, and Vietnam date returns ErrNudgeAlready and does
// not write again. The room must still be active.
func (r *RoomQuestRepository) SaveNudge(ctx context.Context, userID uuid.UUID, rawCode string, target uuid.UUID, now time.Time, text func(displayName, nickname string) string) (code, vnDate, message string, err error) {
	db, err := r.dbOrErr()
	if err != nil {
		return "", "", "", err
	}
	if userID == uuid.Nil || target == uuid.Nil {
		return "", "", "", fmt.Errorf("user id required")
	}
	if userID == target {
		return "", "", "", ErrNudgeSelf
	}
	if text == nil {
		return "", "", "", fmt.Errorf("nudge text required")
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loaded, err := loadSharedRoom(tx, userID, rawCode, now, accessLive)
		if err != nil {
			return err
		}
		var member *SharedMember
		var sender SharedMember
		for i := range loaded.Members {
			if loaded.Members[i].UserID == target {
				member = &loaded.Members[i]
			}
			if loaded.Members[i].UserID == userID {
				sender = loaded.Members[i]
			}
		}
		if member == nil {
			return ErrNudgeTarget
		}
		caredToday, checkedToday := roomday.TodayFlags(now, loaded.Cared[target], loaded.Checked[target])
		if caredToday || checkedToday {
			return ErrNudgeDone
		}
		vnDate = streaktime.DateOf(now).Format("2006-01-02")
		message = text(sender.DisplayName, sender.Nickname)
		code = loaded.Code
		row := domain.MascotRoomNudge{
			RoomCode:     loaded.Code,
			SenderUserID: userID,
			TargetUserID: target,
			VNDate:       vnDate,
			Message:      message,
			CreatedAt:    now.UTC(),
		}
		res := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "room_code"},
				{Name: "sender_user_id"},
				{Name: "target_user_id"},
				{Name: "vn_date"},
			},
			DoNothing: true,
		}).Create(&row)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNudgeAlready
		}
		return nil
	})
	if err != nil {
		return "", "", "", err
	}
	return code, vnDate, message, nil
}

func joinedAtOf(members []SharedMember, userID uuid.UUID) time.Time {
	for _, member := range members {
		if member.UserID == userID {
			return member.JoinedAt
		}
	}
	return time.Time{}
}

func spansOf(members []SharedMember) []roomday.MemberSpan {
	out := make([]roomday.MemberSpan, len(members))
	for i, member := range members {
		out[i] = roomday.MemberSpan{UserID: member.UserID, JoinedAt: member.JoinedAt}
	}
	return out
}

func loadSharedRoom(tx *gorm.DB, userID uuid.UUID, rawCode string, now time.Time, access roomAccess) (SharedRoom, error) {
	var zero SharedRoom
	code, ok := lobby.NormalizeRoomCode(rawCode)
	if !ok {
		return zero, ErrMascotRoomBadCode
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("user required")
	}
	var room domain.MascotRoom
	err := tx.Where("code = ?", code).Take(&room).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return zero, ErrMascotRoomNotFound
	}
	if err != nil {
		return zero, err
	}
	switch access {
	case accessClaim:
		if err := openFor(room, now, true); err != nil {
			return zero, err
		}
	case accessRead:
		if err := openFor(room, now, false); err != nil {
			return zero, err
		}
	default:
		if !roomIsActive(room, now.UTC()) {
			return zero, ErrMascotRoomEnded
		}
	}
	var rows []domain.MascotRoomMember
	if err := tx.Where("room_code = ?", code).Order("joined_at ASC, user_id ASC").Find(&rows).Error; err != nil {
		return zero, err
	}
	callerIn := false
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
		if row.UserID == userID {
			callerIn = true
		}
	}
	if !callerIn {
		return zero, ErrMascotRoomNotMember
	}
	names := map[uuid.UUID]string{}
	if len(ids) > 0 {
		var users []domain.User
		if err := tx.Select("id", "display_name").Where("id IN ?", ids).Find(&users).Error; err != nil {
			return zero, err
		}
		for _, user := range users {
			names[user.ID] = user.DisplayName
		}
	}
	looks := map[uuid.UUID]domain.UserMascot{}
	if len(ids) > 0 {
		var mascots []domain.UserMascot
		if err := tx.Select("user_id", "loadout", "gender").Where("user_id IN ?", ids).Find(&mascots).Error; err != nil {
			return zero, err
		}
		for _, mascot := range mascots {
			looks[mascot.UserID] = mascot
		}
	}
	since := streaktime.DateOf(room.CreatedAt)
	cared, checked, err := loadCareDays(tx, ids, since)
	if err != nil {
		return zero, err
	}
	var claims []domain.MascotRoomQuestClaim
	if err := tx.Where("room_code = ? AND user_id = ?", code, userID).Find(&claims).Error; err != nil {
		return zero, err
	}
	claimed := make([]int, 0, len(claims))
	for _, claim := range claims {
		claimed = append(claimed, claim.MemberDays)
	}
	members := make([]SharedMember, len(rows))
	for i, row := range rows {
		look, gender := lookOf(looks[row.UserID])
		members[i] = SharedMember{
			UserID:      row.UserID,
			JoinedAt:    row.JoinedAt,
			DisplayName: names[row.UserID],
			Nickname:    row.Nickname,
			Look:        look,
			Gender:      gender,
		}
	}
	return SharedRoom{
		Code:      room.Code,
		CreatedAt: room.CreatedAt,
		EndsAt:    effectiveEndsAt(room),
		Members:   members,
		Cared:     cared,
		Checked:   checked,
		Claimed:   claimed,
	}, nil
}

type roomAccess int

const (
	accessLive roomAccess = iota
	accessRead
	accessClaim
)

// openFor allows the room while it is active and for three days after ends_at.
// Past that, a claim reports the chest expiry and a read reports the room ended.
func openFor(room domain.MascotRoom, now time.Time, claim bool) error {
	end := effectiveEndsAt(room)
	if end.IsZero() {
		return ErrMascotRoomEnded
	}
	now = now.UTC()
	if end.After(now) || !now.After(end.Add(questClaimGrace)) {
		return nil
	}
	if claim {
		return ErrQuestChestsExpired
	}
	return ErrMascotRoomEnded
}

func lookOf(row domain.UserMascot) (domain.MascotLoadout, string) {
	look := domain.DefaultMascotLoadout()
	if len(row.Loadout) > 0 {
		var parsed domain.MascotLoadout
		if err := json.Unmarshal(row.Loadout, &parsed); err == nil && parsed.Body != "" {
			look = parsed
		}
	}
	if look.Accessories == nil {
		look.Accessories = []string{}
	}
	if look.RoomItems == nil {
		look.RoomItems = []string{}
	}
	return look, lobby.PublicGender(row.Gender)
}

func loadCareDays(tx *gorm.DB, ids []uuid.UUID, since time.Time) (map[uuid.UUID][]time.Time, map[uuid.UUID][]time.Time, error) {
	cared := map[uuid.UUID][]time.Time{}
	checked := map[uuid.UUID][]time.Time{}
	if len(ids) == 0 {
		return cared, checked, nil
	}
	var routines []domain.RoutineEntry
	err := tx.Select("user_id", "routine_date", "morning", "evening").
		Where("user_id IN ? AND routine_date >= ?", ids, since).
		Find(&routines).Error
	if err != nil {
		return nil, nil, err
	}
	for _, row := range routines {
		if !domain.RoutineSessionDone(row.Morning, row.Evening) {
			continue
		}
		cared[row.UserID] = append(cared[row.UserID], streaktime.DateOf(row.RoutineDate))
	}
	var checks []domain.SkinCheck
	err = tx.Select("user_id", "check_date").
		Where("user_id IN ? AND check_date >= ?", ids, since).
		Find(&checks).Error
	if err != nil {
		return nil, nil, err
	}
	for _, row := range checks {
		if row.CheckDate.IsZero() {
			continue
		}
		checked[row.UserID] = append(checked[row.UserID], streaktime.DateOf(row.CheckDate))
	}
	return cared, checked, nil
}
