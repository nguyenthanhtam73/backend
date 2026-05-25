package domain

// ClimateSnapshot is JSON stored on a SkinCheck (or sent from clients) for optional environmental
// context—region-agnostic ISO fields so we can plug weather/UV APIs later without schema churn.
type ClimateSnapshot struct {
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
	CountryCode  string   `json:"country_code,omitempty"` // ISO 3166-1 alpha-2
	Locality     string   `json:"locality,omitempty"`
	HumidityPct  *int     `json:"humidity_pct,omitempty"`
	UVIndex      *float64 `json:"uv_index,omitempty"`
	TemperatureC *float64 `json:"temperature_c,omitempty"`
	WeatherCode  string   `json:"weather_code,omitempty"` // provider-specific opaque token
	RecordedAt   string   `json:"recorded_at,omitempty"`  // RFC3339 when sample was taken
}
