package ai

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPlainWordsRule_InUserFacingPrompts(t *testing.T) {
	const marker = "ít làm da khô rát, dễ bị kích ứng"
	prompts := []struct {
		name string
		text string
	}{
		{"web beginner", GetCoachPrompt("beginner")},
		{"web intermediate", GetCoachPrompt("intermediate")},
		{"android beginner", GetCoachPromptForClient("beginner", "android")},
		{"android intermediate", GetCoachPromptForClient("intermediate", "android")},
		{"starter", StarterRoutineSystemPrompt()},
		{"suggest routine", suggestRoutineSystemPrompt()},
		{"wardrobe card", WardrobeProductInsightSystemPrompt()},
		{"onboarding vision", OnboardingSkinVisionPrompt()},
		{"onboarding coach", OnboardingCoachSystemPrompt()},
		{"admin review", AdminSkinReviewSystemPrompt()},
		{"admin review compact", AdminSkinReviewCompactSystemPrompt()},
		{"web schema", CoachOutputJSONSchemaBlock},
		{"android schema", coachOutputJSONSchemaBlockAndroid},
		{"affiliate rules", AffiliateRecommendationRulesBlock},
	}
	for _, p := range prompts {
		if !strings.Contains(p.text, marker) {
			t.Errorf("%s missing the plain-words example", p.name)
		}
	}
	web := GetCoachPrompt("intermediate")
	android := GetCoachPromptForClient("intermediate", "android")
	if !strings.Contains(web, "tao / mày") {
		t.Fatal("web pronouns changed")
	}
	if !strings.Contains(android, "mình / bạn") || strings.Contains(android, "tao / mày / con") {
		t.Fatal("android pronouns changed")
	}
}

func TestHardcodedUserFacingCopy_NoUnexplainedJargon(t *testing.T) {
	banned := []string{
		"hàng rào", "hạ kích", "barrier", "ceramides", "niacinamide",
		"bha", "retinoid", "kích ứng ngược", "purging",
	}
	var lines []string
	raw, err := os.ReadFile("affiliate_catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog []struct {
		ReasonTemplate string `json:"reason_template"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, row := range catalog {
		lines = append(lines, row.ReasonTemplate)
	}
	for _, locale := range []string{"vi", "en"} {
		for _, phase := range []string{PhaseCalmFirst, PhaseCanAddActive} {
			for _, item := range guidanceTemplates(phase, locale) {
				lines = append(lines, item.NameOrCategory, item.Why, item.HowToUse, item.Caution)
				lines = append(lines, item.Benefits...)
			}
		}
	}
	for _, step := range []string{"cleanse", "moisturize", "spf", "soothe", "treat"} {
		lines = append(lines, defaultBenefitsForStep(step, "vi")...)
		lines = append(lines, defaultBenefitsForStep(step, "en")...)
		lines = append(lines, defaultHowToUseForStep(step, "vi"), defaultHowToUseForStep(step, "en"))
	}
	for _, line := range lines {
		low := strings.ToLower(line)
		for _, word := range banned {
			if strings.Contains(low, word) {
				t.Errorf("user-facing copy %q contains %q", line, word)
			}
		}
	}
}
