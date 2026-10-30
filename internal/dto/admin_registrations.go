package dto

// AdminRegistrationsBySourceResponse is GET /api/v1/admin/stats/registrations-by-source.
//
// Counts only. Days with no signups are omitted. A day lists every bucket,
// including zeros, so a chart can read one object per civil date.
type AdminRegistrationsBySourceResponse struct {
	// Timezone is the IANA zone used to bucket created_at. Default Asia/Ho_Chi_Minh.
	Timezone string `json:"timezone"`
	// Since is the inclusive civil start date (YYYY-MM-DD) in Timezone.
	Since string `json:"since"`
	// Days is ascending by Date. Empty when nobody registered in the window.
	Days []AdminRegistrationSourceDay `json:"days"`
}

// AdminRegistrationSourceDay is one civil date of signup counts.
//
// TikTok is ttclid set, or utm_source containing "tiktok" (case-insensitive).
// That bucket is decided before Facebook, so a row that matches both is TikTok.
// Facebook is fbclid set, or utm_source exactly fb, facebook, ig, or instagram
// (trimmed, case-insensitive). Other is any other non-empty utm_source.
// None is everyone else, including utm_medium / campaign / content with no source.
type AdminRegistrationSourceDay struct {
	Date     string `json:"date"`
	TikTok   int64  `json:"tiktok"`
	Facebook int64  `json:"facebook"`
	Other    int64  `json:"other"`
	None     int64  `json:"none"`
}
