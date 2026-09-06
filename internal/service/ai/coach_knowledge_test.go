package ai

import (
	"net/url"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
)

func TestCoachKnowledgePack_LoadsAndStaysPublic(t *testing.T) {
	t.Parallel()
	pack, err := loadCoachKnowledgePack()
	if err != nil {
		t.Fatalf("load pack: %v", err)
	}
	if pack.ID != "dadiary-coach-public-knowledge" {
		t.Fatalf("unexpected pack id %q", pack.ID)
	}
	if pack.Version < 1 {
		t.Fatal("pack version must be >= 1")
	}
	lowScope := strings.ToLower(pack.Scope)
	if !strings.Contains(lowScope, "public") {
		t.Fatal("pack scope must say it is public-source only")
	}
	if !strings.Contains(lowScope, "no user diary") && !strings.Contains(lowScope, "no user") {
		t.Fatal("scope must forbid user-diary sources")
	}

	if len(pack.Themes) != 3 {
		t.Fatalf("expected 3 curated themes, got %d", len(pack.Themes))
	}
	got := make(map[string]bool, len(pack.Themes))
	for _, th := range pack.Themes {
		got[th.ID] = true
		if strings.TrimSpace(th.TitleVI) == "" || strings.TrimSpace(th.SummaryVI) == "" {
			t.Fatalf("theme %s missing Vietnamese title/summary", th.ID)
		}
		if len(th.DoVI) == 0 || len(th.DontVI) == 0 || len(th.SeeDoctorVI) == 0 {
			t.Fatalf("theme %s must have do / don't / see-doctor lines", th.ID)
		}
		if len(th.Sources) < 2 {
			t.Fatalf("theme %s needs at least 2 public citations", th.ID)
		}
		if len(th.MatchAny)+len(th.MatchPairs) == 0 {
			t.Fatalf("theme %s has no match rules", th.ID)
		}
		for _, src := range th.Sources {
			if !coachKnowledgeSourceHostAllowed(src.URL) {
				t.Fatalf("theme %s cites disallowed host %q", th.ID, src.URL)
			}
			u, err := url.Parse(src.URL)
			if err != nil || u.Scheme != "https" {
				t.Fatalf("theme %s source must be https: %q", th.ID, src.URL)
			}
			if strings.TrimSpace(src.Accessed) == "" || strings.TrimSpace(src.Title) == "" {
				t.Fatalf("theme %s source missing accessed/title", th.ID)
			}
		}
	}
	for _, id := range CoachKnowledgeThemeIDs {
		if !got[id] {
			t.Fatalf("missing required theme %s", id)
		}
	}
}

func TestCoachKnowledgePack_SafetyNeedles(t *testing.T) {
	t.Parallel()
	raw := string(coachKnowledgeJSON)
	for _, needle := range []string{
		"Không chẩn đoán",
		"không nặn",
		"khám",
		"chống nắng",
	} {
		if !strings.Contains(raw, needle) {
			t.Fatalf("pack missing safety needle %q", needle)
		}
	}
	for _, banned := range []string{
		"chữa khỏi nám",
		"hết nám trong",
		"nặn tại nhà được",
		"facebook.com",
		"skincare-review",
	} {
		if strings.Contains(strings.ToLower(raw), strings.ToLower(banned)) {
			t.Fatalf("pack must not contain %q", banned)
		}
	}
	if !strings.Contains(raw, "CẤM chẩn đoán nám chắc") {
		t.Fatal("pack must explicitly ban diagnosing melasma")
	}
}

func TestMatchCoachKnowledgeThemes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "adapalene irritation",
			in:   "User note: da rát bong sau 5 ngày dùng adapalene",
			want: []string{"irritation_after_adapalene_bha"},
		},
		{
			name: "differin without extra words",
			in:   "Em mới mua Differin gel",
			want: []string{"irritation_after_adapalene_bha"},
		},
		{
			name: "BHA plus sting",
			in:   "Dùng BHA 2% bị châm chích và đỏ rát",
			want: []string{"irritation_after_adapalene_bha"},
		},
		{
			name: "BHA alone is not irritation theme",
			in:   "Tối nay thêm BHA vào routine",
			want: nil,
		},
		{
			name: "melasma vs marks",
			in:   "Đây là thâm sau mụn hay nám vậy?",
			want: []string{"post_acne_marks_vs_melasma"},
		},
		{
			name: "nám diacritic",
			in:   "Da nám má hai bên",
			want: []string{"post_acne_marks_vs_melasma"},
		},
		{
			name: "nam gioi does not mean nám",
			in:   "User là nam giới, da ổn",
			want: nil,
		},
		{
			name: "closed comedones plus pick",
			in:   "Má nhiều mụn ẩn, muốn nặn cho sạch",
			want: []string{"oily_closed_comedones"},
		},
		{
			name: "oily plus mun",
			in:   "Da dầu nhiều mụn vùng chữ T",
			want: []string{"oily_closed_comedones"},
		},
		{
			name: "vision mụn ẩn",
			in:   `VISION_SUMMARY_JSON: {"visible_observations":["má: mụn ẩn thuần"]}`,
			want: []string{"oily_closed_comedones"},
		},
		{
			name: "unrelated check-in",
			in:   "Hôm nay ngủ đủ, trời mát, da ổn",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchCoachKnowledgeThemes(tc.in)
			if len(tc.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("want no themes, got %v", got)
				}
				return
			}
			gotSet := map[string]bool{}
			for _, id := range got {
				gotSet[id] = true
			}
			for _, id := range tc.want {
				if !gotSet[id] {
					t.Fatalf("missing theme %s in %v for %q", id, got, tc.in)
				}
			}
		})
	}
}

