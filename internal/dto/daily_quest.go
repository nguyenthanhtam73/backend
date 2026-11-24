package dto

import "github.com/dadiary/backend/internal/domain"

// DailyQuestStatus is the private-room chest on GET /api/v1/me/mascot.
// Amounts are domain.DailyQuestMorningCoins, DailyQuestEveningCoins, and
// DailyQuestBonusCoins. Old clients ignore this object.
type DailyQuestStatus struct {
	Morning DailyQuestSession `json:"morning"`
	Evening DailyQuestSession `json:"evening"`
	Amounts DailyQuestAmounts `json:"amounts"`
}

// DailyQuestSession is one care-day session.
// Done is true when every stored step in that session is ticked.
// Claimed is true when this user already took the chest for that session.
type DailyQuestSession struct {
	Done    bool `json:"done"`
	Claimed bool `json:"claimed"`
}

// DailyQuestAmounts is the xu schedule the server will credit.
type DailyQuestAmounts struct {
	Morning int `json:"morning"`
	Evening int `json:"evening"`
	Bonus   int `json:"bonus"`
}

// DailyQuestAmountView copies the only payout constants into the API shape.
func DailyQuestAmountView() DailyQuestAmounts {
	return DailyQuestAmounts{
		Morning: domain.DailyQuestMorningCoins,
		Evening: domain.DailyQuestEveningCoins,
		Bonus:   domain.DailyQuestBonusCoins,
	}
}

// DailyQuestClaimResponse is POST /api/v1/mascot/daily-quest/claim.
// Granted is the session amount. Bonus is 0, or the both-sessions bonus
// when this call was the second claim of the care day. Balance is the
// wallet after this call. AlreadyClaimed is true when this session was
// already paid; the call does not credit again.
type DailyQuestClaimResponse struct {
	Granted        int    `json:"granted"`
	Bonus          int    `json:"bonus"`
	Balance        int64  `json:"balance"`
	AlreadyClaimed bool   `json:"already_claimed"`
	Session        string `json:"session"`
}
