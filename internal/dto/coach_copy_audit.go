package dto

// AdminCoachCopyAuditResponse is GET /api/v1/admin/coach-copy-audit.
// Counts only. No note text and no user ids.
type AdminCoachCopyAuditResponse struct {
	Days                   int                         `json:"days"`
	ByClientKind           []AdminCoachCopyClientCount `json:"by_client_kind"`
	NotesMatchingScoreLeak int                         `json:"notes_matching_score_leak"`
	NotesMatchingTaoMay    int                         `json:"notes_matching_tao_may"`
}

// AdminCoachCopyClientCount is one stored client_kind in the window.
type AdminCoachCopyClientCount struct {
	ClientKind      string `json:"client_kind"`
	ChecksCompleted int    `json:"checks_completed"`
	DistinctUsers   int    `json:"distinct_users"`
}