func TestRenderCoachKnowledgeBlock_EmptyWhenUnrelated(t *testing.T) {
	t.Parallel()
	if got := RenderCoachKnowledgeBlock("trời mát, ngủ đủ"); got != "" {
		t.Fatalf("unrelated text must not inject pack, got %q", got)
	}
}

func TestRenderCoachKnowledgeBlock_ThemeNeedles(t *testing.T) {
	t.Parallel()
	block := RenderCoachKnowledgeBlock("Da rát sau adapalene, thâm sau mụn hay nám, má nhiều mụn ẩn muốn nặn")
	for _, needle := range []string{
		"COACH_KNOWLEDGE",
		"irritation_after_adapalene_bha",
		"post_acne_marks_vs_melasma",
		"oily_closed_comedones",
		"Không chẩn đoán",
		"CẤM nặn",
		"Đi khám khi",
		"Nguồn (nội bộ",
		"AAD",
	} {
		mustContain(t, block, needle)
	}
	mustNotContain(t, block, "https://")
}

func TestCoachPrompt_IncludesPublicKnowledgeGuard(t *testing.T) {
	t.Parallel()
	guard := CoachPublicKnowledgeGuard()
	if strings.TrimSpace(guard) == "" {
		t.Fatal("guard must not be empty")
	}
	for _, skill := range []string{"beginner", "intermediate"} {
		p := GetCoachPrompt(skill)
		if !strings.Contains(p, guard) {
			t.Fatalf("%s prompt must concatenate CoachPublicKnowledgeGuard()", skill)
		}
		mustContain(t, p, "COACH_KNOWLEDGE")
		mustContain(t, p, "Không chẩn đoán")
	}
}

func TestBuildDailyFeedbackPrompt_InjectsKnowledge(t *testing.T) {
	t.Parallel()
	_, user := buildDailyFeedbackPrompt("User note: da rát bong sau adapalene", "beginner")
	mustContain(t, user, "COACH_KNOWLEDGE")
	mustContain(t, user, "irritation_after_adapalene_bha")
	mustContain(t, user, "dịu")

	_, quiet := buildDailyFeedbackPrompt("Hôm nay trời mát, da ổn", "beginner")
	mustNotContain(t, quiet, "COACH_KNOWLEDGE")
}

func TestBuildSkinCheckCoachUserMessage_InjectsKnowledge(t *testing.T) {
	t.Parallel()
	check := &domain.SkinCheck{UserNote: "Má nhiều mụn ẩn, muốn nặn"}
	msg := buildSkinCheckCoachUserMessage(check, nil, "", `{"visible_observations":["má: mụn ẩn"]}`, "ok", "")
	mustContain(t, msg, "COACH_KNOWLEDGE")
	mustContain(t, msg, "oily_closed_comedones")
	mustContain(t, msg, "CẤM nặn")
}

// TestCoachKnowledgeEvalNeedles is the offline eval note: sample user lines →
// required care directions in the injected block. No OpenAI spend.
func TestCoachKnowledgeEvalNeedles(t *testing.T) {
	t.Parallel()
	type evalCase struct {
		userLine string
		theme    string
		needles  []string
	}
	cases := []evalCase{
		{
			userLine: "Mới dùng adapalene 4 đêm, má bong và rát",
			theme:    "irritation_after_adapalene_bha",
			needles:  []string{"dưỡng ẩm", "chống nắng", "cách ngày", "khám"},
		},
		{
			userLine: "Thâm sau mụn hay nám má vậy ạ",
			theme:    "post_acne_marks_vs_melasma",
			needles:  []string{"không chốt", "chống nắng", "thâm sau mụn", "khám"},
		},
		{
			userLine: "Da dầu, mụn ẩn dày, tối nay nặn cho sạch",
			theme:    "oily_closed_comedones",
			needles:  []string{"CẤM nặn", "BHA", "chậm", "khám"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.theme, func(t *testing.T) {
			ids := MatchCoachKnowledgeThemes(tc.userLine)
			found := false
			for _, id := range ids {
				if id == tc.theme {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("eval line %q should match %s, got %v", tc.userLine, tc.theme, ids)
			}
			block := strings.ToLower(RenderCoachKnowledgeBlock(tc.userLine))
			for _, n := range tc.needles {
				if !strings.Contains(block, strings.ToLower(n)) {
					t.Fatalf("eval %s missing needle %q in block", tc.theme, n)
				}
			}
		})
	}
}
