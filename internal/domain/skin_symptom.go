package domain

// SkinSymptom is a finer-grained self-report tag (JSON array on check-ins).
// Kept separate from SkinCondition so we can evolve symptom lists without breaking coarse filters.
type SkinSymptom string

const (
	SymptomItching       SkinSymptom = "itching"
	SymptomStinging      SkinSymptom = "stinging"
	SymptomNewBreakouts  SkinSymptom = "new_breakouts"
	SymptomPostAcneMarks SkinSymptom = "post_acne_marks"
	SymptomSunExposure   SkinSymptom = "recent_sun_exposure"
	SymptomMaskFriction  SkinSymptom = "mask_friction"
)
