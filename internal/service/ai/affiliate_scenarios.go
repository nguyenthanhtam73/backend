package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// AffiliateScenario is a QA fixture for affiliate product_suggestions quality.
type AffiliateScenario struct {
	CoachPersona
	Wardrobe []wardrobeItem
	ExpectMaxCount   int
	ExpectIdealMax   int
	ExpectMinCount   int
	PreferCategories []string
	AvoidCategories  []string
}

type wardrobeItem struct {
	Name     string
	Brand    string
	Category string
}

// AffiliateScenarios returns six diverse affiliate QA personas.
func AffiliateScenarios() []AffiliateScenario {
	return []AffiliateScenario{
		affiliateBeginnerOilyAcne(),
		affiliateIntermediateBarrier(),
		affiliateWardrobeFull(),
		affiliateMissingSPF(),
		affiliateFrequentNotHelpful(),
		affiliateHyperpigmentation(),
	}
}

func affiliateBeginnerOilyAcne() AffiliateScenario {
	p := personaBeginnerOily()
	p.ID = "beginner_oily_acne"
	check := p.TodayCheck
	check.Conditions, _ = json.Marshal([]string{"oily", "breakout", "large_pores"})
	check.Symptoms, _ = json.Marshal([]string{"new_breakouts", "shiny_tzone"})
	check.UserNote = "Mới bắt đầu chăm da. Vùng trán và mũi có vài nốt mụn nhỏ, T-zone bóng dầu buổi chiều."
	p.TodayCheck = check
	p.Memory = assembleMemory(
		profileSection(p.Profile),
		recentChecksSection([]recentCheckLine{
			{date: "2026-05-28", tags: "oily, breakout", note: "2-3 nốt mụn trán"},
			{date: "2026-05-27", tags: "oily, large_pores", note: "chưa có routine cố định"},
		}),
		routineSection(12, 20),
	)
	return AffiliateScenario{
		CoachPersona:     p,
		ExpectMaxCount:   2,
		ExpectIdealMax:   2,
		PreferCategories: []string{"cleanser", "spf", "serum"},
		AvoidCategories:  []string{"treatment"},
	}
}

func affiliateIntermediateBarrier() AffiliateScenario {
	uid := uuid.New()
	profile := &domain.SkinProfile{
		ID:         uuid.New(),
		UserID:     uid,
		SkinType:   "combo",
		SkillLevel: domain.SkillLevelIntermediate,
		Concerns:   mustJSONPersona([]string{"dehydration", "weak_barrier"}),
		Notes:      "má khô căng, T hơi dầu, từng dùng actives bị rát",
	}
	check := mkTodayCheck(uid, []string{"combo", "dehydrated", "weak_barrier"}, []string{"tight_cheeks", "stinging"},
		"Má căng khô và hơi châm chích nhẹ. T-zone bóng nhẹ buổi chiều.", "intermediate")
	memory := assembleMemory(
		profileSection(profile),
		recentChecksSection([]recentCheckLine{
			{date: "2026-05-28", tags: "weak_barrier, dehydration", note: "má đỏ nhẹ sau tẩy tết"},
			{date: "2026-05-27", tags: "dehydration, combo", note: "cần focus phục hồi lớp bảo vệ da"},
		}),
		routineSection(45, 55),
	)
	return AffiliateScenario{
		CoachPersona: CoachPersona{
			ID:         "intermediate_barrier",
			SkillLevel: "intermediate",
			Profile:    profile,
			TodayCheck: check,
			Memory:     memory,
		},
		ExpectMaxCount:   2,
		ExpectIdealMax:   2,
		PreferCategories: []string{"cleanser", "moisturizer", "serum", "toner"},
		AvoidCategories:  []string{"treatment"},
	}
}

func affiliateWardrobeFull() AffiliateScenario {
	uid := uuid.New()
	profile := &domain.SkinProfile{
		ID:         uuid.New(),
		UserID:     uid,
		SkinType:   "combo",
		SkillLevel: domain.SkillLevelIntermediate,
		Concerns:   mustJSONPersona([]string{"dehydration"}),
		Notes:      "đã có routine ổn định, tủ đồ khá đầy",
	}
	check := mkTodayCheck(uid, []string{"combo", "dehydrated"}, []string{"tight_cheeks"},
		"Má hơi căng, routine vẫn giữ đều. Không muốn mua thêm nhiều.", "intermediate")
	wardrobe := []wardrobeItem{
		{Name: "Sữa rửa mặt tạo bọt", Brand: "CeraVe", Category: "cleanser"},
		{Name: "Kem chống nắng Relief Sun SPF50+", Brand: "Beauty of Joseon", Category: "spf"},
		{Name: "Serum Niacinamide 10% + Zinc 1%", Brand: "The Ordinary", Category: "serum"},
		{Name: "Kem dưỡng ẩm Hydro Boost Water Gel", Brand: "Neutrogena", Category: "moisturizer"},
		{Name: "Tinh chất ốc sên Advanced Snail 96 Mucin", Brand: "COSRX", Category: "serum"},
	}
	memory := assembleMemory(
		profileSection(profile),
		wardrobeSection(wardrobe),
		recentChecksSection([]recentCheckLine{
			{date: "2026-05-28", tags: "dehydration, combo", note: "má căng nhẹ"},
		}),
		routineSection(78, 85),
	)
	return AffiliateScenario{
		CoachPersona: CoachPersona{
			ID:         "wardrobe_full",
			SkillLevel: "intermediate",
			Profile:    profile,
			TodayCheck: check,
			Memory:     memory,
		},
		Wardrobe:         wardrobe,
		ExpectMaxCount:   2,
		ExpectIdealMax:   1,
		// Wardrobe stocked — any 0–1 gap filler OK; do not require toner/mask specifically.
		PreferCategories: nil,
	}
}

