package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// CoachCopyClientCount is completed checks and distinct owners for one client_kind.
type CoachCopyClientCount struct {
	ClientKind      string
	ChecksCompleted int
	DistinctUsers   int
}

// CoachCopyText is the stored coach prose for one completed check.
// It has no user id. The caller counts leaks and discards the text.
type CoachCopyText struct {
	SummaryNotes string
	Strengths    string
	Improvements string
	RoutineHints string
	AvoidOrPatch string
	SafetyFlags  string
	SkinScores   string
}

// CoachCopyAudit reads aggregate coach-copy stats. It only runs SELECT.
type CoachCopyAudit struct {
	db *gorm.DB
}

// NewCoachCopyAudit wires the shared database handle.
func NewCoachCopyAudit(db *gorm.DB) *CoachCopyAudit {
	return &CoachCopyAudit{db: db}
}

const coachCopyAuditFrom = `
FROM skin_analyses AS a
INNER JOIN skin_checks AS sc ON sc.id = a.skin_check_id
INNER JOIN users AS u ON u.id = sc.user_id
WHERE a.status = ?
  AND a.deleted_at IS NULL
  AND sc.deleted_at IS NULL
  AND u.deleted_at IS NULL
  AND sc.created_at >= ?
  AND LOWER(TRIM(COALESCE(u.email, ''))) NOT LIKE ?
`

// CountCompletedByClient groups completed checks in the window.
// Emails ending in @example.com are excluded.
func (r *CoachCopyAudit) CountCompletedByClient(ctx context.Context, since time.Time) ([]CoachCopyClientCount, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var rows []CoachCopyClientCount
	err := r.db.WithContext(ctx).Raw(`
SELECT COALESCE(NULLIF(TRIM(sc.client_kind), ''), 'web') AS client_kind,
       COUNT(*) AS checks_completed,
       COUNT(DISTINCT sc.user_id) AS distinct_users
`+coachCopyAuditFrom+`
GROUP BY COALESCE(NULLIF(TRIM(sc.client_kind), ''), 'web')
ORDER BY client_kind
`, "completed", since, "%@example.com").Scan(&rows).Error
	return rows, err
}

// ListCompletedCoachText returns stored coach prose for the same window.
// The SELECT list is coach text only.
func (r *CoachCopyAudit) ListCompletedCoachText(ctx context.Context, since time.Time) ([]CoachCopyText, error) {
	if r == nil || r.db == nil {
		return nil, gorm.ErrInvalidDB
	}
	var rows []CoachCopyText
	err := r.db.WithContext(ctx).Raw(`
SELECT COALESCE(a.summary_notes, '') AS summary_notes,
       COALESCE(CAST(a.strengths AS TEXT), '') AS strengths,
       COALESCE(CAST(a.improvements AS TEXT), '') AS improvements,
       COALESCE(CAST(a.routine_hints AS TEXT), '') AS routine_hints,
       COALESCE(CAST(a.avoid_or_patch AS TEXT), '') AS avoid_or_patch,
       COALESCE(CAST(a.safety_flags AS TEXT), '') AS safety_flags,
       COALESCE(CAST(a.skin_scores AS TEXT), '') AS skin_scores
`+coachCopyAuditFrom, "completed", since, "%@example.com").Scan(&rows).Error
	return rows, err
}
