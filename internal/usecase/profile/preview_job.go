package profile

import (
	"strings"
	"sync"
	"time"

	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
)

const previewJobTTL = 15 * time.Minute

type previewJobState struct {
	pending   bool
	failed    bool
	starter   dto.StarterRoutineResponse
	createdAt time.Time
}

var previewJobs sync.Map // map[string]*previewJobState

func newPreviewJobID() string {
	return uuid.NewString()
}

func storePreviewJob(id string, starter dto.StarterRoutineResponse) {
	previewJobs.Store(id, &previewJobState{
		pending:   true,
		starter:   starter,
		createdAt: time.Now(),
	})
}

func finishPreviewJob(id string, starter dto.StarterRoutineResponse) {
	previewJobs.Store(id, &previewJobState{
		pending:   false,
		starter:   starter,
		createdAt: time.Now(),
	})
}

func failPreviewJob(id string, fallback dto.StarterRoutineResponse) {
	previewJobs.Store(id, &previewJobState{
		pending:   false,
		failed:    true,
		starter:   fallback,
		createdAt: time.Now(),
	})
}

// GetPreviewRoutineJob returns the current guest preview job state.
func (s *Service) GetPreviewRoutineJob(jobID string) (dto.OnboardingPreviewPollResponse, bool, error) {
	var zero dto.OnboardingPreviewPollResponse
	id := strings.TrimSpace(jobID)
	if id == "" {
		return zero, false, nil
	}
	raw, ok := previewJobs.Load(id)
	if !ok {
		return zero, false, nil
	}
	job := raw.(*previewJobState)
	if time.Since(job.createdAt) > previewJobTTL {
		previewJobs.Delete(id)
		return zero, false, nil
	}
	return dto.OnboardingPreviewPollResponse{
		StarterRoutine:        job.starter,
		StarterRoutinePending: job.pending,
	}, true, nil
}
