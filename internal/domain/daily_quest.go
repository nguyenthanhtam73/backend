package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Private-room daily quest chest (morning routine, evening routine).
// These constants are the only xu amounts. A client cannot send a price.
// Morning 8 + evening 8 + bonus 2 when both sessions of the same care day
// are claimed = 18 xu. The bonus is credited with the second claim.
const (
	DailyQuestSessionMorning = "morning"
	DailyQuestSessionEvening = "evening"

	DailyQuestMorningCoins = 8
	DailyQuestEveningCoins = 8
	DailyQuestBonusCoins   = 2

	// Ledger reasons. One row per session, and one bonus row per care day.
	CoinReasonDailyQuestMorning = "daily_quest_morning"
	CoinReasonDailyQuestEvening = "daily_quest_evening"
	CoinReasonDailyQuestBonus   = "daily_quest_bonus"
)

// NormalizeDailyQuestSession accepts "morning" or "evening".
func NormalizeDailyQuestSession(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case DailyQuestSessionMorning:
		return DailyQuestSessionMorning, true
	case DailyQuestSessionEvening:
		return DailyQuestSessionEvening, true
	default:
		return "", false
	}
}

// DailyQuestSessionCoins is the xu for one session, before the both-sessions bonus.
func DailyQuestSessionCoins(session string) int {
	switch session {
	case DailyQuestSessionMorning:
		return DailyQuestMorningCoins
	case DailyQuestSessionEvening:
		return DailyQuestEveningCoins
	default:
		return 0
	}
}

// DailyQuestSessionReason is the ledger reason for that session's credit.
func DailyQuestSessionReason(session string) string {
	switch session {
	case DailyQuestSessionMorning:
		return CoinReasonDailyQuestMorning
	case DailyQuestSessionEvening:
		return CoinReasonDailyQuestEvening
	default:
		return ""
	}
}

// DailyQuestClaim is one user opening one session chest on one care day.
// The primary key is the lock: a second insert for the same user, day, and
// session does nothing and does not pay again. Day is the routine care day
// (Vietnam calendar with a 04:00 rollover), stored as YYYY-MM-DD.
// Coins is the session amount that was credited. Bonus is 0, or
// DailyQuestBonusCoins when this claim was the second session of that day.
type DailyQuestClaim struct {
	UserID    uuid.UUID `gorm:"type:uuid;primaryKey"`
	Day       string    `gorm:"primaryKey;size:10"`
	Session   string    `gorm:"primaryKey;size:16"`
	Coins     int       `gorm:"not null"`
	Bonus     int       `gorm:"not null;default:0"`
	CreatedAt time.Time `gorm:"not null"`
}

func (DailyQuestClaim) TableName() string { return "daily_quest_claims" }
