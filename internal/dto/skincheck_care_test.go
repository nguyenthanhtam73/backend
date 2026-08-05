package dto

import (
	"encoding/json"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestBuildCoachDetailCareSuggestionsFromSkinScores(t *testing.T) {
	t.Parallel()
	scores := map[string]any{
		"overall":             0.55,
		"situation_analysis":  "Má đỏ vài nốt.",
		"concern_alignment":   "Khớp tags viêm.",
		"care_suggestions": []map[string]string{
			{
				"slot":        "evening",
				"step":        "Rửa mặt dịu",
				"why":         "Đang sưng.",
				"safety_note": "Đừng nặn.",
			},
			{
				"slot": "morning",
				"step": "Chống nắng",
				"why":  "Hạn chế nắng kích đỏ.",
			},
		},
	}
	ss, err := json.Marshal(scores)
	if err != nil {
		t.Fatal(err)
	}
	a := &domain.SkinAnalysis{
		ID:          uuid.New(),
		SkinCheckID: uuid.New(),
		Status:      domain.AnalysisStatusCompleted,
		SkinScores:  ss,
		Improvements: mustRawJSON(t, []map[string]string{
			{"tip": "legacy tip", "why": "should not replace care"},
		}),
	}
	d := buildCoachDetailFromDomain(a)
	if d == nil {
		t.Fatal("nil detail")
	}
	if d.SituationSummary != "Má đỏ vài nốt." {
		t.Fatalf("summary=%q", d.SituationSummary)
	}
	if len(d.CareSuggestions) != 2 {
		t.Fatalf("care=%d", len(d.CareSuggestions))
	}
	if d.CareSuggestions[0].Step != "Rửa mặt dịu" || d.CareSuggestions[0].SafetyNote != "Đừng nặn." {
		t.Fatalf("%#v", d.CareSuggestions[0])
	}
}

func TestBuildCoachDetailCareSuggestionsFallbackImprovements(t *testing.T) {
	t.Parallel()
	a := &domain.SkinAnalysis{
		ID:          uuid.New(),
		SkinCheckID: uuid.New(),
		Status:      domain.AnalysisStatusCompleted,
		Improvements: mustRawJSON(t, []map[string]string{
			{"tip": "Sáng: chống nắng", "why": "Đỏ nhẹ."},
			{"tip": "Tối: rửa mặt dịu", "why": "Đang viêm."},
		}),
	}
	d := buildCoachDetailFromDomain(a)
	if len(d.CareSuggestions) != 2 {
		t.Fatalf("care=%d", len(d.CareSuggestions))
	}
	if d.CareSuggestions[0].Slot != "morning" || d.CareSuggestions[0].Step != "chống nắng" {
		t.Fatalf("got %#v", d.CareSuggestions[0])
	}
	if d.CareSuggestions[1].Slot != "evening" {
		t.Fatalf("slot=%s", d.CareSuggestions[1].Slot)
	}
}

func mustRawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