func affiliateMissingSPF() AffiliateScenario {
	uid := uuid.New()
	profile := &domain.SkinProfile{
		ID:         uuid.New(),
		UserID:     uid,
		SkinType:   "normal",
		SkillLevel: domain.SkillLevelBeginner,
		Concerns:   mustJSONPersona([]string{"dullness"}),
		Notes:      "hay quên kem chống nắng buổi sáng",
	}
	check := mkTodayCheck(uid, []string{"dull", "dehydrated"}, []string{"recent_sun_exposure"},
		"Da hơi xỉn, hôm qua ra ngoài nắng 2 tiếng không bôi kem chống nắng.", "beginner")
	wardrobe := []wardrobeItem{
		{Name: "Sữa rửa mặt dưỡng ẩm", Brand: "CeraVe", Category: "cleanser"},
		{Name: "Kem dưỡng ẩm Hydro Boost Water Gel", Brand: "Neutrogena", Category: "moisturizer"},
	}
	memory := assembleMemory(
		profileSection(profile),
		wardrobeSection(wardrobe),
		recentChecksSection([]recentCheckLine{
			{date: "2026-05-28", tags: "dull, recent_sun_exposure", note: "quên SPF sáng nay"},
			{date: "2026-05-27", tags: "dull", note: "không có bước chống nắng"},
		}),
		routineSection(40, 50),
	)
	return AffiliateScenario{
		CoachPersona: CoachPersona{
			ID:         "missing_spf",
			SkillLevel: "beginner",
			Profile:    profile,
			TodayCheck: check,
			Memory:     memory,
		},
		Wardrobe:         wardrobe,
		ExpectMaxCount:   2,
		ExpectIdealMax:   1,
		ExpectMinCount:   0,
		PreferCategories: []string{"spf"},
	}
}

func affiliateFrequentNotHelpful() AffiliateScenario {
	p := personaFrequentNotHelpful()
	return AffiliateScenario{
		CoachPersona:     p,
		ExpectMaxCount:   1,
		ExpectIdealMax:   1,
		PreferCategories: []string{"cleanser", "moisturizer"},
		AvoidCategories:  []string{"treatment"},
	}
}

func affiliateHyperpigmentation() AffiliateScenario {
	uid := uuid.New()
	profile := &domain.SkinProfile{
		ID:         uuid.New(),
		UserID:     uid,
		SkinType:   "normal",
		SkillLevel: domain.SkillLevelIntermediate,
		Concerns:   mustJSONPersona([]string{"hyperpigmentation", "pih"}),
		Notes:      "thâm sau mụn vùng má, muốn sáng da dần",
	}
	check := mkTodayCheck(uid, []string{"hyperpigmentation", "dull"}, []string{"post_acne_marks"},
		"Vài chấm thâm nâu vùng má trái, da còn lại ổn. Đã dùng SPF không đều.", "intermediate")
	memory := assembleMemory(
		profileSection(profile),
		recentChecksSection([]recentCheckLine{
			{date: "2026-05-28", tags: "hyperpigmentation, pih", note: "thâm má trái 3-4 chấm"},
			{date: "2026-05-26", tags: "post_acne_marks", note: "thâm cũ chưa mờ"},
		}),
		routineSection(60, 65),
	)
	return AffiliateScenario{
		CoachPersona: CoachPersona{
			ID:         "hyperpigmentation",
			SkillLevel: "intermediate",
			Profile:    profile,
			TodayCheck: check,
			Memory:     memory,
		},
		ExpectMaxCount:   2,
		ExpectIdealMax:   2,
		PreferCategories: []string{"spf", "serum"},
	}
}

func wardrobeSection(items []wardrobeItem) string {
	if len(items) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Wardrobe (products user already owns — DO NOT re-recommend these)\n")
	for _, it := range items {
		fmt.Fprintf(&b, "- %s | brand: %s | category: %s\n", it.Name, it.Brand, it.Category)
	}
	b.WriteString("If wardrobe already covers today's gap, return product_suggestions: [].\n")
	return b.String()
}
