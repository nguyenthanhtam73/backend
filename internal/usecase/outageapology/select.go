package outageapology

import (
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// Recipient is one user who should receive the apology, plus the check to link
// when a result is already available.
type Recipient struct {
	UserID         uuid.UUID
	Email          string
	CompletedCheck uuid.UUID // set when any in-window check is now completed
	NeedsRecheck   bool
}

// SelectRecipients keeps distinct users who have a skin check in [start, end]
// whose analysis is missing or not completed. @example.com and deleted users
// are dropped. If the user also has a completed check in the window, the latest
// one is the "has result" link; otherwise the mail asks them to check in again.
func SelectRecipients(checks []domain.SkinCheck, start, end time.Time) []Recipient {
	type acc struct {
		email       string
		order       time.Time
		failed      bool
		completed   uuid.UUID
		completedAt time.Time
	}
	byUser := map[uuid.UUID]*acc{}
	var order []uuid.UUID
	for _, check := range checks {
		created := check.CreatedAt.UTC()
		if created.Before(start) || created.After(end) {
			continue
		}
		if check.User.ID == uuid.Nil || check.User.DeletedAt.Valid {
			continue
		}
		email := strings.TrimSpace(check.User.Email)
		if email == "" || isExampleEmail(email) {
			continue
		}
		row := byUser[check.UserID]
		if row == nil {
			row = &acc{email: email, order: created}
			byUser[check.UserID] = row
			order = append(order, check.UserID)
		}
		if created.Before(row.order) {
			row.order = created
		}
		if check.Analysis == nil || check.Analysis.Status != domain.AnalysisStatusCompleted {
			row.failed = true
			continue
		}
		if row.completed == uuid.Nil || created.After(row.completedAt) {
			row.completed = check.ID
			row.completedAt = created
		}
	}
	out := make([]Recipient, 0, len(order))
	for _, id := range order {
		row := byUser[id]
		if !row.failed {
			continue
		}
		out = append(out, Recipient{
			UserID:         id,
			Email:          row.email,
			CompletedCheck: row.completed,
			NeedsRecheck:   row.completed == uuid.Nil,
		})
	}
	return out
}

func isExampleEmail(email string) bool {
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(email)), "@example.com")
}
