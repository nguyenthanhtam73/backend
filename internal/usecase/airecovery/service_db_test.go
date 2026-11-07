package airecovery

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	profileuc "github.com/dadiary/backend/internal/usecase/profile"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestListSelectsFailedPhotoChecksOldestFirst(t *testing.T) {
	db := openRecoveryDB(t)
	checks := repository.NewSkinCheckRepository(db)
	profiles := repository.NewSkinProfileRepository(db)
	since := domain.AIAnalysisOutageSince
	now := since.Add(3 * time.Hour)

	older := seedCheck(t, db, checks, since.Add(10*time.Minute), `["checks/older.jpg"]`, domain.AnalysisStatusFailed)
	_ = seedCheck(t, db, checks, since.Add(20*time.Minute), `["checks/done.jpg"]`, domain.AnalysisStatusCompleted)
	_ = seedCheck(t, db, checks, since.Add(30*time.Minute), `[]`, domain.AnalysisStatusFailed)
	_ = seedCheck(t, db, checks, since.Add(-time.Hour), `["checks/before.jpg"]`, domain.AnalysisStatusFailed)
	newer := seedCheck(t, db, checks, since.Add(40*time.Minute), `["checks/newer.jpg"," "]`, domain.AnalysisStatusFailed)
	fresh := seedCheck(t, db, checks, since.Add(50*time.Minute), `["checks/live.jpg"]`, domain.AnalysisStatusProcessing)
	if err := db.Model(&domain.SkinAnalysis{}).Where("skin_check_id = ?", fresh.ID).Update("updated_at", now.Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	stale := seedCheck(t, db, checks, since.Add(5*time.Minute), `["checks/stale.jpg"]`, domain.AnalysisStatusPending)
	if err := db.Model(&domain.SkinAnalysis{}).Where("skin_check_id = ?", stale.ID).Update("updated_at", now.Add(-InFlightGrace-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}

	proc := &fakeProcessor{}
	svc := NewService(checks, profiles, proc, nil)
	got, err := svc.defaultListChecks(context.Background(), since, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []uuid.UUID{stale.ID, older.ID, newer.ID}
	if len(got) != len(want) {
		t.Fatalf("selected %d checks %v, want %v", len(got), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order[%d]=%s, want %s (full %v)", i, got[i], want[i], got)
		}
	}
	if proc.calls != 0 {
		t.Fatal("listing must not call the provider")
	}
}

func TestReplayCheckSkipsCompletedAndIsIdempotent(t *testing.T) {
	db := openRecoveryDB(t)
	checks := repository.NewSkinCheckRepository(db)
	since := domain.AIAnalysisOutageSince
	done := seedCheck(t, db, checks, since.Add(time.Minute), `["checks/done.jpg"]`, domain.AnalysisStatusCompleted)
	failed := seedCheck(t, db, checks, since.Add(2*time.Minute), `["checks/failed.jpg"]`, domain.AnalysisStatusFailed)

	proc := &fakeProcessor{onCall: func(id uuid.UUID) {
		row, err := checks.GetByID(context.Background(), id)
		if err != nil || row == nil || row.Analysis == nil {
			t.Errorf("reload %s: %v", id, err)
			return
		}
		row.Analysis.Status = domain.AnalysisStatusCompleted
		row.Analysis.ErrorMessage = ""
		if err := checks.SaveAnalysis(context.Background(), row.Analysis); err != nil {
			t.Errorf("save %s: %v", id, err)
		}
	}}
	svc := NewService(checks, nil, proc, nil)

	skipped, err := svc.defaultReplayCheck(context.Background(), done.ID)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != OutcomeSkipped || proc.calls != 0 {
		t.Fatalf("completed check outcome=%v calls=%d", skipped, proc.calls)
	}

	first, err := svc.defaultReplayCheck(context.Background(), failed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first != OutcomeDone || proc.calls != 1 {
		t.Fatalf("first replay outcome=%v calls=%d", first, proc.calls)
	}
	second, err := svc.defaultReplayCheck(context.Background(), failed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second != OutcomeSkipped || proc.calls != 1 {
		t.Fatalf("second replay outcome=%v calls=%d, want skip without another provider call", second, proc.calls)
	}
}

func TestListSelectsScaffoldStartersOnly(t *testing.T) {
	db := openRecoveryDB(t)
	profiles := repository.NewSkinProfileRepository(db)
	since := domain.AIAnalysisOutageSince
	now := since.Add(2 * time.Hour)

	scaffoldUser := uuid.New()
	aiUser := uuid.New()
	editedUser := uuid.New()
	oldUser := uuid.New()
	pendingFresh := uuid.New()

	mustProfile(t, db, profiles, scaffoldUser, scaffoldSnap(profileuc.QuickScaffoldEncouragementVI), since.Add(time.Minute))
	mustProfile(t, db, profiles, aiUser, aiSnap(), since.Add(2*time.Minute))
	mustProfile(t, db, profiles, editedUser, editedSnap(), since.Add(3*time.Minute))
	mustProfile(t, db, profiles, oldUser, scaffoldSnap(profileuc.QuickScaffoldEncouragementEN), since.Add(-time.Hour))
	mustProfile(t, db, profiles, pendingFresh, pendingSnap(), now.Add(-time.Minute))

	svc := NewService(nil, profiles, nil, nil)
	got, err := svc.defaultListStarters(context.Background(), since, now, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != scaffoldUser {
		t.Fatalf("starters = %v, want only the scaffold user", got)
	}
}

type fakeProcessor struct {
	calls  int
	onCall func(uuid.UUID)
	err    error
}

func (f *fakeProcessor) Process(_ context.Context, id uuid.UUID, _ string) error {
	f.calls++
	if f.onCall != nil {
		f.onCall(id)
	}
	return f.err
}

func openRecoveryDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:airecovery_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.SkinCheck{}, &domain.SkinAnalysis{}, &domain.SkinProfile{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedCheck(t *testing.T, db *gorm.DB, repo *repository.GormSkinCheckRepository, created time.Time, images string, status domain.AnalysisStatus) *domain.SkinCheck {
	t.Helper()
	check := &domain.SkinCheck{
		UserID:     uuid.New(),
		ImageURLs:  json.RawMessage(images),
		Visibility: domain.CheckVisibilityPrivate,
		CheckDate:  created,
		CreatedAt:  created,
	}
	row := &domain.SkinAnalysis{Status: status, CreatedAt: created, UpdatedAt: created}
	if err := repo.CreateWithAnalysis(context.Background(), check, row); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.SkinCheck{}).Where("id = ?", check.ID).Update("created_at", created).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.SkinAnalysis{}).Where("skin_check_id = ?", check.ID).Updates(map[string]any{
		"created_at": created,
		"updated_at": created,
		"status":     status,
	}).Error; err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(context.Background(), check.ID)
	if err != nil || got == nil {
		t.Fatalf("reload: %v", err)
	}
	return got
}

func mustProfile(t *testing.T, db *gorm.DB, repo *repository.GormSkinProfileRepository, userID uuid.UUID, snap []byte, created time.Time) {
	t.Helper()
	p := &domain.SkinProfile{
		UserID:             userID,
		SkinType:           "combination",
		OnboardingSnapshot: snap,
		CreatedAt:          created,
		UpdatedAt:          created,
	}
	if err := repo.UpsertForUser(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.SkinProfile{}).Where("user_id = ?", userID).Updates(map[string]any{
		"created_at": created,
		"updated_at": created,
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func scaffoldSnap(encouragement string) []byte {
	b, _ := json.Marshal(map[string]any{
		"completed_via": "onboarding_v1",
		"locale":        "vi",
		"starter_routine": map[string]any{
			"encouragement": encouragement,
			"morning":       []string{"Rửa mặt dịu"},
			"evening":       []string{"Rửa mặt buổi tối"},
		},
	})
	return b
}

func aiSnap() []byte {
	b, _ := json.Marshal(map[string]any{
		"completed_via":          "onboarding_v1",
		"starter_routine_source": profileuc.StarterRoutineSourceAI,
		"starter_routine": map[string]any{
			"encouragement": "Da bạn hôm nay cần dịu.",
			"morning":       []string{"Rửa mặt"},
			"skin_readback": "Da hỗn hợp.",
		},
	})
	return b
}

func editedSnap() []byte {
	b, _ := json.Marshal(map[string]any{
		"completed_via":       "onboarding_v1",
		"starter_user_edited": true,
		"starter_routine": map[string]any{
			"encouragement": profileuc.QuickScaffoldEncouragementVI,
			"morning":       []string{"Bước của mình"},
		},
	})
	return b
}

func pendingSnap() []byte {
	b, _ := json.Marshal(map[string]any{
		"completed_via":           "onboarding_v1",
		"starter_routine_pending": true,
		"starter_routine": map[string]any{
			"encouragement": profileuc.QuickScaffoldEncouragementVI,
			"morning":       []string{"Rửa mặt dịu"},
		},
	})
	return b
}
