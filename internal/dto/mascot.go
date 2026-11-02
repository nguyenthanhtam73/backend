package dto

import "github.com/dadiary/backend/internal/domain"

// MascotResponse is GET /api/v1/me/mascot and PUT /api/v1/me/mascot/loadout.
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
