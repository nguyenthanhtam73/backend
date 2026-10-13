package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
)

// crudeAddressPermitted reports whether a coach prompt tells the model to
// address the user as mày/tao. Lines that only forbid those pronouns do not count.
func crudeAddressPermitted(p string) bool {
	cleaned := p
	for _, ban := range []string{
		"Cấm tao, mày, con, thằng này, bà này",
		"ZERO tao/mày/tục",
		"không dùng tao/mày",
		"NO tao/mày",
		"Never tao/mày",
		"BAN: tao/mày",
	} {
		cleaned = strings.ReplaceAll(cleaned, ban, "")
	}
	return strings.Contains(cleaned, "mày") || strings.Contains(cleaned, "tao")
}

func TestDefaultPrompt_DoesNotPermitMayTao(t *testing.T) {
	for _, skill := range []string{"", "beginner", "intermediate", "advanced"} {
		for _, kind := range []string{"", "web", domain.RefreshClientAndroid} {
			p := GetCoachPromptForClient(skill, kind)
			if crudeAddressPermitted(p) {
				t.Fatalf("skill %q kind %q permits mày/tao", skill, kind)
			}
			if !strings.Contains(p, "bạn") {
				t.Fatalf("skill %q kind %q does not address the user as bạn", skill, kind)
			}
		}
	}
	schema := coachOutputSchemaForClient("")
	user := buildSkinCheckCoachUserMessage(&domain.SkinCheck{}, nil, "", `{"visible_observations":["má"]}`, "ok", "TODAY")
	_, daily := buildDailyFeedbackPrompt("Hôm nay da ổn", "beginner")
	checklist := coachTurnChecklist("## Recent SkinChecks\n- 2026-05-01", true)
	for _, name := range []struct{ label, text string }{
		{"schema", schema},
		{"check-in", user},
		{"daily", daily},
		{"checklist", checklist},
	} {
		if crudeAddressPermitted(name.text) {
			t.Fatalf("%s permits mày/tao", name.label)
		}
	}
}

func TestEmptyUserNote_OmitsNotesInstruction(t *testing.T) {
	if todayNoteBlock(nil, "") != "" || todayNoteBlock(&domain.SkinCheck{}, "TODAY") != "" {
		t.Fatal("empty note returned a notes instruction")
	}
	empty := buildSkinCheckCoachUserMessage(&domain.SkinCheck{}, nil, "", `{"visible_observations":["má"]}`, "ok", "TODAY")
	if strings.Contains(empty, "USER_WROTE_NOTE:") || strings.Contains(empty, "User note:") {
		t.Fatal("empty notes produced a notes instruction")
	}
	_, daily := buildDailyFeedbackPrompt("Hôm nay da ổn. Không có ghi chú.", "beginner")
	if strings.Contains(daily, "USER_WROTE_NOTE:") || strings.Contains(daily, "User note:") {
		t.Fatal("empty daily notes produced a notes instruction")
	}
	if !strings.Contains(GetCoachPrompt("beginner"), "Không bịa ghi chú") {
		t.Fatal("default prompt missing the do-not-fabricate rule")
	}

	check := &domain.SkinCheck{UserNote: "lo về mụn ẩn"}
	filled := buildSkinCheckCoachUserMessage(check, nil, "", `{"visible_observations":["má"]}`, "ok", "")
	if !strings.Contains(filled, "USER_WROTE_NOTE:") || !strings.Contains(filled, "lo về mụn ẩn") {
		t.Fatal("written note missing from the check-in prompt")
	}
	_, withNote := buildDailyFeedbackPrompt("User note: lo về mụn ẩn", "beginner")
	if !strings.Contains(withNote, "USER_WROTE_NOTE:") || !strings.Contains(withNote, "lo về mụn ẩn") {
		t.Fatal("written note missing from the daily prompt")
	}
}

func TestDefaultVoiceGuard_RewritesMayTao(t *testing.T) {
	out := &CoachStructuredOutput{
		SituationAnalysis: "Mày thấy hôm nay má đỏ.",
		ConcernAlignment:  "Mày ghi lo về mụn ẩn.",
		SummaryNotes:      "Nếu mày đang dùng BHA thì dừng. Mai tao check.",
		CareSuggestions: []CoachCareSuggestion{{
			Slot: "evening", Step: "Dưỡng ẩm", Why: "Nếu mày đang dùng retinol thì nghỉ.",
		}},
	}
	if !SanitizeAndroidCoachOutput(out) {
		t.Fatal("guard did not rewrite crude address")
	}
	blob := out.SituationAnalysis + "\n" + out.ConcernAlignment + "\n" + out.SummaryNotes + "\n" + out.CareSuggestions[0].Why
	if strings.Contains(blob, "mày") || strings.Contains(blob, "tao") {
		t.Fatalf("guard left crude address: %s", blob)
	}
	lower := strings.ToLower(blob)
	if !strings.Contains(lower, "bạn") || !strings.Contains(lower, "mình") {
		t.Fatalf("guard missed bạn/mình: %s", blob)
	}
	if !strings.Contains(out.CareSuggestions[0].Why, "Nếu bạn đang dùng") {
		t.Fatalf("care tip %q", out.CareSuggestions[0].Why)
	}
	// Eyebrow "mày" stays an eyebrow.
	if got := SanitizeAndroidVoiceText("Lông mày đậm, da bạn ổn"); got != "Lông mày đậm, da bạn ổn" {
		t.Fatalf("eyebrow rewritten: %q", got)
	}
}
