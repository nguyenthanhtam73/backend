// Package dto — routine.go defines the public API shapes for the Routine
// Management feature (GET / POST / history / AI suggest).
package dto

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// RoutineStep is one ordered step inside a Morning or Evening routine.
//
// `ID` is a client-stable string (the frontend generates a UUID per step so it
// can reorder/edit before any server round-trip). `Category` is an open string
// (e.g. "cleanser", "toner", "serum", "moisturizer", "spf", "treatment", "other")
// — the backend does not enum-validate it because skincare vocabulary evolves
// fast and we'd rather let the AI/user pick than reject valid input.
type RoutineStep struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Category  string `json:"category,omitempty"`
	Notes     string `json:"notes,omitempty"`
	Completed bool   `json:"completed,omitempty"`
}

// RoutineResponse is what GET /routines returns and what POST /routines
// reflects back. `Saved` is false when there is no row for today yet (we
// return the latest "template" carried forward so the UI can render the user's
// usual steps with all `completed:false`).
type RoutineResponse struct {
	ID          string        `json:"id,omitempty"`
	UserID      string        `json:"user_id"`
	RoutineDate string        `json:"routine_date"`
	Morning     []RoutineStep `json:"morning"`
	Evening     []RoutineStep `json:"evening"`
	Notes       string        `json:"notes,omitempty"`
	Source      string        `json:"source,omitempty"`
	SkillMode   string        `json:"skill_mode,omitempty"`
	// CarriedFromDate is the original routine_date when today's row does not
	// exist yet and we fall back to the latest saved entry (saved=false).
	CarriedFromDate string `json:"carried_from_date,omitempty"`
	// Saved is true once today's row exists in the DB (i.e. user persisted at
	// least once today). Front-end uses this to switch the header copy
	// between "Routine của bạn (đã lưu)" and "Routine gợi ý cho hôm nay".
	Saved     bool   `json:"saved"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// PutRoutineRequest is sent by POST /routines (create or update today).
type PutRoutineRequest struct {
	RoutineDate string        `json:"routine_date,omitempty"` // optional ISO date; defaults to "today UTC"
	Morning     []RoutineStep `json:"morning"`
	Evening     []RoutineStep `json:"evening"`
	Notes       string        `json:"notes,omitempty"`
	Source      string        `json:"source,omitempty"`     // "manual" | "ai_suggested" | "carried_over"
	SkillMode   string        `json:"skill_mode,omitempty"` // beginner | intermediate | advanced
	// SaveKind lets the server distinguish completion ticks from structural edits.
	// tick_only → free users may autosave ticks without consuming manual-edit quota.
	SaveKind string `json:"save_kind,omitempty"` // tick_only | manual_edit
}

// SuggestRoutineRequest is sent by POST /routines/suggest. Body is optional —
// when empty, the backend pulls SkinProfile + last skin check from the DB.
type SuggestRoutineRequest struct {
	// Optional hint from the UI (lets the user re-roll a different mode without
	// touching SkinProfile permanently).
	SkillMode string `json:"skill_mode,omitempty"`
	// Locale ("vi" | "en") so the AI writes in the same language as the app shell.
	Locale string `json:"locale,omitempty"`
	// Optional free-text "hôm nay da căng, dùng đỡ activ nhé" — added to the prompt.
	FocusNote string `json:"focus_note,omitempty"`
}

// SuggestRoutineResponse returns AI-suggested AM/PM steps plus the supportive
// copy the coach generates (week_notes / safety_notes / encouragement, etc.).
// It does NOT persist anything — the user can preview the suggestion, tweak,
// and then save via POST /routines.
type SuggestRoutineResponse struct {
	Morning            []RoutineStep         `json:"morning"`
	Evening            []RoutineStep         `json:"evening"`
	Encouragement      string                `json:"encouragement,omitempty"`
	Rationale          string                `json:"rationale,omitempty"`
	WeekNotes          string                `json:"week_notes,omitempty"`
	SafetyNotes        string                `json:"safety_notes,omitempty"`
	ClosingReminder    string                `json:"closing_reminder,omitempty"`
	ProductSuggestions []ProductSuggestion   `json:"product_suggestions,omitempty"`
	SkillMode          string                `json:"skill_mode,omitempty"`
	Locale             string                `json:"locale,omitempty"`
	// Source is always "ai_suggested" — present so the UI can label the card.
	Source string `json:"source"`
	// FeedbackTargetID — fresh per-call UUID the frontend uses when posting
	// thumbs-up/down votes against this transient suggestion (POST
	// /ai/feedback with target_type="suggested_routine"). Suggestions are
	// not persisted server-side; this id only exists so feedback rows can
	// be uniquely traced.
	FeedbackTargetID string `json:"feedback_target_id,omitempty"`
}

// SuggestJobCreatedResponse is returned immediately by POST /routines/suggest.
type SuggestJobCreatedResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"` // processing
}

