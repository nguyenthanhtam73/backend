package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
)

func TestGetCoachPromptForClient_WebUnchanged(t *testing.T) {
	for _, skill := range []string{"", "beginner", "intermediate", "advanced"} {
		web := GetCoachPrompt(skill)
		for _, kind := range []string{"", "web", "Web", "Android", "ANDROID", "dadiary-android", " ios "} {
			got := GetCoachPromptForClient(skill, kind)
			if got != web {
				t.Fatalf("skill %q kind %q changed the web prompt", skill, kind)
			}
		}
	}

	beginner := GetCoachPrompt("beginner")
	for _, frag := range []string{
		"Đm da mày hôm nay…",
		"tao / mày / con / thằng này / bà này",
		"không đổi sang \"mình/bạn\" làm mặc định",
		"Được dùng vl/đm nhưng đừng dày đặc",
		"Mày thấy hôm nay vùng má trái lỗ chân lông to vl",
	} {
		if !strings.Contains(beginner, frag) {
			t.Fatalf("web beginner prompt missing %q", frag)
		}
	}
	if !strings.Contains(GetCoachPrompt("intermediate"), "bựa full") {
		t.Fatal("web intermediate prompt missing bựa full")
	}
	if coachOutputSchemaForClient("") != CoachOutputJSONSchemaBlock {
		t.Fatal("empty client must keep the web schema block")
	}
	if coachOutputSchemaForClient("web") != CoachOutputJSONSchemaBlock {
		t.Fatal("web client must keep the web schema block")
	}
	if !strings.Contains(CoachOutputJSONSchemaBlock, "Đm da mày hôm nay…") {
		t.Fatal("web schema block lost the crude opener")
	}
	if !strings.Contains(CoachOutputJSONSchemaBlock, "tao muốn xem mày có chịu làm không") {
		t.Fatal("web schema block lost the crude summary example")
	}

	webMsg := buildSkinCheckCoachUserMessage(&domain.SkinCheck{ClientKind: domain.RefreshClientWeb}, nil, "", "", "ok", "TODAY")
	if !strings.Contains(webMsg, "Đm da mày hôm nay…") {
		t.Fatal("web user message lost the crude schema opener")
	}
	sys, user := buildDailyFeedbackPrompt("Hôm nay da ổn", "beginner")
	sys2, user2 := buildDailyFeedbackPromptForClient("Hôm nay da ổn", "beginner", "")
	if sys != sys2 || user != user2 {
		t.Fatal("daily feedback web prompt changed")
	}
	if !strings.Contains(user, "Đm da mày hôm nay…") {
		t.Fatal("web daily feedback schema lost the crude opener")
	}
}

