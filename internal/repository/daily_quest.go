package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Daily quest claim errors. The handler maps these to HTTP codes.
var (
	ErrDailyQuestSession = errors.New("session must be morning or evening")
	ErrDailyQuestSteps   = errors.New("Buổi này chưa xong hết các bước.")
)

// DailyQuestClaimResult is one chest open, including a repeat that did not pay again.
type DailyQuestClaimResult struct {
	Granted int
	Bonus   int
	Balance int64
	Already bool
	Session string
}

// DailyQuestSessionState is whether that session's steps are all ticked,
// and whether this user already claimed it for the care day.
type DailyQuestSessionState struct {
	Done    bool
	Claimed bool
}

// DailyQuestStatus is the care-day chest for GET /api/v1/me/mascot.
type DailyQuestStatus struct {
	Morning DailyQuestSessionState
	Evening DailyQuestSessionState
}

// DailyQuestRepository credits the private-room daily chest.
type DailyQuestRepository struct {
	db *gorm.DB
}

// NewDailyQuestRepository returns the daily quest store.
func NewDailyQuestRepository(db *gorm.DB) *DailyQuestRepository {
	return &DailyQuestRepository{db: db}
}

func (r *DailyQuestRepository) dbOrErr() (*gorm.DB, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("database not configured")
	}
	return r.db, nil
}

// Status reads the care-day routine and which sessions are already claimed.
// It does not credit anything. Amounts stay in domain.DailyQuest*Coins.
func (r *DailyQuestRepository) Status(ctx context.Context, userID uuid.UUID, now time.Time) (DailyQuestStatus, error) {
	var zero DailyQuestStatus
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("user required")
	}
	day := streaktime.RoutineCareDay(now)
	dayKey := day.Format("2006-01-02")
	var routine domain.RoutineEntry
	err = db.WithContext(ctx).
		Where("user_id = ? AND routine_date = ?", userID, day).
		Take(&routine).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	if err != nil {
		return zero, err
	}
	var claims []domain.DailyQuestClaim
	if err := db.WithContext(ctx).
		Where("user_id = ? AND day = ?", userID, dayKey).
		Find(&claims).Error; err != nil {
		return zero, err
	}
	claimed := map[string]bool{}
	for _, claim := range claims {
		claimed[claim.Session] = true
	}
	return DailyQuestStatus{
		Morning: DailyQuestSessionState{
			Done:    domain.RoutineOneSessionDone(routine.Morning),
			Claimed: claimed[domain.DailyQuestSessionMorning],
		},
		Evening: DailyQuestSessionState{
			Done:    domain.RoutineOneSessionDone(routine.Evening),
			Claimed: claimed[domain.DailyQuestSessionEvening],
		},
	}, nil
}

