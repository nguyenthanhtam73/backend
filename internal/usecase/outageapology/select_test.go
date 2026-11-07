package outageapology

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestSelectRecipients(t *testing.T) {
	start, end := WindowStart, WindowEnd
	failedUser := uuid.New()
	fixedUser := uuid.New()
	okUser := uuid.New()
	exampleUser := uuid.New()
	deletedUser := uuid.New()
	outsideUser := uuid.New()

	failedCheck := uuid.New()
	olderFailed := uuid.New()
	completed := uuid.New()

	rows := []domain.SkinCheck{
		check(failedUser, "a@dadiary.vn", failedCheck, start.Add(time.Hour), domain.AnalysisStatusFailed, true),
		check(fixedUser, "b@dadiary.vn", olderFailed, start.Add(2*time.Hour), domain.AnalysisStatusFailed, true),
		check(fixedUser, "b@dadiary.vn", completed, start.Add(3*time.Hour), domain.AnalysisStatusCompleted, true),
		check(okUser, "c@dadiary.vn", uuid.New(), start.Add(time.Hour), domain.AnalysisStatusCompleted, true),
		check(exampleUser, "d@example.com", uuid.New(), start.Add(time.Hour), domain.AnalysisStatusFailed, true),
		check(uuid.New(), "qa-person@gmail.com", uuid.New(), start.Add(time.Hour), domain.AnalysisStatusFailed, true),
		check(uuid.New(), "seed@test.com", uuid.New(), start.Add(time.Hour), domain.AnalysisStatusFailed, true),
		check(deletedUser, "e@dadiary.vn", uuid.New(), start.Add(time.Hour), domain.AnalysisStatusFailed, true),
		check(outsideUser, "f@dadiary.vn", uuid.New(), end.Add(time.Second), domain.AnalysisStatusFailed, true),
		check(failedUser, "a@dadiary.vn", uuid.New(), start, "", false),
	}
	rows[7].User.DeletedAt.Valid = true
	rows[7].User.ID = deletedUser

	repairedUser := uuid.New()
	repairedCheck := uuid.New()
	repaired := check(repairedUser, "repaired@dadiary.vn", repairedCheck, start.Add(4*time.Hour), domain.AnalysisStatusCompleted, true)
	repaired.Analysis.OutageAffected = true
	rows = append(rows, repaired)

	got := SelectRecipients(rows, start, end)
	if len(got) != 4 {
		t.Fatalf("recipients = %d, want 4 (failed, mixed, qa-gmail, repaired)", len(got))
	}
	if got[0].UserID != failedUser || !got[0].NeedsRecheck || got[0].CompletedCheck != uuid.Nil {
		t.Fatalf("failed user = %+v", got[0])
	}
	if got[1].UserID != fixedUser || got[1].NeedsRecheck || got[1].CompletedCheck != completed {
		t.Fatalf("mixed user = %+v", got[1])
	}
	var repairedGot *Recipient
	for i := range got {
		if got[i].UserID == repairedUser {
			repairedGot = &got[i]
		}
	}
	if repairedGot == nil || repairedGot.NeedsRecheck || repairedGot.CompletedCheck != repairedCheck {
		t.Fatalf("replayed user = %+v", repairedGot)
	}
}

func check(userID uuid.UUID, email string, id uuid.UUID, created time.Time, status domain.AnalysisStatus, withAnalysis bool) domain.SkinCheck {
	row := domain.SkinCheck{
		ID:        id,
		UserID:    userID,
		CreatedAt: created,
		User:      domain.User{ID: userID, Email: email},
	}
	if withAnalysis {
		row.Analysis = &domain.SkinAnalysis{Status: status, SkinCheckID: id}
	}
	return row
}