// SuggestJobStatusResponse is returned by GET /routines/suggest/status.
type SuggestJobStatusResponse struct {
	JobID      string                  `json:"job_id"`
	Status     string                  `json:"status"` // processing | completed | failed | cancelled
	Error      string                  `json:"error,omitempty"`
	Suggestion *SuggestRoutineResponse `json:"suggestion,omitempty"`
}

// RoutineHistoryResponse is the payload for GET /routines/history.
type RoutineHistoryResponse struct {
	RangeDays int               `json:"range_days"`
	From      string            `json:"from,omitempty"`
	To        string            `json:"to,omitempty"`
	Entries   []RoutineResponse `json:"entries"`
	// StreakDays counts consecutive days (ending today) where at least one step
	// in either AM or PM was ticked complete. Used by the UI for the small
	// "🔥 N days" badge.
	StreakDays int `json:"streak_days"`
	// CompletionAvg is the average completion ratio (0–1) across all entries
	// in the window — i.e. (completed steps) / (total steps). Useful for the
	// summary card and the future progress chart.
	CompletionAvg float64 `json:"completion_avg"`
}

// RoutineFromDomain converts a DB row into the public response.
// Pass `saved=true` when the row exists for today. When the caller fakes a
// "carried over" response from an older row, set `saved=false` and the source
// will be reported as "carried_over" regardless of what is stored.
func RoutineFromDomain(r *domain.RoutineEntry, saved bool) RoutineResponse {
	if r == nil {
		return RoutineResponse{
			Morning: []RoutineStep{},
			Evening: []RoutineStep{},
			Saved:   false,
		}
	}
	morning := decodeRoutineSteps(r.Morning)
	evening := decodeRoutineSteps(r.Evening)

	source := r.Source
	if !saved {
		source = "carried_over"
		for i := range morning {
			morning[i].Completed = false
		}
		for i := range evening {
			evening[i].Completed = false
		}
	}

	return RoutineResponse{
		ID:          r.ID.String(),
		UserID:      r.UserID.String(),
		RoutineDate: r.RoutineDate.UTC().Format("2006-01-02"),
		Morning:     morning,
		Evening:     evening,
		Notes:       r.Notes,
		Source:      source,
		SkillMode:   r.SkillMode,
		Saved:       saved,
		UpdatedAt:   r.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// EmptyRoutineResponse is the "nothing on file" projection used when the user
// has never saved a routine. Returns a stable shape so the frontend can render
// the empty-state without null-checks.
func EmptyRoutineResponse(userID uuid.UUID) RoutineResponse {
	now := time.Now().UTC()
	return RoutineResponse{
		UserID:      userID.String(),
		RoutineDate: now.Format("2006-01-02"),
		Morning:     []RoutineStep{},
		Evening:     []RoutineStep{},
		Saved:       false,
	}
}

func decodeRoutineSteps(raw json.RawMessage) []RoutineStep {
	if len(raw) == 0 {
		return []RoutineStep{}
	}
	var steps []RoutineStep
	if err := json.Unmarshal(raw, &steps); err != nil || steps == nil {
		// Backward-compatible decode: starter_routine stores morning/evening as []string.
		var lines []string
		if err := json.Unmarshal(raw, &lines); err == nil {
			out := make([]RoutineStep, 0, len(lines))
			for i, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				out = append(out, RoutineStep{
					ID:    deterministicStepID(i, line),
					Title: line,
				})
			}
			return out
		}
		return []RoutineStep{}
	}
	out := make([]RoutineStep, 0, len(steps))
	for i, s := range steps {
		title := strings.TrimSpace(s.Title)
		if title == "" {
			continue
		}
		id := strings.TrimSpace(s.ID)
		if id == "" {
			id = deterministicStepID(i, title)
		}
		out = append(out, RoutineStep{
			ID:        id,
			Title:     title,
			Category:  strings.TrimSpace(s.Category),
			Notes:     strings.TrimSpace(s.Notes),
			Completed: s.Completed,
		})
	}
	return out
}

// deterministicStepID derives a stable id when the stored JSON predates the
// `id` field (legacy []string starter routines). We use UUIDv5 over a fixed
// namespace + (index|title) so the same legacy bullet always maps to the same
// id between requests — the frontend's React keys stay stable.
func deterministicStepID(index int, title string) string {
	ns := uuid.MustParse("9d9b3a1e-1bb1-4d51-9c25-1f0d12345678")
	seed := strings.ToLower(strings.TrimSpace(title))
	if seed == "" {
		seed = "step"
	}
	return uuid.NewSHA1(ns, []byte(seed+"|"+strconv.Itoa(index))).String()
}
