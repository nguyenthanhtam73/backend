package dto

// RoomMemberCare is one person on a Bé Da room list or the diligence board.
// Nickname is the room persona. CareStreak is consecutive Asia/Ho_Chi_Minh
// days on which every stored routine step was ticked. Today counts when that
// day is complete; otherwise the run is counted through yesterday. The value
// is 0 when neither today nor yesterday is complete.
// No account id, email, glow level, or check-in streak is included.
type RoomMemberCare struct {
	Nickname   string `json:"nickname"`
	CareStreak int    `json:"care_streak"`
}

// RoomBoardResponse is GET /api/v1/mascot/rooms/:code/board.
// Members are sorted by care_streak descending, then nickname ascending
// (Unicode code point order). Equal nickname and streak keep join order.
type RoomBoardResponse struct {
	Members []RoomMemberCare `json:"members"`
}
