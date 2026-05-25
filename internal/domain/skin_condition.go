package domain

// SkinCondition is a coarse self-reported flag on a daily check-in (stored as string[] JSON).
type SkinCondition string

const (
	SkinConditionDry        SkinCondition = "dry"
	SkinConditionOily       SkinCondition = "oily"
	SkinConditionCombo      SkinCondition = "combo"
	SkinConditionNormal     SkinCondition = "normal"
	SkinConditionSensitive  SkinCondition = "sensitive"
	SkinConditionBreakout   SkinCondition = "breakout"
	SkinConditionRedness    SkinCondition = "redness"
	SkinConditionDull       SkinCondition = "dull"
	SkinConditionDehydrated SkinCondition = "dehydrated"
	// Pigmentation / texture (optional coarse flags; finer detail can use SkinSymptom).
	SkinConditionHyperpigmentation SkinCondition = "hyperpigmentation"
	SkinConditionCloggedPores      SkinCondition = "clogged_pores"
	SkinConditionPIH               SkinCondition = "pih"
	SkinConditionWeakBarrier       SkinCondition = "weak_barrier"
	SkinConditionLargePores        SkinCondition = "large_pores"
	// Inflammatory look / flare (self-report; not a diagnosis).
	SkinConditionInflammation SkinCondition = "inflammation"
)