func TestGetCoachPromptForClient_AndroidPolite(t *testing.T) {
	for _, skill := range []string{"beginner", "intermediate", "advanced"} {
		t.Run(skill, func(t *testing.T) {
			p := GetCoachPromptForClient(skill, "  android  ")
			if p == GetCoachPrompt(skill) {
				t.Fatal("android prompt must differ from web")
			}
			for _, frag := range []string{
				"mình / bạn",
				"≥3–4 chi tiết cụ thể",
				"Quy tắc ngôn ngữ",
				"jawline",
				"vùng hàm",
				"lớp bảo vệ da",
				"PHOTO_EVIDENCE",
				"BẮT BUỘC nói chưa chắc",
				"BREVITY",
				"situation_analysis",
				"improvements",
				"care_suggestions",
				"So với lần trước",
				"USER_MEMORY",
				"COACH_ACTION",
				"Routine adherence",
				"COACH_KNOWLEDGE",
				"Không chẩn đoán",
			} {
				if !strings.Contains(p, frag) {
					t.Fatalf("android %s prompt missing %q", skill, frag)
				}
			}
			for _, banned := range []string{
				"Đm da mày hôm nay",
				"Được dùng vl/đm",
				"tao / mày / con / thằng này / bà này",
				"không đổi sang \"mình/bạn\" làm mặc định",
				"lỗ chân lông to vl",
				"bựa full",
			} {
				if strings.Contains(p, banned) {
					t.Fatalf("android %s prompt still has web voice fragment %q", skill, banned)
				}
			}
		})
	}

	beginner := GetCoachPromptForClient("beginner", domain.RefreshClientAndroid)
	if !strings.Contains(beginner, "nhẹ tay hơn") {
		t.Fatal("beginner android prompt should still soften severity")
	}
	if !strings.Contains(beginner, "routine_hints 2–3") {
		t.Fatal("beginner android prompt lost the shorter hint cap")
	}
	normal := GetCoachPromptForClient("intermediate", domain.RefreshClientAndroid)
	if !strings.Contains(normal, "routine_hints 3–4") {
		t.Fatal("normal android prompt lost the hint cap")
	}
	if strings.Contains(normal, "nhẹ tay hơn một chút") {
		t.Fatal("normal android prompt picked up the beginner softness block")
	}

	schema := coachOutputSchemaForClient(domain.RefreshClientAndroid)
	if strings.Contains(schema, "Đm da mày") || strings.Contains(schema, "tao muốn xem mày") {
		t.Fatal("android schema still shows crude examples")
	}
	if !strings.Contains(schema, "Mình thấy hôm nay…") {
		t.Fatal("android schema missing polite opener")
	}
	for _, key := range []string{
		`"score"`, `"strengths"`, `"situation_analysis"`, `"improvements"`,
		`"care_suggestions"`, `"routine_hints"`, `"avoid_or_patch"`,
		`"safety_reminders"`, `"skin_scores"`, `"concern_alignment"`,
		`"medical_disclaimer"`, `"summary_notes"`, `"product_suggestions"`,
	} {
		if !strings.Contains(schema, key) || !strings.Contains(CoachOutputJSONSchemaBlock, key) {
			t.Fatalf("schema key %s missing from web or android block", key)
		}
	}
	for _, cap := range []string{
		"situation_analysis 2–3 sentences",
		"improvements 2–3 items",
		"care_suggestions 3–5 items",
		"routine_hints 3–4 lines (Beginner 2–3)",
	} {
		if !strings.Contains(schema, cap) {
			t.Fatalf("android schema lost cap %q", cap)
		}
	}

	check := &domain.SkinCheck{ClientKind: domain.RefreshClientAndroid}
	msg := buildSkinCheckCoachUserMessage(check, nil, "", `{"visible_observations":["má đỏ"]}`, "ok", "TODAY")
	if strings.Contains(msg, "Đm da mày hôm nay") {
		t.Fatal("android user message still requires the crude opener")
	}
	if !strings.Contains(msg, "Mình thấy hôm nay…") {
		t.Fatal("android user message missing polite opener")
	}
	if !strings.Contains(msg, "NO tao/mày") {
		t.Fatal("android checklist missing the profanity ban")
	}

	_, daily := buildDailyFeedbackPromptForClient("Hôm nay da ổn", "beginner", domain.RefreshClientAndroid)
	sys, _ := buildDailyFeedbackPromptForClient("Hôm nay da ổn", "beginner", domain.RefreshClientAndroid)
	if strings.Contains(daily, "Đm da mày hôm nay") || strings.Contains(sys, "Đm da mày hôm nay") {
		t.Fatal("android daily feedback still uses the crude voice")
	}
	if !strings.Contains(sys, "mình / bạn") {
		t.Fatal("android daily feedback system prompt is not polite")
	}
}

func TestCoachVoiceFollowsStoredClientKind(t *testing.T) {
	// analysis.Process reloads the skin check and passes that row in.
	// The prompt must follow the stored column, not a request that already returned.
	stored := &domain.SkinCheck{ClientKind: domain.RefreshClientAndroid}
	sys := GetCoachPromptForClient("beginner", stored.ClientKind)
	user := buildSkinCheckCoachUserMessage(stored, nil, "memory", `{"visible_observations":["má"]}`, "ok", "full")
	if !IsAndroidCoachVoice(stored.ClientKind) || strings.Contains(sys, "Đm da mày") || strings.Contains(user, "Đm da mày") {
		t.Fatal("stored android kind did not select the polite voice")
	}
	stored.ClientKind = ""
	if GetCoachPromptForClient("beginner", stored.ClientKind) != GetCoachPrompt("beginner") {
		t.Fatal("empty stored kind must stay on the web prompt")
	}
}
