package ai

import (
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestTitleFromUserQuestion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, want string
	}{
		{
			name: "strips polite lead-in and tail, keeps the question",
			in:   "Cho em hỏi má em nổi nhiều nốt nhỏ màu da là bị gì ạ, em cảm ơn",
			want: "Má em nổi nhiều nốt nhỏ màu da là bị gì?",
		},
		{
			name: "keeps an existing question mark",
			in:   "Thâm hai bên mép miệng và dưới cằm do đâu?",
			want: "Thâm hai bên mép miệng và dưới cằm do đâu?",
		},
		{
			name: "first sentence only",
			in:   "Da em bị mụn ẩn ở má. Em dùng sữa rửa mặt Cerave với toner mỗi ngày rồi mà không đỡ.",
			want: "Da em bị mụn ẩn ở má",
		},
		{
			name: "statement stays a statement",
			in:   "Cổ em có nhiều nốt màu da nổi cao",
			want: "Cổ em có nhiều nốt màu da nổi cao",
		},
		{
			name: "empty stays empty",
			in:   "   ",
			want: "",
		},
		{
			name: "only politeness leaves nothing",
			in:   "cảm ơn mọi người ạ",
			want: "",
		},
	}
	for _, c := range cases {
		if got := TitleFromUserQuestion(c.in, "vi"); got != c.want {
			t.Errorf("%s:\n  in   %q\n  got  %q\n  want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestTitleFromUserQuestion_LengthCapped(t *testing.T) {
	t.Parallel()
	long := "Má em nổi rất nhiều nốt nhỏ màu da suốt mấy tháng nay không hết và em đã thử đủ loại sữa rửa mặt khác nhau"
	got := TitleFromUserQuestion(long, "vi")
	if n := len([]rune(got)); n > maxShareTitleRunes+1 { // +1 for an appended "?"
		t.Fatalf("title too long (%d runes): %q", n, got)
	}
	if strings.HasSuffix(got, " ") || strings.Contains(got, "  ") {
		t.Fatalf("title not tidy: %q", got)
	}
}

func TestPublicShareTitle_FallsBackToTheFinding(t *testing.T) {
	t.Parallel()
	a := &dto.AdminSkinReviewAnalysis{
		MorphologyGroup: string(GroupMiliaLike),
		AttentionAreas: []dto.AdminSkinAttentionArea{
			{Region: "cheeks", Concern: "acne", Severity: "moderate", Note: "note"},
		},
	}
	got := PublicShareTitle(a, "", "vi")
	if got != "Mụn ẩn hoặc milia ở má" {
		t.Fatalf("got %q", got)
	}

	// A group label that already names the region must not repeat it.
	neck := &dto.AdminSkinReviewAnalysis{
		MorphologyGroup: string(GroupNeckCrease),
		AttentionAreas: []dto.AdminSkinAttentionArea{
			{Region: "neck", Concern: "texture", Severity: "mild", Note: "note"},
		},
	}
	if got := PublicShareTitle(neck, "", "vi"); strings.Count(strings.ToLower(got), "cổ") > 1 {
		t.Fatalf("region repeated in title: %q", got)
	}

	// No group recorded (older reviews) → fall back to the concern wording.
	old := &dto.AdminSkinReviewAnalysis{
		AttentionAreas: []dto.AdminSkinAttentionArea{
			{Region: "chin", Concern: "pigmentation", Severity: "mild", Note: "note"},
		},
	}
	if got := PublicShareTitle(old, "", "vi"); got != "Thâm ở cằm" {
		t.Fatalf("legacy fallback: got %q", got)
	}

	// Nothing usable at all → empty, so the frontend keeps its own fallback.
	if got := PublicShareTitle(&dto.AdminSkinReviewAnalysis{}, "", "vi"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

// The user's question outranks the derived description: it is the phrase people search.
func TestPublicShareTitle_PrefersTheQuestion(t *testing.T) {
	t.Parallel()
	a := &dto.AdminSkinReviewAnalysis{
		MorphologyGroup: string(GroupMiliaLike),
		AttentionAreas: []dto.AdminSkinAttentionArea{
			{Region: "cheeks", Concern: "acne", Severity: "moderate", Note: "note"},
		},
	}
	got := PublicShareTitle(a, "Nốt trên má có phải mụn thịt không ạ", "vi")
	if !strings.Contains(got, "mụn thịt") {
		t.Fatalf("expected the question to win, got %q", got)
	}
}

// Two different reviews must not produce the same title — that was the original bug.
func TestPublicShareTitle_DistinctAcrossReviews(t *testing.T) {
	t.Parallel()
	seen := map[string]int{}
	inputs := []struct {
		group, region, concern, question string
	}{
		{string(GroupMiliaLike), "cheeks", "acne", ""},
		{string(GroupRoughTexture), "cheeks", "texture", ""},
		{string(GroupPigment), "chin", "pigmentation", ""},
		{string(GroupSkinTag), "neck", "other", ""},
		{string(GroupPustules), "forehead", "pustules", ""},
		{string(GroupMiliaLike), "cheeks", "acne", "Má nổi nốt nhỏ màu da là gì ạ"},
	}
	for _, in := range inputs {
		a := &dto.AdminSkinReviewAnalysis{
			MorphologyGroup: in.group,
			AttentionAreas: []dto.AdminSkinAttentionArea{
				{Region: in.region, Concern: in.concern, Severity: "moderate", Note: "note"},
			},
		}
		title := PublicShareTitle(a, in.question, "vi")
		if title == "" {
			t.Fatalf("no title for %+v", in)
		}
		seen[title]++
	}
	for title, n := range seen {
		if n > 1 {
			t.Fatalf("duplicate title %q produced %d times", title, n)
		}
	}
}
