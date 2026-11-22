package dto

// RoomMascotLook is the equipped Bé Da. It is the cosmetic loadout only.
type RoomMascotLook struct {
	Body        string   `json:"body"`
	Hair        string   `json:"hair"`
	Outfit      string   `json:"outfit"`
	Accessory   string   `json:"accessory"`
	Accessories []string `json:"accessories"`
	RoomItems   []string `json:"room_items"`
	Gender      string   `json:"gender,omitempty"`
}

// RoomQuestMilestoneJSON is one chest on GET /mascot/rooms/:code/quest.
type RoomQuestMilestoneJSON struct {
	MemberDays int    `json:"member_days"`
	Coins      int    `json:"coins"`
	Tier       string `json:"tier"`
	Reached    bool   `json:"reached"`
	Claimed    bool   `json:"claimed"`
	Claimable  bool   `json:"claimable"`
}

// RoomQuestResponse is the shared 8-day quest bar for the caller.
type RoomQuestResponse struct {
	Progress      int                      `json:"progress"`
	SpanDays      int                      `json:"span_days"`
	MemberCount   int                      `json:"member_count"`
	MaxMemberDays int                      `json:"max_member_days"`
	VNToday       string                   `json:"vn_today"`
	Milestones    []RoomQuestMilestoneJSON `json:"milestones"`
}

// RoomQuestClaimResponse is POST /mascot/rooms/:code/quest/claim.
// already_claimed is true when this member already opened that chest.
// balance is the coin wallet after this call and does not increase twice.
type RoomQuestClaimResponse struct {
	MemberDays     int    `json:"member_days"`
	Coins          int    `json:"coins"`
	Tier           string `json:"tier"`
	Balance        int64  `json:"balance"`
	AlreadyClaimed bool   `json:"already_claimed"`
	Progress       int    `json:"progress"`
}

// RoomTodayMember is one row on the Hôm nay board.
// DisplayName is the account name, or the room nickname when that name is blank.
// Look is the mascot. CaredToday / CheckedToday are the Vietnam civil day only.
type RoomTodayMember struct {
	UserID       string         `json:"user_id"`
	DisplayName  string         `json:"display_name"`
	Look         RoomMascotLook `json:"look"`
	CaredToday   bool           `json:"cared_today"`
	CheckedToday bool           `json:"checked_today"`
}

// RoomTodayResponse is GET /mascot/rooms/:code/today.
type RoomTodayResponse struct {
	VNDate  string            `json:"vn_date"`
	Members []RoomTodayMember `json:"members"`
}

// RoomNudgeResponse is POST /mascot/rooms/:code/nudge.
// Message is chosen on the server. Push is "sent" or "skipped".
type RoomNudgeResponse struct {
	UserID  string `json:"user_id"`
	Message string `json:"message"`
	VNDate  string `json:"vn_date"`
	Push    string `json:"push"`
}
