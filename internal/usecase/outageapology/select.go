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

// SelectRecipients keeps distinct users who had a skin check in [start, end]
// that failed or never completed. outage_affected keeps them after a replay
// marks the row completed. The template follows the current result: a completed
// check in the window gets the "has result" link; otherwise the mail asks them
// to check in again.
//
// Test accounts dropped here are only domains that are clearly not customers:
// example.com / example.org / example.net, example.test, test.com, dadiary.test,
// localhost, and any .test TLD. There is no qa- local-part convention in this
// repo, so those addresses are kept.
func SelectRecipients(checks []domain.SkinCheck, start, end time.Time) []Recipient {
	type acc struct {
		email       string
		affected    bool
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
		if email == "" || isTestEmail(email) {
			continue
		}
		row := byUser[check.UserID]
		if row == nil {
			row = &acc{email: email}
			byUser[check.UserID] = row
			order = append(order, check.UserID)
		}
		if analysisWasAffected(check.Analysis) {
			row.affected = true
		}
		if check.Analysis != nil && check.Analysis.Status == domain.AnalysisStatusCompleted {
			if row.completed == uuid.Nil || !created.Before(row.completedAt) {
				row.completed = check.ID
				row.completedAt = created
			}
		}
	}
	out := make([]Recipient, 0, len(order))
	for _, id := range order {
		row := byUser[id]
		if !row.affected {
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

// analysisWasAffected reports a check that did not finish at the time of the
// outage, including one later replayed (outage_affected stays set).
func analysisWasAffected(a *domain.SkinAnalysis) bool {
	if a == nil {
		return true
	}
	if a.OutageAffected {
		return true
	}
	return a.Status != domain.AnalysisStatusCompleted
}

func isTestEmail(email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return true
	}
	domain := email[at+1:]
	switch domain {
	case "example.com", "example.org", "example.net", "example.test", "test.com", "dadiary.test", "localhost":
		return true
	}
	if strings.HasSuffix(domain, ".example.com") || strings.HasSuffix(domain, ".example.org") || strings.HasSuffix(domain, ".example.net") {
		return true
	}
	if strings.HasSuffix(domain, ".test") || strings.HasSuffix(domain, ".localhost") {
		return true
	}
	return false
}
