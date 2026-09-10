package routine

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
)

const suggestJobTTL = 15 * time.Minute

// pollableSuggestFailMsg is what clients see on a failed poll. Never the
// provider/DB string — those stay in logs.
const pollableSuggestFailMsg = "could not generate a routine suggestion"

type suggestJobState struct {
	userID    uuid.UUID
	status    string // processing | completed | failed | cancelled
	req       dto.SuggestRoutineRequest
	result    dto.SuggestRoutineResponse
	errMsg    string
	cancelled atomic.Bool
	createdAt time.Time
}

var suggestJobs sync.Map // map[string]*suggestJobState — tests / no-repo fallback

func storeSuggestJob(id string, userID uuid.UUID, req dto.SuggestRoutineRequest) {
	suggestJobs.Store(id, &suggestJobState{
		userID:    userID,
		status:    "processing",
		req:       req,
		createdAt: time.Now(),
	})
	slog.Info("routine suggest: job created", "job_id", id, "user_id", userID)
}

func finishSuggestJob(id string, result dto.SuggestRoutineResponse) {
	raw, ok := suggestJobs.Load(id)
	if !ok {
		slog.Warn("routine suggest: complete miss", "job_id", id)
		return
	}
	job := raw.(*suggestJobState)
	if job.cancelled.Load() {
		job.status = "cancelled"
		slog.Info("routine suggest: late result discarded (cancelled)", "job_id", id, "user_id", job.userID)
		return
	}
	job.status = "completed"
	job.result = result
	slog.Info("routine suggest: job completed", "job_id", id, "user_id", job.userID)
}

func failSuggestJob(id string, msg string) {
	raw, ok := suggestJobs.Load(id)
	if !ok {
		slog.Warn("routine suggest: fail miss", "job_id", id)
		return
	}
	job := raw.(*suggestJobState)
	if job.cancelled.Load() {
		job.status = "cancelled"
		slog.Info("routine suggest: late failure discarded (cancelled)", "job_id", id, "user_id", job.userID)
		return
	}
	job.status = "failed"
	job.errMsg = strings.TrimSpace(msg)
	if job.errMsg == "" {
		job.errMsg = pollableSuggestFailMsg
	}
	slog.Warn("routine suggest: job failed", "job_id", id, "user_id", job.userID, "err", job.errMsg)
}

func cancelSuggestJob(id string) bool {
	raw, ok := suggestJobs.Load(id)
	if !ok {
		slog.Info("routine suggest: cancel miss (not found)", "job_id", id)
		return false
	}
	job := raw.(*suggestJobState)
	if job.status != "processing" {
		slog.Info("routine suggest: cancel noop (already terminal)", "job_id", id, "user_id", job.userID, "status", job.status)
		return true
	}
	job.cancelled.Store(true)
	job.status = "cancelled"
	slog.Info("routine suggest: job cancelled", "job_id", id, "user_id", job.userID)
	return true
}

func loadSuggestJob(jobID string) (*suggestJobState, bool) {
	id := strings.TrimSpace(jobID)
	if id == "" {
		return nil, false
	}
	raw, ok := suggestJobs.Load(id)
	if !ok {
		return nil, false
	}
	job := raw.(*suggestJobState)
	if time.Since(job.createdAt) > suggestJobTTL {
		suggestJobs.Delete(id)
		slog.Info("routine suggest: job expired", "job_id", id, "user_id", job.userID)
		return nil, false
	}
	return job, true
}