// Claim credits one session chest for the care day of now.
// The server reads the stored routine. A client amount is never accepted.
// A second call for the same user, care day, and session returns Already
// and the balance already on the wallet. The both-sessions bonus is credited
// once, on the claim that finds the other session already claimed.
func (r *DailyQuestRepository) Claim(ctx context.Context, userID uuid.UUID, rawSession string, now time.Time) (DailyQuestClaimResult, error) {
	var zero DailyQuestClaimResult
	session, ok := domain.NormalizeDailyQuestSession(rawSession)
	if !ok {
		return zero, ErrDailyQuestSession
	}
	if userID == uuid.Nil {
		return zero, fmt.Errorf("user required")
	}
	db, err := r.dbOrErr()
	if err != nil {
		return zero, err
	}
	day := streaktime.RoutineCareDay(now)
	dayKey := day.Format("2006-01-02")
	coins := domain.DailyQuestSessionCoins(session)
	reason := domain.DailyQuestSessionReason(session)
	if coins <= 0 || reason == "" {
		return zero, fmt.Errorf("daily quest payout is not configured")
	}
	var out DailyQuestClaimResult
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockDailyQuestUser(tx, userID); err != nil {
			return err
		}
		var existing domain.DailyQuestClaim
		err := tx.Where("user_id = ? AND day = ? AND session = ?", userID, dayKey, session).Take(&existing).Error
		if err == nil {
			return fillDailyQuestAlready(tx, existing, &out)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var routine domain.RoutineEntry
		err = tx.Where("user_id = ? AND routine_date = ?", userID, day).Take(&routine).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrDailyQuestSteps
		}
		if err != nil {
			return err
		}
		raw := routine.Morning
		if session == domain.DailyQuestSessionEvening {
			raw = routine.Evening
		}
		if !domain.RoutineOneSessionDone(raw) {
			return ErrDailyQuestSteps
		}
		var other int64
		if err := tx.Model(&domain.DailyQuestClaim{}).
			Where("user_id = ? AND day = ? AND session <> ?", userID, dayKey, session).
			Count(&other).Error; err != nil {
			return err
		}
		bonus := 0
		if other > 0 {
			bonus = domain.DailyQuestBonusCoins
		}
		row := domain.DailyQuestClaim{
			UserID:    userID,
			Day:       dayKey,
			Session:   session,
			Coins:     coins,
			Bonus:     bonus,
			CreatedAt: now.UTC(),
		}
		res := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "user_id"},
				{Name: "day"},
				{Name: "session"},
			},
			DoNothing: true,
		}).Create(&row)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Where("user_id = ? AND day = ? AND session = ?", userID, dayKey, session).
				Take(&existing).Error; err != nil {
				return err
			}
			return fillDailyQuestAlready(tx, existing, &out)
		}
		key := fmt.Sprintf("dailyquest:%s:%s:%s", userID.String(), dayKey, session)
		balance, err := creditCoins(tx, userID, int64(coins), key, reason, now)
		if err != nil {
			return err
		}
		if bonus > 0 {
			bonusKey := fmt.Sprintf("dailyquest:%s:%s:bonus", userID.String(), dayKey)
			balance, err = creditCoins(tx, userID, int64(bonus), bonusKey, domain.CoinReasonDailyQuestBonus, now)
			if err != nil {
				return err
			}
		}
		out = DailyQuestClaimResult{
			Granted: coins,
			Bonus:   bonus,
			Balance: balance,
			Session: session,
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	return out, nil
}

func fillDailyQuestAlready(tx *gorm.DB, existing domain.DailyQuestClaim, out *DailyQuestClaimResult) error {
	balance, err := coinBalance(tx, existing.UserID)
	if err != nil {
		return err
	}
	*out = DailyQuestClaimResult{
		Granted: existing.Coins,
		Bonus:   existing.Bonus,
		Balance: balance,
		Already: true,
		Session: existing.Session,
	}
	return nil
}

// lockDailyQuestUser serializes claims for one account so the both-sessions
// bonus is decided once. Postgres locks the user row. SQLite takes the
// write lock by touching that row; a second transaction waits, then sees
// the claim that committed.
func lockDailyQuestUser(tx *gorm.DB, userID uuid.UUID) error {
	if userID == uuid.Nil {
		return fmt.Errorf("user required")
	}
	if tx.Dialector != nil && tx.Dialector.Name() == "postgres" {
		// id comes back as text. Scanning into uuid.UUID treats it as bytes.
		var id string
		err := tx.Raw(`SELECT id::text FROM users WHERE id = ? FOR UPDATE`, userID).Scan(&id).Error
		if err != nil {
			return err
		}
		if id == "" {
			return fmt.Errorf("user required")
		}
		return nil
	}
	// The UPDATE takes SQLite's write lock even when the timestamp does not
	// change. A second claim waits here, then reads the committed row.
	if err := tx.Exec(`UPDATE users SET updated_at = updated_at WHERE id = ?`, userID).Error; err != nil {
		return err
	}
	var n int64
	if err := tx.Model(&domain.User{}).Where("id = ?", userID).Count(&n).Error; err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("user required")
	}
	return nil
}
