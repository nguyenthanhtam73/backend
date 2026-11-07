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
			ok, err := repo.ClaimAnalysisRun(context.Background(), check.ID, time.Now().UTC())
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
	check := &domain.SkinCheck{
		UserID:     uuid.New(),
		ImageURLs:  []byte(`["checks/a.jpg"]`),
		Visibility: domain.CheckVisibilityPrivate,
		CheckDate:  time.Now().UTC(),
	}
	if err := db.Create(check).Error; err != nil {
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
