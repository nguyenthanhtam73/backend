package dto

// ExportUserDataResponse is GET /api/v1/me/export — a portable diary dump (Premium+).
type ExportUserDataResponse struct {
	ExportedAt string `json:"exported_at"`
	PlanTier   string `json:"plan_tier"`
	UserID     string `json:"user_id"`

	Profile    *ExportSkinProfile   `json:"profile,omitempty"`
	Streak     *StreakResponse      `json:"streak,omitempty"`
	SkinChecks []ExportSkinCheck    `json:"skin_checks"`
	Routines   []ExportRoutineDay   `json:"routines"`
	Wardrobe   []ExportWardrobeItem `json:"wardrobe"`
}

// ExportSkinProfile is a privacy-safe profile snapshot for export.
type ExportSkinProfile struct {
	SkinType        string   `json:"skin_type,omitempty"`
	Undertone       string   `json:"undertone,omitempty"`
	Concerns        []string `json:"concerns,omitempty"`
	Goals           []string `json:"goals,omitempty"`
	SensitivityNotes string  `json:"sensitivity_notes,omitempty"`
}

// ExportSkinCheck is one check-in row without raw image bytes.
type ExportSkinCheck struct {
	ID         string   `json:"id"`
	CheckDate  string   `json:"check_date"`
	Title      string   `json:"title,omitempty"`
	UserNote   string   `json:"user_note,omitempty"`
	Conditions []string `json:"conditions,omitempty"`
	Symptoms   []string `json:"symptoms,omitempty"`
	ImageURLs  []string `json:"image_urls,omitempty"`
	CoachNotes string   `json:"coach_notes,omitempty"`
}

// ExportRoutineDay is one day's AM/PM routine snapshot.
type ExportRoutineDay struct {
	Date    string `json:"date"`
	Morning any    `json:"morning,omitempty"`
	Evening any    `json:"evening,omitempty"`
	Notes   string `json:"notes,omitempty"`
}

// ExportWardrobeItem is one shelf product.
type ExportWardrobeItem struct {
	Name      string `json:"name"`
	Brand     string `json:"brand,omitempty"`
	Category  string `json:"category,omitempty"`
	OpenedAt  string `json:"opened_at,omitempty"`
	Notes     string `json:"notes,omitempty"`
}
