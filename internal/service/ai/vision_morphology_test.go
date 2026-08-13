package ai

import (
	"strings"
	"testing"
)

func TestVisionMorphologyRules_SharedAcrossPipelines(t *testing.T) {
	t.Parallel()
	shared := VisionMorphologyRules()
	if strings.TrimSpace(shared) == "" {
		t.Fatal("VisionMorphologyRules must not be empty")
	}
	for name, blob := range map[string]string{
		"admin":      AdminSkinReviewSystemPrompt(),
		"onboarding": OnboardingSkinVisionPrompt(),
		"check-in":   VisionObservationSystemPrompt(),
	} {
		if !strings.Contains(blob, shared) {
			t.Fatalf("%s vision prompt must concatenate VisionMorphologyRules() verbatim — edit vision_morphology.go, not a fork", name)
		}
	}
}

func TestVisionMorphologyCoachGuard_SharedAcrossCoaches(t *testing.T) {
	t.Parallel()
	guard := VisionMorphologyCoachGuard()
	if strings.TrimSpace(guard) == "" {
		t.Fatal("VisionMorphologyCoachGuard must not be empty")
	}
	for name, blob := range map[string]string{
		"onboarding-coach": OnboardingCoachSystemPrompt(),
		"check-in-coach":   GetCoachPrompt("intermediate"),
		"beginner-coach":   GetCoachPrompt("beginner"),
		"starter-routine":  StarterRoutineSystemPrompt(),
	} {
		if !strings.Contains(blob, guard) {
			t.Fatalf("%s must concatenate VisionMorphologyCoachGuard() so care advice follows vision groups", name)
		}
	}
}

func TestVisionMorphologyRules_KeyNeedles(t *testing.T) {
	t.Parallel()
	p := VisionMorphologyRules()
	for _, needle := range []string{
		"trông giống mụn ẩn hoặc milia",
		"sần sùi / texture không đều",
		"CẤM tuyệt đối",
		"mụn thịt",
		"nếp gấp / nếp ngang cổ",
		"Thâm / sẫm khóe miệng",
		"Viêm cấp sát mép môi",
		"Nốt nhỏ + đỏ hồng rõ",
		"closed comedones",
		"đẩy BHA/retinol",
	} {
		if !strings.Contains(p, needle) {
			t.Fatalf("shared morphology missing %q — this is the source of truth for admin+onboarding+check-in", needle)
		}
	}
}

func TestOnboardingPrompt_IncludesSharedMorphologyMap(t *testing.T) {
	t.Parallel()
	p := OnboardingSkinVisionPrompt()
	for _, needle := range []string{
		"Map nhóm hình thái → JSON onboarding",
		"trông giống mụn ẩn hoặc milia",
		"vừa mụn ẩn vừa kích ứng",
		"đẩy BHA/retinol",
	} {
		if !strings.Contains(p, needle) {
			t.Fatalf("onboarding vision missing %q", needle)
		}
	}
}

func TestCheckInPrompt_IncludesSharedMorphologyMap(t *testing.T) {
	t.Parallel()
	p := VisionObservationSystemPrompt()
	if !strings.Contains(p, CheckInMorphologyJSONMap()) {
		t.Fatal("check-in vision must include CheckInMorphologyJSONMap")
	}
	if !strings.Contains(p, "tên nhóm phải đúng") {
		t.Fatal("check-in map must stress correct group names")
	}
}
