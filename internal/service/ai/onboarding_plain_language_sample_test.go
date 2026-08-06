package ai

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

// Prints sample user-facing onboarding text (fallback path + vision map) for manual jargon checklist.
func TestPrintOnboardingPlainLanguageSamples(t *testing.T) {
	raw := onboardingVisionRaw{
		SkinObservations: dto.OnboardingSkinObservations{
			OverallSkinType: "combination",
			TZone:           "very_oily",
			Cheeks:          "normal",
			PoreSize:        "large",
			Texture:         "slightly_rough",
			Redness:         "mild",
			Pigmentation:    "dark_spots",
			AcneStatus:      "inflammatory_acne",
			OilinessLevel:   "high",
		},
		DetailedObservations: "Trán mày bóng rõ hơn má. Cằm đang có khoảng 4–5 nốt đỏ nhỏ kiểu mụn viêm. Má hơi không đều màu, xen vài đốm thâm nhẹ. Mũi lộ lỗ chân lông to. Da hơi sần ở trán–mũi–cằm.",
		MainConcerns:         []string{"mụn viêm", "thâm", "lỗ chân lông to"},
		SkinTone:             "medium",
		Undertone:            "warm",
		PhotoQuality:         "good",
	}
	mapped := mapOnboardingVisionRaw(raw, "vi")
	notes := fallbackOnboardingCoachingNotes(&mapped, "vi")

	fmt.Println("\n=== SAMPLE coaching_notes (fallback, có ảnh) ===")
	fmt.Println(notes)
	fmt.Println("\n=== SAMPLE visual_observations ===")
	for _, b := range mapped.VisualObservations {
		fmt.Println("-", b)
	}
	fmt.Println("\n=== SAMPLE main_concerns (vision) → friendly ===")
	for _, c := range mapped.MainConcerns {
		fmt.Println("-", c, "→", friendlyConcern(c, "vi"))
	}
	for _, id := range mapped.Concerns {
		fmt.Println("- id", id, "→", friendlyConcern(id, "vi"))
	}

	// Goal-only path: no photos — emulate short coach-style note from friendly labels.
	goalOnly := fmt.Sprintf(
		"Mày chọn mục tiêu làm dịu / da dễ kích ứng, quan tâm %s và %s.\n\nBắt đầu đơn giản: rửa mặt dịu + kem dưỡng + kem chống nắng buổi sáng.",
		friendlyConcern("redness", "vi"),
		friendlyConcern("dryness", "vi"),
	)
	fmt.Println("\n=== SAMPLE notes (không ảnh / goal-only) ===")
	fmt.Println(goalOnly)

	fmt.Println("\n=== SAMPLE routine steps (goal-only style) ===")
	steps := []string{
		"Rửa mặt dịu — làm sạch nhẹ, không chà khi da đang dễ đỏ.",
		"Kem dưỡng ẩm — giữ da êm trước khi thêm hoạt chất mạnh.",
		"Kem chống nắng buổi sáng — bảo vệ da mỗi ngày, kể cả ở nhà gần cửa sổ.",
	}
	for i, s := range steps {
		fmt.Printf("%d. %s\n", i+1, s)
	}

	jargon := []string{
		"barrier", "erythema", "sebum", "papules", "comedone",
		"hyperpigmentation", "inflammation", "texture", "T-zone:",
		"inflammatory_acne", "very_oily", "hàng rào da",
	}
	check := notes + "\n" + strings.Join(mapped.VisualObservations, "\n") + "\n" + goalOnly + "\n" + strings.Join(steps, "\n")
	for _, bad := range jargon {
		if strings.Contains(strings.ToLower(check), strings.ToLower(bad)) {
			t.Errorf("jargon checklist FAIL: %q still in sample user text", bad)
		}
	}
}
