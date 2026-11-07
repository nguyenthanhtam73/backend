package profile

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSnapshotNeedsAIStarter(t *testing.T) {
	scaffold := snap(t, map[string]any{
		"completed_via": "onboarding_v1",
		"starter_routine": map[string]any{
			"encouragement": QuickScaffoldEncouragementVI,
			"morning":       []string{"Rửa mặt dịu"},
		},
	})
	if !SnapshotNeedsAIStarter(scaffold) {
		t.Fatal("scaffold should need an AI starter")
	}
	en := snap(t, map[string]any{
		"completed_via": "onboarding_v1",
		"starter_routine": map[string]any{
			"encouragement": QuickScaffoldEncouragementEN,
			"morning":       []string{"Gentle cleanser"},
		},
	})
	if !SnapshotNeedsAIStarter(en) {
		t.Fatal("english scaffold should need an AI starter")
	}
	edited := snap(t, map[string]any{
		"completed_via":       "onboarding_v1",
		"starter_user_edited": true,
		"starter_routine": map[string]any{
			"encouragement": QuickScaffoldEncouragementVI,
			"morning":       []string{"My step"},
		},
	})
	if SnapshotNeedsAIStarter(edited) {
		t.Fatal("user-edited steps must be skipped")
	}
	done := snap(t, map[string]any{
		"completed_via":          "onboarding_v1",
		"starter_routine_source": StarterRoutineSourceAI,
		"starter_routine": map[string]any{
			"encouragement": "Một câu của mô hình.",
			"morning":       []string{"Rửa mặt"},
			"skin_readback": "Da hỗn hợp.",
		},
	})
	if SnapshotNeedsAIStarter(done) {
		t.Fatal("source=ai must be skipped")
	}
	modelText := snap(t, map[string]any{
		"completed_via": "onboarding_v1",
		"starter_routine": map[string]any{
			"encouragement": "Mình thấy da đang cần dịu lại.",
			"morning":       []string{"Rửa mặt"},
			"skin_readback": "Da hỗn hợp, mục tiêu giảm mụn.",
		},
	})
	if SnapshotNeedsAIStarter(modelText) {
		t.Fatal("a model encouragement is already a starter routine")
	}
	guest := snap(t, map[string]any{
		"completed_via": "onboarding_guest_preview",
		"starter_routine": map[string]any{
			"encouragement": QuickScaffoldEncouragementVI,
		},
	})
	if SnapshotNeedsAIStarter(guest) {
		t.Fatal("guest previews are not persisted user routines")
	}

	now := time.Now().UTC()
	pending := snap(t, map[string]any{
		"completed_via":           "onboarding_v1",
		"starter_routine_pending": true,
		"starter_routine": map[string]any{
			"encouragement": QuickScaffoldEncouragementVI,
		},
	})
	if StarterReplayCandidate(pending, now.Add(-time.Minute), now) {
		t.Fatal("a fresh pending refresh is in flight")
	}
	if !StarterReplayCandidate(pending, now.Add(-StarterInFlightGrace-time.Second), now) {
		t.Fatal("a stale pending refresh should be replayed")
	}
}

func TestReplayMissingStarter_Idempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:starter_replay_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.SkinProfile{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewSkinProfileRepository(db)
	userID := uuid.New()
	raw := snap(t, map[string]any{
		"completed_via": "onboarding_v1",
		"locale":        "vi",
		"skin_type":     "combination",
		"goal":          "clear_acne",
		"starter_routine": map[string]any{
			"encouragement": QuickScaffoldEncouragementVI,
			"morning":       []string{"Rửa mặt dịu"},
			"evening":       []string{"Rửa mặt buổi tối"},
		},
	})
	if err := repo.UpsertForUser(context.Background(), &domain.SkinProfile{
		UserID:             userID,
		SkinType:           "combination",
		OnboardingSnapshot: raw,
	}); err != nil {
		t.Fatal(err)
	}

	var calls int
	svc := NewService(nil, repo, nil, nil, nil, nil, nil)
	svc.starterGen = func(context.Context, []byte, string, string) (ai.StarterRoutine, error) {
		calls++
		return ai.StarterRoutine{
			Encouragement: "Da đang cần dịu, mình đi chậm thôi.",
			SkinReadback:  "Da hỗn hợp, mục tiêu giảm mụn.",
			Morning:       []string{"Rửa mặt dịu — bụi cả ngày."},
			Evening:       []string{"Dưỡng nhẹ — da đỡ căng."},
			SafetyNotes:   "Không thay thế bác sĩ.",
		}, nil
	}

	if err := svc.ReplayMissingStarter(context.Background(), userID); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("generator calls = %d, want 1", calls)
	}
	stored, err := repo.GetByUserID(context.Background(), userID)
	if err != nil || stored == nil {
		t.Fatal(err)
	}
	if SnapshotNeedsAIStarter(stored.OnboardingSnapshot) {
		t.Fatalf("snapshot should no longer need a starter: %s", stored.OnboardingSnapshot)
	}
	err = svc.ReplayMissingStarter(context.Background(), userID)
	if !errors.Is(err, ErrStarterPresent) {
		t.Fatalf("second replay err = %v, want ErrStarterPresent", err)
	}
	if calls != 1 {
		t.Fatalf("second replay called the generator, calls=%d", calls)
	}
}

func snap(t *testing.T, v map[string]any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
