package analysis

import (
	"context"
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

func TestClaimAnalysisRun_OnlyOneWinner(t *testing.T) {
	db := openClaimDB(t)
	repo := repository.NewSkinCheckRepository(db)
	check := seedClaimCheck(t, db, true)

	const n = 8
	var wg sync.WaitGroup
	wins := make(chan bool, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, ok, err := repo.ClaimAnalysisRun(context.Background(), check.ID, time.Now().UTC())
			if err != nil {
				t.Errorf("claim: %v", err)
				return
			}
			wins <- ok
		}()
	}
	wg.Wait()
	close(wins)
	got := 0
	for ok := range wins {
		if ok {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("winners = %d, want 1", got)
	}
}

func TestProcess_MissingAnalysisIsCreatedAndSecondCallerLosesClaim(t *testing.T) {
	db := openClaimDB(t)
	repo := repository.NewSkinCheckRepository(db)
	check := seedClaimCheck(t, db, false)
	svc := New(&config.Config{}, repo, nil, nil, nil, nil, nil, nil)

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	blockAfterClaim = func() {
		entered <- struct{}{}
		<-release
	}
	t.Cleanup(func() { blockAfterClaim = nil })

	var first error
	done := make(chan struct{})
	go func() {
		first = svc.Process(context.Background(), check.ID, "")
		close(done)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first process did not claim")
	}
	second := svc.Process(context.Background(), check.ID, "")
	close(release)
	<-done

	if second != ErrAnalysisBusy {
		t.Fatalf("second process err = %v, want ErrAnalysisBusy", second)
	}
	reloaded, err := repo.GetByID(context.Background(), check.ID)
	if err != nil || reloaded == nil || reloaded.Analysis == nil {
		t.Fatalf("missing analysis row was not created: %v", err)
	}
	if reloaded.Analysis.Status == "" {
		t.Fatal("analysis status empty")
	}
	_ = first
}

func TestFinishAnalysisIfClaim_IgnoresStaleToken(t *testing.T) {
	db := openClaimDB(t)
	repo := repository.NewSkinCheckRepository(db)
	check := seedClaimCheck(t, db, true)
	token, won, err := repo.ClaimAnalysisRun(context.Background(), check.ID, time.Now().UTC())
	if err != nil || !won || token == "" {
		t.Fatalf("claim won=%v token=%q err=%v", won, token, err)
	}
	reloaded, err := repo.GetByID(context.Background(), check.ID)
	if err != nil || reloaded == nil || reloaded.Analysis == nil {
		t.Fatal(err)
	}
	if !reloaded.Analysis.OutageAffected {
		t.Fatal("claim in the apology window should set outage_affected")
	}
	stale := *reloaded.Analysis
	stale.Status = domain.AnalysisStatusFailed
	stale.ErrorMessage = "stale worker"
	stale.SummaryNotes = "should not land"
	ok, err := repo.FinishAnalysisIfClaim(context.Background(), &stale, "run:someone-else")
	if err != nil || ok {
		t.Fatalf("stale finish ok=%v err=%v", ok, err)
	}
	fresh := *reloaded.Analysis
	fresh.Status = domain.AnalysisStatusCompleted
	fresh.ModelVersion = "pipeline=test"
	fresh.SummaryNotes = "new worker"
	now := time.Now().UTC()
	fresh.AnalyzedAt = &now
	ok, err = repo.FinishAnalysisIfClaim(context.Background(), &fresh, token)
	if err != nil || !ok {
		t.Fatalf("owner finish ok=%v err=%v", ok, err)
	}
	ok, err = repo.FailAnalysisIfClaim(context.Background(), check.ID, token, "too late")
	if err != nil || ok {
		t.Fatalf("old token should not mark failed after completion, ok=%v err=%v", ok, err)
	}
	got, err := repo.GetByID(context.Background(), check.ID)
	if err != nil || got == nil || got.Analysis == nil {
		t.Fatal(err)
	}
	if got.Analysis.Status != domain.AnalysisStatusCompleted || got.Analysis.SummaryNotes != "new worker" {
		t.Fatalf("row overwritten: status=%s notes=%q", got.Analysis.Status, got.Analysis.SummaryNotes)
	}
	if !got.Analysis.OutageAffected {
		t.Fatal("completion cleared outage_affected")
	}
}

func TestExpireStaleAnalysis_KeepsTokenAndOutageFlag(t *testing.T) {
	db := openClaimDB(t)
	repo := repository.NewSkinCheckRepository(db)
	check := seedClaimCheck(t, db, true)
	expiredAt := time.Now().UTC().Add(-10 * time.Minute)
	if err := db.Exec(`
UPDATE skin_analyses
SET status = ?, model_version = ?, outage_affected = ?, updated_at = ?, error_message = ''
WHERE skin_check_id = ?`,
		domain.AnalysisStatusProcessing, "run:old", true, expiredAt, check.ID,
	).Error; err != nil {
		t.Fatal(err)
	}
	polled, err := repo.GetByID(context.Background(), check.ID)
	if err != nil || polled == nil || polled.Analysis == nil {
		t.Fatal(err)
	}
	if !ExpireStaleAnalysis(context.Background(), repo, polled.Analysis) {
		t.Fatal("unchanged stale row should be marked failed")
	}
	got, err := repo.GetByID(context.Background(), check.ID)
	if err != nil || got == nil || got.Analysis == nil {
		t.Fatal(err)
	}
	if got.Analysis.Status != domain.AnalysisStatusFailed {
		t.Fatalf("status = %s", got.Analysis.Status)
	}
	if got.Analysis.ModelVersion != "run:old" {
		t.Fatalf("token changed to %q", got.Analysis.ModelVersion)
	}
	if !got.Analysis.OutageAffected {
		t.Fatal("expire cleared outage_affected")
	}
}

func TestExpireStaleAnalysis_LosesToReplayClaim(t *testing.T) {
	db := openClaimDB(t)
	repo := repository.NewSkinCheckRepository(db)
	check := seedClaimCheck(t, db, true)
	expiredAt := time.Now().UTC().Add(-10 * time.Minute)
	if err := db.Exec(`
UPDATE skin_analyses
SET status = ?, model_version = ?, outage_affected = ?, updated_at = ?, error_message = ''
WHERE skin_check_id = ?`,
		domain.AnalysisStatusProcessing, "run:old", true, expiredAt, check.ID,
	).Error; err != nil {
		t.Fatal(err)
	}
	polled, err := repo.GetByID(context.Background(), check.ID)
	if err != nil || polled == nil || polled.Analysis == nil {
		t.Fatal(err)
	}
	seen := *polled.Analysis

	replayToken, won, err := repo.ClaimAnalysisRun(context.Background(), check.ID, time.Now().UTC())
	if err != nil || !won {
		t.Fatalf("replay claim won=%v err=%v", won, err)
	}
	if replayToken == "" || replayToken == "run:old" {
		t.Fatalf("replay token = %q", replayToken)
	}
	if ExpireStaleAnalysis(context.Background(), repo, &seen) {
		t.Fatal("poller failure write should affect 0 rows")
	}
	got, err := repo.GetByID(context.Background(), check.ID)
	if err != nil || got == nil || got.Analysis == nil {
		t.Fatal(err)
	}
	if got.Analysis.ModelVersion != replayToken {
		t.Fatalf("token = %q, want replay token %q", got.Analysis.ModelVersion, replayToken)
	}
	if !got.Analysis.OutageAffected {
		t.Fatal("poller cleared outage_affected")
	}
	if got.Analysis.Status != domain.AnalysisStatusProcessing {
		t.Fatalf("status = %s, want processing", got.Analysis.Status)
	}
	if got.Analysis.ErrorMessage != "" {
		t.Fatalf("error_message = %q", got.Analysis.ErrorMessage)
	}
}

func openClaimDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:claim_"+uuid.NewString()+"?mode=memory&cache=shared&_pragma=busy_timeout(5000)"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&domain.SkinCheck{}, &domain.SkinAnalysis{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedClaimCheck(t *testing.T, db *gorm.DB, withAnalysis bool) *domain.SkinCheck {
	t.Helper()
	created := domain.OutageApologyStart.Add(time.Hour)
	check := &domain.SkinCheck{
		UserID:     uuid.New(),
		ImageURLs:  []byte(`["checks/a.jpg"]`),
		Visibility: domain.CheckVisibilityPrivate,
		CheckDate:  created,
		CreatedAt:  created,
	}
	if err := db.Create(check).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE skin_checks SET created_at = ? WHERE id = ?`, created, check.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !withAnalysis {
		return check
	}
	row := &domain.SkinAnalysis{
		SkinCheckID: check.ID,
		Status:      domain.AnalysisStatusFailed,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	return check
}