func marshalSuggestRequest(req dto.SuggestRoutineRequest) json.RawMessage {
	b, err := json.Marshal(req)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

func (s *Service) persistNewSuggestJob(ctx context.Context, jobID, userID uuid.UUID, req dto.SuggestRoutineRequest) error {
	if s == nil || s.suggestJobs == nil {
		return nil
	}
	job := &domain.RoutineSuggestJob{
		ID:        jobID,
		UserID:    userID,
		Status:    domain.SuggestJobProcessing,
		Request:   marshalSuggestRequest(req),
		ExpiresAt: time.Now().UTC().Add(suggestJobTTL),
	}
	return s.suggestJobs.Create(ctx, job)
}

func (s *Service) completePersistedSuggestJob(ctx context.Context, userID uuid.UUID, jobID string, result dto.SuggestRoutineResponse) {
	if s == nil || s.suggestJobs == nil {
		finishSuggestJob(jobID, result)
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(jobID))
	if err != nil || id == uuid.Nil {
		return
	}
	raw, err := json.Marshal(result)
	if err != nil {
		s.failPersistedSuggestJob(ctx, userID, jobID, pollableSuggestFailMsg)
		return
	}
	ok, err := s.suggestJobs.MarkTerminal(ctx, userID, id, domain.SuggestJobCompleted, raw, "")
	if err != nil {
		slog.Warn("routine suggest: persist complete failed", "job_id", jobID, "user_id", userID, "err", err)
		return
	}
	if !ok {
		slog.Info("routine suggest: late result discarded", "job_id", jobID, "user_id", userID)
		return
	}
	slog.Info("routine suggest: job completed", "job_id", jobID, "user_id", userID)
}

func (s *Service) failPersistedSuggestJob(ctx context.Context, userID uuid.UUID, jobID, msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = pollableSuggestFailMsg
	}
	if s == nil || s.suggestJobs == nil {
		failSuggestJob(jobID, msg)
		return
	}
	id, err := uuid.Parse(strings.TrimSpace(jobID))
	if err != nil || id == uuid.Nil {
		return
	}
	ok, err := s.suggestJobs.MarkTerminal(ctx, userID, id, domain.SuggestJobFailed, nil, msg)
	if err != nil {
		slog.Warn("routine suggest: persist fail failed", "job_id", jobID, "user_id", userID, "err", err)
		return
	}
	if !ok {
		slog.Info("routine suggest: late failure discarded", "job_id", jobID, "user_id", userID)
		return
	}
	slog.Warn("routine suggest: job failed", "job_id", jobID, "user_id", userID)
}

func (s *Service) cancelPersistedSuggestJob(ctx context.Context, userID uuid.UUID, jobID string) bool {
	if s == nil || s.suggestJobs == nil {
		return cancelSuggestJob(jobID)
	}
	id, err := uuid.Parse(strings.TrimSpace(jobID))
	if err != nil || id == uuid.Nil {
		return false
	}
	job, err := s.suggestJobs.GetByIDForUser(ctx, userID, id)
	if err != nil || job == nil || job.IsExpired(time.Now()) {
		slog.Info("routine suggest: cancel miss", "job_id", jobID, "user_id", userID)
		return false
	}
	if job.Status != domain.SuggestJobProcessing {
		return true
	}
	ok, err := s.suggestJobs.MarkTerminal(ctx, userID, id, domain.SuggestJobCancelled, nil, "")
	if err != nil {
		slog.Warn("routine suggest: persist cancel failed", "job_id", jobID, "user_id", userID, "err", err)
		return false
	}
	if ok {
		slog.Info("routine suggest: job cancelled", "job_id", jobID, "user_id", userID)
	}
	return true
}

func (s *Service) loadPersistedSuggestJob(
	ctx context.Context,
	userID uuid.UUID,
	jobID string,
) (dto.SuggestJobStatusResponse, bool, error) {
	var zero dto.SuggestJobStatusResponse
	id, err := uuid.Parse(strings.TrimSpace(jobID))
	if err != nil || id == uuid.Nil {
		return zero, false, nil
	}
	job, err := s.suggestJobs.GetByIDForUser(ctx, userID, id)
	if err != nil {
		return zero, false, err
	}
	if job == nil || job.IsExpired(time.Now()) {
		return zero, false, nil
	}
	out := dto.SuggestJobStatusResponse{
		JobID:  job.ID.String(),
		Status: string(job.Status),
	}
	if job.Status == domain.SuggestJobFailed {
		out.Error = strings.TrimSpace(job.ErrorMsg)
		if out.Error == "" {
			out.Error = pollableSuggestFailMsg
		}
	}
	if job.Status == domain.SuggestJobCompleted && len(job.Result) > 0 {
		var res dto.SuggestRoutineResponse
		if err := json.Unmarshal(job.Result, &res); err == nil {
			out.Suggestion = &res
		}
	}
	return out, true, nil
}
