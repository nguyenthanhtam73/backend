package dto

// AdminActivityResponse is GET /api/v1/admin/activity?date=YYYY-MM-DD
// (Vietnam civil day; defaults to today).
type AdminActivityResponse struct {
	Date              string                    `json:"date"`
	CheckInCount      int                       `json:"check_in_count"`
	CheckInPhotoCount int                       `json:"check_in_photo_count"`
	ProductUsageCount int                       `json:"product_usage_count"`
	CheckIns          []AdminActivityCheckIn    `json:"check_ins"`
	ProductUsage      []AdminActivityProductUse `json:"product_usage"`
}

// AdminActivityCheckIn is one skin check-in on the requested day.
type AdminActivityCheckIn struct {
	UserID      string   `json:"user_id"`
	Username    string   `json:"username"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name,omitempty"`
	CheckID     string   `json:"check_id"`
	HasPhotos   bool     `json:"has_photos"`
	PhotoCount  int      `json:"photo_count"`
	PhotoURLs   []string `json:"photo_urls"`
	CreatedAt   string   `json:"created_at"`
}

// AdminActivityProductUse is a routine row where the user ticked at least one step.
type AdminActivityProductUse struct {
	UserID        string   `json:"user_id"`
	Username      string   `json:"username"`
	Email         string   `json:"email"`
	DisplayName   string   `json:"display_name,omitempty"`
	MorningTicked int      `json:"morning_ticked"`
	EveningTicked int      `json:"evening_ticked"`
	TickedTitles  []string `json:"ticked_titles"`
	UpdatedAt     string   `json:"updated_at"`
}
