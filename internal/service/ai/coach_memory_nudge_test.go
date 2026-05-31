package ai

import "testing"

func TestCoachMemoryTurnChecklist(t *testing.T) {
	if got := coachMemoryTurnChecklist("USER_MEMORY\nno saved memory yet"); got != "" {
		t.Fatalf("empty memory should yield no checklist, got %q", got)
	}
	withAdherence := "## Routine adherence (IMPORTANT\n- COACH_ACTION: low"
	got := coachMemoryTurnChecklist("## Recent SkinChecks\nfoo\n" + withAdherence)
	mustContain(t, got, "So với lần trước")
	mustContain(t, got, "COACH_ACTION")

	visionOnly := coachTurnChecklist("USER_MEMORY\n(no saved memory yet)", true)
	mustContain(t, visionOnly, "≥4–6 photo details")
	mustContain(t, visionOnly, "da hỗn hợp")
}

func TestPrependCoachActionPriority(t *testing.T) {
	ctx := "## Routine adherence\n- COACH_ACTION: low adherence — simplify\n"
	got := prependCoachActionPriority(ctx)
	mustContain(t, got, "PRIORITY")
	mustContain(t, got, "simplify")
}

func TestNeedsAdherenceRetry(t *testing.T) {
	mem := "- COACH_ACTION: low adherence"
	out := &CoachStructuredOutput{
		Strengths:         []string{"Bạn ghi nhật ký tốt"},
		SituationAnalysis: "Hôm nay da dầu",
		RoutineHints:      []string{"Sáng: kem chống nắng"},
	}
	if !needsAdherenceRetry(mem, out) {
		t.Fatal("expected retry when adherence not mentioned")
	}
	out.Strengths = []string{"Mấy hôm tick ít bước cũng OK — hôm nay chỉ 2 bước thôi"}
	if needsAdherenceRetry(mem, out) {
		t.Fatal("should not retry when adherence mentioned in strengths")
	}
}
