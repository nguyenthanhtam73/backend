package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeCareSuggestions(t *testing.T) {
	t.Parallel()
	out := &CoachStructuredOutput{
		CareSuggestions: []CoachCareSuggestion{
			{Slot: "Sáng", Step: "Rửa mặt dịu", Why: "Má đang đỏ.", SafetyNote: "Đừng nặn."},
			{Slot: "pm", Step: "Dưỡng ẩm", Why: "Giữ ẩm khi da kích."},
			{Slot: "priority", Step: "Giảm active mạnh", Why: "Đang viêm."},
			{Slot: "today", Step: "", Why: "skip empty step"},
		},
	}
	got := NormalizeCareSuggestions(out)
	if len(got) != 3 {
		t.Fatalf("want 3 got %d", len(got))
	}
	if got[0].Slot != "morning" || got[1].Slot != "evening" || got[2].Slot != "today" {
		t.Fatalf("slots=%v %v %v", got[0].Slot, got[1].Slot, got[2].Slot)
	}
	if got[0].SafetyNote != "Đừng nặn." {
		t.Fatalf("safety=%q", got[0].SafetyNote)
	}
}

func TestNormalizeCareSuggestionsFallbackFromImprovements(t *testing.T) {
	t.Parallel()
	out := &CoachStructuredOutput{
		Improvements: []struct {
			Tip string `json:"tip"`
			Why string `json:"why"`
		}{
			{Tip: "Sáng: chống nắng vùng má", Why: "Đỏ nhẹ sau nắng."},
			{Tip: "Tối: rửa mặt dịu", Why: "Tránh kích thêm."},
		},
	}
	got := NormalizeCareSuggestions(out)
	if len(got) != 2 {
		t.Fatalf("want 2 got %d", len(got))
	}
	if got[0].Slot != "morning" || !strings.Contains(strings.ToLower(got[0].Step), "chống nắng") {
		t.Fatalf("got %#v", got[0])
	}
	if got[1].Slot != "evening" {
		t.Fatalf("slot=%s", got[1].Slot)
	}
}

func TestParseCoachStructuredOutputCareSuggestions(t *testing.T) {
	t.Parallel()
	raw := `{
  "score": 0.6,
  "strengths": ["Check-in đều"],
  "situation_analysis": "Mày thấy hôm nay má trái đỏ vài nốt sưng nhẹ.",
  "improvements": [{"tip": "Tối: rửa mặt dịu", "why": "Đang viêm."}],
  "care_suggestions": [
    {"slot": "evening", "step": "Rửa mặt dịu", "why": "Má đang sưng — dịu để khỏi kích.", "safety_note": "Tránh nặn."},
    {"slot": "morning", "step": "Chống nắng", "why": "Hạn chế nắng làm đỏ thêm.", "safety_note": ""},
    {"slot": "today", "step": "Giảm active mạnh", "why": "Da đang kích.", "safety_note": "Tạm bỏ acid mạnh."}
  ],
  "routine_hints": ["Sáng: chống nắng", "Tối: rửa mặt dịu", "Tối: dưỡng ẩm"],
  "avoid_or_patch": ["Đừng nặn"],
  "safety_reminders": ["Ổ to/đau kéo dài thì nên khám da liễu."],
  "skin_scores": {"hydration": 0.5, "clarity": 0.4, "barrier": 0.5},
  "concern_alignment": "Tags khớp vùng má đỏ trên ảnh.",
  "medical_disclaimer": "Chỉ mang tính tham khảo, không thay bác sĩ.",
  "summary_notes": "Mai chụp lại góc má nhé.",
  "product_suggestions": []
}`
	parsed, err := parseCoachStructuredOutput(raw, "test-care")
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.CareSuggestions) != 3 {
		t.Fatalf("care=%d", len(parsed.CareSuggestions))
	}
	// Persist shape used by analysis.Process
	labels := map[string]any{"care_suggestions": parsed.CareSuggestions}
	b, _ := json.Marshal(labels)
	var round map[string]any
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	if round["care_suggestions"] == nil {
		t.Fatal("missing care_suggestions after marshal")
	}
}
