package routine

import (
	"context"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPersistedSuggestJob_StatusAndCancel(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:suggest_job_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.RoutineEntry{}, &domain.RoutineSuggestJob{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	jobs := repository.NewRoutineSuggestJobRepository(db)
	routines := repository.NewRoutineEntryRepository(db)
	svc := NewService(nil, routines, nil, nil, nil, nil, nil, nil)
	svc.AttachSuggestJobs(jobs)

	uid := uuid.New()
	jobID := uuid.New()
	ctx := context.Background()
	if err := jobs.Create(ctx, &domain.RoutineSuggestJob{
		ID:        jobID,
		UserID:    uid,
		Status:    domain.SuggestJobProcessing,
		Request:   []byte(`{"locale":"vi"}`),
		ExpiresAt: time.Now().UTC().Add(15 * time.Minute),
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	st, ok, err := svc.GetSuggestJobStatus(ctx, uid, jobID.String())
	if err != nil || !ok {
		t.Fatalf("status: ok=%v err=%v", ok, err)
	}
	if st.Status != "processing" || st.Suggestion != nil {
		t.Fatalf("processing payload: %+v", st)
	}

	other := uuid.New()
	if _, ok, err := svc.GetSuggestJobStatus(ctx, other, jobID.String()); err != nil || ok {
		t.Fatalf("other user must not see job: ok=%v err=%v", ok, err)
	}

	if !svc.CancelSuggestJob(ctx, uid, jobID.String()) {
		t.Fatal("cancel")
	}
	st, ok, err = svc.GetSuggestJobStatus(ctx, uid, jobID.String())
	if err != nil || !ok {
		t.Fatalf("status after cancel: ok=%v err=%v", ok, err)
	}
	if st.Status != "cancelled" {
		t.Fatalf("want cancelled, got %q", st.Status)
	}
}
