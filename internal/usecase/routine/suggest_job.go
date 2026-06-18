package routine

import (
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
)

const suggestJobTTL = 15 * time.Minute

type suggestJobState struct {
	userID    uuid.UUID
	status    string // processing | completed | failed | cancelled
	req       dto.SuggestRoutineRequest
	result    dto.SuggestRoutineResponse
	errMsg    string
	cancelled atomic.Bool
	createdAt time.Time
}

var suggestJobs sync.Map // map[string]*suggestJobState

func newSuggestJobID() string {
	return uuid.NewString()
}

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
