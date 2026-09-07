package skincheck

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type recordingEnqueuer struct {
	mu    sync.Mutex
	ids   []uuid.UUID
	after func(uuid.UUID)
}

func (r *recordingEnqueuer) EnqueueAnalysis(id uuid.UUID) {
	r.mu.Lock()
	r.ids = append(r.ids, id)
	r.mu.Unlock()
	if r.after != nil {
		r.after(id)
	}
}

func (r *recordingEnqueuer) calls() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]uuid.UUID, len(r.ids))
	copy(out, r.ids)
	return out
}

func setupReanalyzeSvc(t *testing.T, enq *recordingEnqueuer) (*Service, *repository.GormSkinCheckRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:reanalyze_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.SkinCheck{}, &domain.SkinAnalysis{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewSkinCheckRepository(db)
	svc := NewService(&config.Config{}, repo, nil, enq, nil, nil, nil)
	return svc, repo
}

func seedCheck(t *testing.T, repo *repository.GormSkinCheckRepository, owner uuid.UUID, images json.RawMessage, status domain.AnalysisStatus) *domain.SkinCheck {
	t.Helper()
	checkDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	check := &domain.SkinCheck{
		UserID:     owner,
		Title:      "morning",
		ImageURLs:  images,
		Visibility: domain.CheckVisibilityPrivate,
		CheckDate:  checkDate,
	}
	analyzed := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	row := &domain.SkinAnalysis{
		Status:       status,
		SummaryNotes: "original",
		ModelVersion: "v-original",
	}
	if status == domain.AnalysisStatusCompleted {
		row.AnalyzedAt = &analyzed
	}
	if err := repo.CreateWithAnalysis(context.Background(), check, row); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(context.Background(), check.ID)
	if err != nil || got == nil {
		t.Fatalf("reload seed: %v", err)
	}
	return got
}

func TestReanalyze_OwnerOnly(t *testing.T) {
	enq := &recordingEnqueuer{}
	svc, repo := setupReanalyzeSvc(t, enq)
	owner := uuid.New()
	other := uuid.New()
	check := seedCheck(t, repo, owner, json.RawMessage(`["checks/a.jpg"]`), domain.AnalysisStatusCompleted)

	_, err := svc.Reanalyze(context.Background(), other, check.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if n := len(enq.calls()); n != 0 {
		t.Fatalf("enqueued %d jobs for non-owner", n)
	}

	res, err := svc.Reanalyze(context.Background(), owner, check.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Check.ID != check.ID.String() {
		t.Fatalf("check id changed: %s", res.Check.ID)
	}
	if res.Check.CheckDate != "2026-09-01" {
		t.Fatalf("check_date mutated: %s", res.Check.CheckDate)
	}
	if len(res.ImageURLs) != 1 || res.ImageURLs[0] != "/uploads/checks/a.jpg" {
		t.Fatalf("photos mutated: %v", res.ImageURLs)
	}
	if res.Analysis.Status != string(domain.AnalysisStatusProcessing) {
		t.Fatalf("status=%s want processing", res.Analysis.Status)
	}
	if n := len(enq.calls()); n != 1 {
		t.Fatalf("enqueued %d jobs, want 1", n)
	}
}

func TestReanalyze_NoPhotos(t *testing.T) {
	enq := &recordingEnqueuer{}
	svc, repo := setupReanalyzeSvc(t, enq)
	owner := uuid.New()
	empty := seedCheck(t, repo, owner, json.RawMessage(`[]`), domain.AnalysisStatusCompleted)
	_, err := svc.Reanalyze(context.Background(), owner, empty.ID)
	if !errors.Is(err, ErrNoPhotos) {
		t.Fatalf("empty images: want ErrNoPhotos, got %v", err)
	}

	skip := seedCheck(t, repo, owner, json.RawMessage(`["", "  "]`), domain.AnalysisStatusCompleted)
	_, err = svc.Reanalyze(context.Background(), owner, skip.ID)
	if !errors.Is(err, ErrNoPhotos) {
		t.Fatalf("blank images: want ErrNoPhotos, got %v", err)
	}
	if n := len(enq.calls()); n != 0 {
		t.Fatalf("enqueued %d jobs without photos", n)
	}
}

func TestReanalyze_ProcessingIdempotent(t *testing.T) {
	enq := &recordingEnqueuer{}
	svc, repo := setupReanalyzeSvc(t, enq)
	owner := uuid.New()
	check := seedCheck(t, repo, owner, json.RawMessage(`["checks/a.jpg"]`), domain.AnalysisStatusCompleted)

	first, err := svc.Reanalyze(context.Background(), owner, check.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Reanalyze(context.Background(), owner, check.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Analysis.Status != string(domain.AnalysisStatusProcessing) || second.Analysis.Status != string(domain.AnalysisStatusProcessing) {
		t.Fatalf("statuses first=%s second=%s", first.Analysis.Status, second.Analysis.Status)
	}
	if n := len(enq.calls()); n != 1 {
		t.Fatalf("enqueued %d jobs, want 1 (no double job)", n)
	}
}

func TestReanalyze_DailyLimitAndFailedRetry(t *testing.T) {
	enq := &recordingEnqueuer{}
	svc, repo := setupReanalyzeSvc(t, enq)
	owner := uuid.New()
	check := seedCheck(t, repo, owner, json.RawMessage(`["checks/a.jpg"]`), domain.AnalysisStatusCompleted)

	if _, err := svc.Reanalyze(context.Background(), owner, check.ID); err != nil {
		t.Fatal(err)
	}
	// Simulate the job finishing the same UTC day.
	got, err := repo.GetByID(context.Background(), check.ID)
	if err != nil || got == nil || got.Analysis == nil {
		t.Fatal(err)
	}
	got.Analysis.Status = domain.AnalysisStatusCompleted
	got.Analysis.SummaryNotes = "pass 1"
	if err := repo.SaveAnalysis(context.Background(), got.Analysis); err != nil {
		t.Fatal(err)
	}

	_, err = svc.Reanalyze(context.Background(), owner, check.ID)
	if !errors.Is(err, ErrReanalyzeLimit) {
		t.Fatalf("want ErrReanalyzeLimit, got %v", err)
	}
	if n := len(enq.calls()); n != 1 {
		t.Fatalf("limit still enqueued, calls=%d", n)
	}

	// A failed pipeline may retry the same day (do not lock the user out).
	got, err = repo.GetByID(context.Background(), check.ID)
	if err != nil {
		t.Fatal(err)
	}
	got.Analysis.Status = domain.AnalysisStatusFailed
	got.Analysis.ErrorMessage = "timeout"
	if err := repo.SaveAnalysis(context.Background(), got.Analysis); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Reanalyze(context.Background(), owner, check.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Analysis.Status != string(domain.AnalysisStatusProcessing) {
		t.Fatalf("failed retry status=%s", res.Analysis.Status)
	}
	if n := len(enq.calls()); n != 2 {
		t.Fatalf("failed retry did not enqueue, calls=%d", n)
	}
}

func TestReanalyze_ReplacesAnalysisSameCheck(t *testing.T) {
	owner := uuid.New()
	var repo *repository.GormSkinCheckRepository
	enq := &recordingEnqueuer{
		after: func(id uuid.UUID) {
			got, err := repo.GetByID(context.Background(), id)
			if err != nil || got == nil || got.Analysis == nil {
				return
			}
			got.Analysis.Status = domain.AnalysisStatusCompleted
			got.Analysis.SummaryNotes = "soi lai"
			got.Analysis.ModelVersion = "v-reanalyzed"
			now := time.Now().UTC()
			got.Analysis.AnalyzedAt = &now
			_ = repo.SaveAnalysis(context.Background(), got.Analysis)
		},
	}
	svc, repo := setupReanalyzeSvc(t, enq)
	check := seedCheck(t, repo, owner, json.RawMessage(`["checks/a.jpg"]`), domain.AnalysisStatusCompleted)
	origAnalysisID := check.Analysis.ID
	origDate := check.CheckDate

	res, err := svc.Reanalyze(context.Background(), owner, check.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Check.ID != check.ID.String() {
		t.Fatalf("check id changed")
	}
	if res.Analysis.ID != origAnalysisID.String() {
		t.Fatalf("analysis row replaced: %s vs %s", res.Analysis.ID, origAnalysisID)
	}
	if res.Analysis.Status != string(domain.AnalysisStatusCompleted) {
		t.Fatalf("status=%s", res.Analysis.Status)
	}
	if res.Analysis.Coach == nil || res.Analysis.Coach.SummaryNotes != "soi lai" {
		t.Fatalf("analysis not replaced: %+v", res.Analysis.Coach)
	}
	got, err := repo.GetByID(context.Background(), check.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CheckDate.Equal(origDate) {
		t.Fatalf("check_date mutated")
	}
}

func TestClaimReanalyze_ConcurrentSingleWinner(t *testing.T) {
	_, repo := setupReanalyzeSvc(t, &recordingEnqueuer{})
	owner := uuid.New()
	check := seedCheck(t, repo, owner, json.RawMessage(`["checks/a.jpg"]`), domain.AnalysisStatusCompleted)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	wins := make(chan bool, n)
	now := time.Now().UTC()
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ok, err := repo.ClaimReanalyze(context.Background(), check.ID, now)
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			wins <- ok
		}()
	}
	wg.Wait()
	close(wins)
	claimed := 0
	for ok := range wins {
		if ok {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed=%d want 1", claimed)
	}
}

func TestHasStoredPhotos(t *testing.T) {
	if hasStoredPhotos(nil) || hasStoredPhotos([]byte(`[]`)) || hasStoredPhotos([]byte(`[" "]`)) {
		t.Fatal("expected no photos")
	}
	if !hasStoredPhotos([]byte(`["a.jpg"]`)) {
		t.Fatal("expected photo")
	}
}
