package dto

import "github.com/dadiary/backend/internal/domain"

// MascotResponse is GET /api/v1/me/mascot, PUT /api/v1/me/mascot/loadout,
// and POST /api/v1/me/mascot/glow/seen.
// Mood and lines are chosen on the server so app and web match.
// Skin marks are independent of the missed-day mood.
type MascotResponse struct {
	Loadout       domain.MascotLoadout `json:"loadout"`
	Mood          string               `json:"mood"`
	LineVI        string               `json:"line_vi"`
	LineEN        string               `json:"line_en"`
	SkinMarks     []MascotSkinMark     `json:"skin_marks"`
	SkinMarksPrev []MascotSkinMark     `json:"skin_marks_prev"`
	LastCheckInAt *string              `json:"last_checkin_at"`
	RoutineToday  MascotRoutineToday   `json:"routine_today"`
	LastStepDone  *string              `json:"last_step_done"`
	StreakDays    int                  `json:"streak_days"`
	MissedDays    int                  `json:"missed_days"`
	MessyLevel    int                  `json:"messy_level"`
	// GlowLevel is max(stored high water, level from glow points). It does not fall.
	// GlowPoints is the total from stored check-ins and finished routines.
	// NextLevelPoints is the point total that starts the next level. Null at the top.
	// GlowProgress is glow_points / next_level_points, clamped to 0..1. It is 1 at the top.
	// NearLevelUp is true when GlowProgress is at least 80% and a next level exists.
	// LevelUp is true when GlowLevel is above the level last acknowledged.
	// SkinMarks are the latest check-in only. Glow points do not change them.
	GlowLevel       int     `json:"glow_level"`
	GlowPoints      int     `json:"glow_points"`
	NextLevelPoints *int    `json:"next_level_points"`
	GlowProgress    float64 `json:"glow_progress"`
	NearLevelUp     bool    `json:"near_level_up"`
	LevelUp         bool    `json:"level_up"`
}

// MascotSkinMark is one concern from a check-in. Level is light, medium, or heavy.
type MascotSkinMark struct {
	ID    string `json:"id"`
	Level string `json:"level"`
}

// MascotRoutineToday counts today's morning and evening checklist.
type MascotRoutineToday struct {
	AMDone  int `json:"am_done"`
	AMTotal int `json:"am_total"`
	PMDone  int `json:"pm_done"`
	PMTotal int `json:"pm_total"`
}
