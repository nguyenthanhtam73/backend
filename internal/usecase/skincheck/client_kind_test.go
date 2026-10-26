package skincheck

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/google/uuid"
)

func TestCreate_PersistsClientKindForBackgroundJob(t *testing.T) {
	enq := &recordingEnqueuer{}
	svc, repo := setupReanalyzeSvc(t, enq)
	owner := uuid.New()

	res, err := svc.Create(context.Background(), owner, CreateInput{
		UserNote:   "má hơi đỏ",
		SkipMode:   true,
		ClientKind: "  android  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(enq.calls()) != 1 {
		t.Fatalf("enqueue calls=%d", len(enq.calls()))
	}
	// The request has returned. The job only has the row.
	got, err := repo.GetByID(context.Background(), enq.calls()[0])
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.ClientKind != domain.RefreshClientAndroid {
		t.Fatalf("stored client_kind=%q", got.ClientKind)
	}
	if res.Check.ID != got.ID.String() {
		t.Fatalf("response id %s row %s", res.Check.ID, got.ID)
	}
	prompt := ai.GetCoachPromptForClient("beginner", got.ClientKind)
	if strings.Contains(prompt, "Đm da mày hôm nay") || !strings.Contains(prompt, "mình / bạn") {
		t.Fatal("reloaded android row did not select the polite prompt")
	}

	webRes, err := svc.Create(context.Background(), owner, CreateInput{
		UserNote: "da ổn",
		SkipMode: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	webID, err := uuid.Parse(webRes.Check.ID)
	if err != nil {
		t.Fatal(err)
	}
	webRow, err := repo.GetByID(context.Background(), webID)
	if err != nil || webRow == nil {
		t.Fatal(err)
	}
	if webRow.ClientKind != domain.RefreshClientWeb {
		t.Fatalf("default client_kind=%q", webRow.ClientKind)
	}
	if ai.GetCoachPromptForClient("intermediate", webRow.ClientKind) != ai.GetCoachPrompt("intermediate") {
		t.Fatal("web row must keep today's prompt")
	}
}

func TestReanalyze_VoiceUpgradeDoesNotDowngrade(t *testing.T) {
	enq := &recordingEnqueuer{}
	svc, repo := setupReanalyzeSvc(t, enq)
	owner := uuid.New()
	images := json.RawMessage(`["checks/a.jpg"]`)

	webCheck := seedCheck(t, repo, owner, images, domain.AnalysisStatusCompleted)
	if _, err := svc.Reanalyze(context.Background(), owner, webCheck.ID, "android"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(context.Background(), webCheck.ID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.ClientKind != domain.RefreshClientAndroid {
		t.Fatalf("android reanalyze stored %q", got.ClientKind)
	}
	if ai.GetCoachPromptForClient("beginner", got.ClientKind) == ai.GetCoachPrompt("beginner") {
		t.Fatal("upgraded row still selects the web prompt")
	}

	androidCheck := seedCheck(t, repo, owner, images, domain.AnalysisStatusCompleted)
	if err := repo.SetClientKind(context.Background(), androidCheck.ID, domain.RefreshClientAndroid); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reanalyze(context.Background(), owner, androidCheck.ID, ""); err != nil {
		t.Fatal(err)
	}
	kept, err := repo.GetByID(context.Background(), androidCheck.ID)
	if err != nil || kept == nil {
		t.Fatal(err)
	}
	if kept.ClientKind != domain.RefreshClientAndroid {
		t.Fatalf("web reanalyze downgraded to %q", kept.ClientKind)
	}

	for _, kind := range []string{"Android", "dadiary-android", "web"} {
		plain := seedCheck(t, repo, owner, images, domain.AnalysisStatusCompleted)
		if _, err := svc.Reanalyze(context.Background(), owner, plain.ID, kind); err != nil {
			t.Fatal(kind, err)
		}
		row, err := repo.GetByID(context.Background(), plain.ID)
		if err != nil || row == nil {
			t.Fatal(err)
		}
		if row.ClientKind == domain.RefreshClientAndroid {
			t.Fatalf("kind %q upgraded the check", kind)
		}
	}
}

func TestReanalyze_InFlightAndroidHeaderUpgradesBeforeLoad(t *testing.T) {
	enq := &recordingEnqueuer{}
	svc, repo := setupReanalyzeSvc(t, enq)
	owner := uuid.New()
	check := seedCheck(t, repo, owner, json.RawMessage(`["checks/a.jpg"]`), domain.AnalysisStatusProcessing)
	// Keep the row fresh so stale-expiry does not flip it to failed.
	check.Analysis.UpdatedAt = time.Now().UTC()
	if err := repo.SaveAnalysis(context.Background(), check.Analysis); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Reanalyze(context.Background(), owner, check.ID, "  android  "); err != nil {
		t.Fatal(err)
	}
	if len(enq.calls()) != 0 {
		t.Fatalf("in-flight reanalyze enqueued %d jobs", len(enq.calls()))
	}
	got, err := repo.GetByID(context.Background(), check.ID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.ClientKind != domain.RefreshClientAndroid {
		t.Fatalf("in-flight upgrade stored %q", got.ClientKind)
	}
}
