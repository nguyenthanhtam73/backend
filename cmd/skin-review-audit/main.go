// skin-review-audit reports self-contradictions in recent admin skin reviews.
//
// Purpose: find accuracy bugs WITHOUT needing anyone to label anything. A review that says
// "không thấy đỏ sưng" in a note while carrying a red-bump chip, or an analysis-level group
// that no region supports, is wrong on its own terms — no human verdict required. Measuring
// true accuracy still needs operator corrections plus the eval (see
// docs/SKIN-ACCURACY-LOOP.md); this catches the cheaper class of bug.
//
// Read-only. Prints field values and note excerpts; never downloads photos.
//
// Usage:
//
//	go run ./cmd/skin-review-audit --env .env.prod-eval.local
//	go run ./cmd/skin-review-audit --env .env.prod-eval.local --limit 40
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
)

// Concerns that assert visible redness/inflammation.
var redConcerns = map[string]struct{}{
	"papules": {}, "pustules": {}, "redness": {}, "irritation": {},
}

func main() {
	envPath := flag.String("env", ".env.prod-eval.local", "env file with prod DB credentials")
	limit := flag.Int("limit", 15, "how many recent reviews to audit")
	flag.Parse()

	cfg, err := config.Load(*envPath)
	if err != nil {
		fail("config: %v", err)
	}
	db, err := repository.NewPostgres(cfg)
	if err != nil {
		fail("database: %v", err)
	}

	var rows []domain.AdminSkinReview
	if err := db.Order("created_at DESC").Limit(*limit).Find(&rows).Error; err != nil {
		fail("query: %v", err)
	}
	if len(rows) == 0 {
		fmt.Println("No reviews yet.")
		return
	}

	fmt.Printf("=== SKIN REVIEW AUDIT (%d most recent) ===\n\n", len(rows))
	totalFindings := 0

	for _, r := range rows {
		a := dto.AdminSkinReviewAnalysis{}
		if len(r.Analysis) > 0 {
			_ = json.Unmarshal(r.Analysis, &a)
		}

		visible := make([]dto.AdminSkinAttentionArea, 0, len(a.AttentionAreas))
		chips := make([]string, 0, len(a.AttentionAreas))
		for _, ar := range a.AttentionAreas {
			switch normLower(ar.Concern) {
			case "", "none", "not_visible":
				continue
			}
			visible = append(visible, ar)
			chips = append(chips, ar.Region+"="+ar.Concern+"/"+ar.Severity)
		}

		findings := auditReview(r, a, visible)
		totalFindings += len(findings)

		fmt.Printf("%s  %s  corrected=%v\n",
			r.CreatedAt.Local().Format("2006-01-02 15:04"), r.ID.String()[:8], r.AnalysisCorrectedAt != nil)
		fmt.Printf("   group=%-24s confidence=%-7s ask_user=%v  skin_context=%v\n",
			orDash(a.MorphologyGroup), orDash(a.Confidence), a.NeedsMoreInfo, strings.TrimSpace(r.SkinContext) != "")
		fmt.Printf("   chips=%s\n", orDash(strings.Join(chips, " ")))
		for _, f := range findings {
			fmt.Printf("   !! %s\n", f)
		}
		fmt.Println()
	}

	fmt.Printf("=== %d finding(s) across %d review(s) ===\n", totalFindings, len(rows))
	if totalFindings == 0 {
		fmt.Println("No self-contradictions. Measuring true accuracy needs corrections + the eval.")
		return
	}
	fmt.Println("Each finding is a review that disagrees with itself, so it is a bug in the")
	fmt.Println("rules or the cue parser — not a judgement call. See docs/SKIN-ACCURACY-LOOP.md.")
}

// auditReview returns the self-contradictions in one review.
func auditReview(r domain.AdminSkinReview, a dto.AdminSkinReviewAnalysis, visible []dto.AdminSkinAttentionArea) []string {
	var out []string

	if strings.TrimSpace(a.Overview) == "" {
		return []string{"empty analysis (nothing to audit)"}
	}

	// A note that denies redness must not carry a red-bump chip.
	for _, ar := range visible {
		if _, isRed := redConcerns[normLower(ar.Concern)]; !isRed {
			continue
		}
		if ai.ProseDeniesRedness(ar.Note) {
			out = append(out, fmt.Sprintf("%s chip=%s but its note denies redness: %q",
				ar.Region, ar.Concern, clip(ar.Note, 90)))
		}
	}

	// Face regions must never be called skin tags.
	for _, ar := range visible {
		if ai.IsFaceRegion(ar.Region) && ai.ProseMentionsSkinTagOnFace(ar.Note) {
			out = append(out, fmt.Sprintf("%s note says mụn thịt on the face: %q", ar.Region, clip(ar.Note, 90)))
		}
	}

	group := ai.MorphologyGroup(strings.TrimSpace(a.MorphologyGroup))
	if group == "" {
		if len(visible) > 0 {
			out = append(out, "no morphology group recorded (cue parser did not recognise the wording, or the review predates the classifier)")
		}
		return out
	}

	// The analysis-level group must be supported by at least one region.
	supported := false
	for _, ar := range visible {
		v := ai.ClassifyMorphology(ai.MorphologyFeaturesFromProse(ar.Note, ar.Region))
		if v.Group == group {
			supported = true
			break
		}
	}
	if !supported {
		out = append(out, fmt.Sprintf("group=%s is not supported by any region note (chips: %s)",
			group, strings.Join(regionsOf(visible), ", ")))
	}

	// Calm-first groups should show up as a redness-ish chip somewhere.
	if ai.GroupImpliesRedness(group) {
		anyRed := false
		for _, ar := range visible {
			if _, isRed := redConcerns[normLower(ar.Concern)]; isRed {
				anyRed = true
				break
			}
		}
		if !anyRed {
			out = append(out, fmt.Sprintf("group=%s implies inflammation but no region carries a red chip", group))
		}
	}

	if a.NeedsMoreInfo && len(a.ClarifyQuestions) == 0 {
		out = append(out, "needs_more_info is set but there are no clarify questions to show")
	}
	if !a.NeedsMoreInfo && len(a.ClarifyQuestions) > 0 {
		out = append(out, "clarify questions present while needs_more_info is false")
	}
	return out
}

func regionsOf(areas []dto.AdminSkinAttentionArea) []string {
	out := make([]string, 0, len(areas))
	for _, ar := range areas {
		out = append(out, ar.Region+"="+ar.Concern)
	}
	return out
}

func normLower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
