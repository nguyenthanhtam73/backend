package profile

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
)

const previewJobTTL = 15 * time.Minute

// PreviewJobStore persists guest preview jobs (Postgres) so multi-instance
// deploys can poll reliably. Access requires the secret token minted at create.
type PreviewJobStore = repository.OnboardingPreviewJobRepository

func newPreviewJobID() uuid.UUID {
	return uuid.New()
}

func newPreviewAccessToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashPreviewToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func checkPreviewToken(storedHash, raw string) bool {
	want := hashPreviewToken(raw)
	return subtle.ConstantTimeCompare([]byte(storedHash), []byte(want)) == 1
}

func marshalStarter(starter dto.StarterRoutineResponse) (json.RawMessage, error) {
	b, err := json.Marshal(starter)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func unmarshalStarter(raw json.RawMessage) (dto.StarterRoutineResponse, error) {
	var out dto.StarterRoutineResponse
	if len(raw) == 0 {
		return out, fmt.Errorf("empty starter")
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	return out, nil
}

// createPreviewJob persists a pending job and returns (id, plaintext access token).
func (s *Service) createPreviewJob(ctx context.Context, starter dto.StarterRoutineResponse) (id uuid.UUID, accessToken string, err error) {
	if s == nil || s.previewJobs == nil {
		return uuid.Nil, "", fmt.Errorf("%w: preview job store unavailable", ErrUnavailable)
	}
	token, err := newPreviewAccessToken()
	if err != nil {
		return uuid.Nil, "", err
	}
	starterJSON, err := marshalStarter(starter)
	if err != nil {
		return uuid.Nil, "", err
	}
	id = newPreviewJobID()
	job := &domain.OnboardingPreviewJob{
		ID:          id,
		AccessToken: hashPreviewToken(token),
		Status:      domain.PreviewJobPending,
		StarterJSON: starterJSON,
		ExpiresAt:   time.Now().UTC().Add(previewJobTTL),
	}
	if err := s.previewJobs.Create(ctx, job); err != nil {
		return uuid.Nil, "", err
	}
	return id, token, nil
}

func (s *Service) finishPreviewJob(ctx context.Context, id uuid.UUID, starter dto.StarterRoutineResponse) {
	if s == nil || s.previewJobs == nil || id == uuid.Nil {
		return
	}
	job, err := s.previewJobs.GetByID(ctx, id)
	if err != nil || job == nil {
		return
	}
	starterJSON, err := marshalStarter(starter)
	if err != nil {
		return
	}
	job.Status = domain.PreviewJobReady
	job.StarterJSON = starterJSON
	_ = s.previewJobs.Update(ctx, job)
}

func (s *Service) failPreviewJob(ctx context.Context, id uuid.UUID, fallback dto.StarterRoutineResponse) {
	if s == nil || s.previewJobs == nil || id == uuid.Nil {
		return
	}
	job, err := s.previewJobs.GetByID(ctx, id)
	if err != nil || job == nil {
		return
	}
	starterJSON, err := marshalStarter(fallback)
	if err != nil {
		return
	}
	job.Status = domain.PreviewJobFailed
	job.StarterJSON = starterJSON
	_ = s.previewJobs.Update(ctx, job)
}

// GetPreviewRoutineJob returns the current guest preview job when the access token matches.
func (s *Service) GetPreviewRoutineJob(
	ctx context.Context,
	jobID, accessToken string,
) (dto.OnboardingPreviewPollResponse, bool, error) {
	var zero dto.OnboardingPreviewPollResponse
	if s == nil || s.previewJobs == nil {
		return zero, false, fmt.Errorf("%w", ErrUnavailable)
	}
	id, err := uuid.Parse(strings.TrimSpace(jobID))
	if err != nil || id == uuid.Nil {
		return zero, false, nil
	}
	token := strings.TrimSpace(accessToken)
	if token == "" {
		return zero, false, nil
	}

	job, err := s.previewJobs.GetByID(ctx, id)
	if err != nil {
		return zero, false, err
	}
	if job == nil {
		return zero, false, nil
	}
	now := time.Now().UTC()
	if job.IsExpired(now) {
		_ = s.previewJobs.Delete(ctx, id)
		return zero, false, nil
	}
	if !checkPreviewToken(job.AccessToken, token) {
		// Wrong token — treat as not found (no existence oracle).
		return zero, false, nil
	}

	starter, err := unmarshalStarter(job.StarterJSON)
	if err != nil {
		return zero, false, err
	}
	pending := job.Status == domain.PreviewJobPending
	return dto.OnboardingPreviewPollResponse{
		StarterRoutine:        starter,
		StarterRoutinePending: pending,
	}, true, nil
}
