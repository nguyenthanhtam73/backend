package ai

import (
	"strings"
	"testing"
)

func TestParseAdminSkinReviewSuggestedAnswer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"json", `{"answer":"Má của mày đang đỏ viêm."}`, "Má của mày đang đỏ viêm."},
		{"fenced", "```json\n{\"answer\":\"Ok.\"}\n```", "Ok."},
		{"bare prose", "Má của mày đang có cụm mụn viêm đỏ sưng. Rửa dịu thôi.", "Má của mày đang có cụm mụn viêm đỏ sưng. Rửa dịu thôi."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseAdminSkinReviewSuggestedAnswer(tc.raw)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestAdminSkinEmptyOrRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
		meta    adminSkinVisionMeta
		want    bool
	}{
		{"ok content", `{"overview":"x"}`, adminSkinVisionMeta{}, false},
		{"empty content", "", adminSkinVisionMeta{FinishReason: "stop"}, true},
		{"whitespace content", "   ", adminSkinVisionMeta{}, true},
		{"refusal with empty", "", adminSkinVisionMeta{Refusal: "I'm sorry, I can't assist with that."}, true},
		{"refusal with content", `{"a":1}`, adminSkinVisionMeta{Refusal: "can't assist"}, true},
		{"content apology refuse", "I'm sorry, I can't assist with that.", adminSkinVisionMeta{}, true},
		{"json ok not refuse", `{"overview":"Má đỏ nhẹ."}`, adminSkinVisionMeta{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := adminSkinEmptyOrRefused(tc.content, tc.meta); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestAcceptExpandedAdminSkinNote(t *testing.T) {
	t.Parallel()
	orig := "Má có vài nốt đỏ sưng. Màu đỏ nhẹ."
	good := "Má của mày đang có cụm mụn viêm đỏ sưng. Khoảng vài hạt hơi nổi gần giữa má. Màu đỏ nhẹ, mức vừa. Chưa thấy đầu trắng rõ. Đây đúng kiểu dầu bít tắc tại chỗ. Má đang đỏ rõ đó."
	if !acceptExpandedAdminSkinNote(orig, good) {
		t.Fatal("expected thicker clean rewrite to be accepted")
	}
	if acceptExpandedAdminSkinNote(orig, orig) {
		t.Fatal("same-length rewrite must be rejected")
	}
	if acceptExpandedAdminSkinNote(orig, good+" Nên dùng CeraVe.") {
		t.Fatal("brand rewrite must be rejected")
	}
	if acceptExpandedAdminSkinNote(orig, "Má có vài nốt. Nên thoa retinol nhẹ.") {
		t.Fatal("retinol rewrite must be rejected")
	}
	if acceptExpandedAdminSkinNote(orig, "Má có papules mức moderate. Có erythema.") {
		t.Fatal("jargon rewrite must be rejected")
	}
	if acceptExpandedAdminSkinNote(orig, "") {
		t.Fatal("empty rewrite must be rejected")
	}
	avoidAdvice := "Má của mày đang có cụm mụn viêm đỏ sưng. Khoảng vài hạt hơi nổi gần giữa má. Màu đỏ nhẹ, mức vừa. Không nên dùng tay bẩn chạm mặt. Đây đúng kiểu dầu bít tắc tại chỗ. Má đang đỏ rõ đó."
	if !acceptExpandedAdminSkinNote(orig, avoidAdvice) {
		t.Fatal("không nên dùng (avoid advice) must not false-positive ban")
	}

	// New hedge phrases must be rejected (full phrases only).
	withHedge := good + " Không chắc 100% chỉ từ một ảnh."
	if acceptExpandedAdminSkinNote(orig, withHedge) {
		t.Fatal("new 'không chắc 100%' hedge must be rejected")
	}
	withNghi := "Má của mày đang có cụm mụn viêm. Khoảng vài hạt hơi nổi gần giữa má. Màu đỏ nhẹ. Trên ảnh nghi dầu bít. Đây đúng kiểu kích ứng tại chỗ. Má đang đỏ rõ đó."
	if acceptExpandedAdminSkinNote(orig, withNghi) {
		t.Fatal("new 'trên ảnh nghi' hedge must be rejected")
	}
	withDoiKhi := "Má của mày đang có cụm mụn viêm. Khoảng vài hạt hơi nổi gần giữa má. Màu đỏ nhẹ. Đôi khi liên quan cọ xát. Đây đúng kiểu dầu bít. Má đang đỏ rõ đó."
	if acceptExpandedAdminSkinNote(orig, withDoiKhi) {
		t.Fatal("new 'đôi khi liên quan' hedge must be rejected")
	}
	withChuaChac := "Má của mày đang có cụm mụn viêm. Khoảng vài hạt hơi nổi gần giữa má. Màu đỏ nhẹ, chưa chắc đầu trắng. Đây đúng kiểu dầu bít. Má đang đỏ rõ đó."
	if acceptExpandedAdminSkinNote(orig, withChuaChac) {
		t.Fatal("new 'chưa chắc' hedge must be rejected")
	}
	// Bare "có thể" / "nghi" alone must NOT false-positive.
	withCoThe := "Má của mày đang có cụm mụn viêm. Khoảng vài hạt hơi nổi gần giữa má. Màu đỏ nhẹ. Crop hẹp nên mật độ có thể trông dày hơn. Đây đúng kiểu dầu bít. Má đang đỏ rõ đó."
	if !acceptExpandedAdminSkinNote(orig, withCoThe) {
		t.Fatal("bare 'có thể' must not false-positive hedge ban")
	}
	withCoTheLa := "Má của mày đang có cụm mụn viêm. Khoảng vài hạt hơi nổi gần giữa má. Màu đỏ nhẹ. Có thể là dầu bít tắc. Đây đúng kiểu kích ứng tại chỗ. Má đang đỏ rõ đó."
	if acceptExpandedAdminSkinNote(orig, withCoTheLa) {
		t.Fatal("new 'có thể là' hedge must be rejected")
	}
	// Preserving a hedge already in the original is allowed (not newly added).
	origWithHedge := "Má có vài nốt đỏ sưng. Không chắc 100% chỉ từ một ảnh."
	preserve := "Má của mày đang có cụm mụn viêm đỏ sưng. Khoảng vài hạt hơi nổi gần giữa má. Màu đỏ nhẹ, mức vừa. Không chắc 100% chỉ từ một ảnh. Đây đúng kiểu dầu bít tắc tại chỗ."
	if !acceptExpandedAdminSkinNote(origWithHedge, preserve) {
		t.Fatal("preserving original hedge phrase should still accept thicker rewrite")
	}
}

func TestAdminSkinReviewUserTextCompact(t *testing.T) {
	t.Parallel()
	full := adminSkinReviewUserText("vi", false)
	compact := adminSkinReviewUserText("vi", true)
	if len(compact) >= len(full) {
		t.Fatalf("compact user text should be shorter: compact=%d full=%d", len(compact), len(full))
	}
	for _, needle := range []string{"possible_causes", "soothing_tips", "Retry"} {
		if !strings.Contains(compact, needle) {
			t.Fatalf("compact text missing %q", needle)
		}
	}
	sys := AdminSkinReviewCompactSystemPrompt()
	for _, needle := range []string{"possible_causes", "soothing_tips", "Observations-first"} {
		if !strings.Contains(sys, needle) {
			t.Fatalf("compact system missing %q", needle)
		}
	}
}
