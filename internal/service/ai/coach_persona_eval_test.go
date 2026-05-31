package ai

import (
	"strings"
	"testing"
)

// TestCoachPersona_MemoryBlocks verifies each QA persona ships a rich USER_MEMORY
// block with the sections the v11 prompt references.
func TestCoachPersona_MemoryBlocks(t *testing.T) {
	for _, p := range CoachPersonas() {
		t.Run(p.ID, func(t *testing.T) {
			mustContain(t, p.Memory, "USER_MEMORY")
			mustContain(t, p.Memory, "## Saved SkinProfile")
			mustContain(t, p.Memory, "## Recent SkinChecks")
			mustContain(t, p.Memory, "## Routine adherence")
			mustNotContain(t, p.Memory, "no saved memory yet")

			ctx := p.TodayContext()
			mustContain(t, ctx, "Daily skin check-in")
			if p.SkillLevel == "beginner" {
				mustContain(t, ctx, "beginner")
			}
		})
	}
}

func TestCoachPersona_FrequentNotHelpful_HasFeedbackSummary(t *testing.T) {
	var p CoachPersona
	for _, x := range CoachPersonas() {
		if x.ID == "frequent_not_helpful" {
			p = x
			break
		}
	}
	if p.ID == "" {
		t.Fatal("persona not found")
	}
	mustContain(t, p.Memory, "## Feedback summary")
	mustContain(t, p.Memory, "not_helpful")
	mustContain(t, p.Memory, "USER_FEEDBACK_HISTORY")
	mustContain(t, p.Memory, "BHA quá mạnh")
}

func TestCoachPrompt_v11_MemoryBindingRules(t *testing.T) {
	p := GetCoachPrompt("intermediate")
	mustContain(t, p, "USER_MEMORY")
	mustContain(t, p, "Routine adherence")
	mustContain(t, p, "COACH_ACTION")
	mustContain(t, p, "≥3 chi tiết cụ thể từ ảnh")
	mustContain(t, p, "KHÔNG bịa")
}

func TestCoachPrompt_BeginnerExamples(t *testing.T) {
	p := GetCoachPrompt("beginner")
	mustContain(t, p, "mấy lần gần đây")
	mustContain(t, p, "adherence thấp")
	mustContain(t, p, "2 bước")
	mustContain(t, p, "≥3 chi tiết")
}

// TestScoreCoachPersonalization_Heuristic validates the offline scorer on synthetic output.
func TestScoreCoachPersonalization_Heuristic(t *testing.T) {
	persona := personaBeginnerOily()
	out := &CoachStructuredOutput{
		SituationAnalysis: "Hôm nay vùng T vẫn hơi dầu, má hơi khô — giống vài lần gần đây bạn ghi nhận.",
		Strengths:         []string{"Bạn quay lại ghi nhật ký đều — tốt lắm."},
		RoutineHints:      []string{"Sáng: kem chống nắng nhẹ", "Tối: kem dưỡng ẩm"},
		SummaryNotes:      "Mai chụp cùng góc nhé.",
	}
	score := ScoreCoachPersonalization(persona, out, true)
	if score.Score < 0.4 {
		t.Fatalf("expected score >= 0.4, got %.2f missing=%v", score.Score, score.MissingWant)
	}
	if len(score.HitAvoid) > 0 {
		t.Fatalf("unexpected avoid hits: %v", score.HitAvoid)
	}
}

// TestPersonaContext_MemoryDelta ensures the with-memory context is strictly
// larger and contains history sections the no-memory baseline lacks.
func TestPersonaContext_MemoryDelta(t *testing.T) {
	for _, p := range CoachPersonas() {
		t.Run(p.ID, func(t *testing.T) {
			withMem := p.FullContextWithMemory()
			without := p.TodayContextWithoutHistory()
			if len(withMem) <= len(without) {
				t.Fatalf("with-memory context should be longer")
			}
			mustContain(t, withMem, "## Recent SkinChecks")
			mustNotContain(t, without, "## Recent SkinChecks")
		})
	}
}

// TestFlattenCoachOutput_CaseInsensitive makes scoring robust to casing.
func TestFlattenCoachOutput_CaseInsensitive(t *testing.T) {
	out := &CoachStructuredOutput{
		SituationAnalysis: "Vùng T dầu",
	}
	text := FlattenCoachOutput(out)
	if !strings.Contains(text, "vùng t") {
		t.Fatalf("expected lowercase flatten, got %q", text)
	}
}
