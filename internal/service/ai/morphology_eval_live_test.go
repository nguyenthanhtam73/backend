package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
)

// morphology_eval_live_test.go — REAL-PHOTO EVAL (opt-in).
//
// This is how "nhận xét chính xác hơn" stops being a guess: point it at labeled photos
// and it prints per-group accuracy plus every miss. Without it, a prompt or model change
// can fix milia and silently break texture with nobody noticing.
//
// Setup — normally you do not write the manifest by hand:
//
//	go run ./cmd/eval-morphology-export        # turns admin corrections into labels
//	DADIARY_OPENAI_API_KEY=... go test ./internal/service/ai/ -run TestMorphologyEvalLive -v
//
// Manual entries work too:
//
//	[{"file":"cheek-milia-01.jpg","region":"cheeks","want_group":"milia_like",
//	  "skin_context":"Sờ: cứng như hạt cát. Bao lâu: nhiều tháng, không đổi."}]
//
// want_group holds a MorphologyGroup constant (milia_like, rough_texture, skin_tag,
// closed_comedones, inflamed_acne, pustules, pigment, neck_crease, …). Some corrected
// enums legitimately cover several groups (`acne`, `redness`), so those cases carry
// want_concern instead and are scored on the concern enum — better than inventing a label.

type morphologyEvalCase struct {
	File      string `json:"file"`
	Region    string `json:"region"`
	WantGroup string `json:"want_group"`
	// WantConcern scores the attention-area enum when no single group maps to it.
	WantConcern string `json:"want_concern"`
	SkinContext string `json:"skin_context"`
	Question    string `json:"question"`
	// AIConcern is what the model said before the operator corrected it (context only).
	AIConcern string `json:"ai_concern"`
}

func (c morphologyEvalCase) label() string {
	if g := strings.TrimSpace(c.WantGroup); g != "" {
		return g
	}
	return "concern:" + strings.TrimSpace(c.WantConcern)
}

const morphologyEvalDir = "testdata/morphology"

func TestMorphologyEvalLive(t *testing.T) {
	manifestPath := filepath.Join(morphologyEvalDir, "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Skipf("no labeled photo set at %s — see this file's header to create one", manifestPath)
	}
	var cases []morphologyEvalCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("manifest.json is not a valid case list: %v", err)
	}
	if len(cases) == 0 {
		t.Skip("manifest.json has no cases")
	}
	apiKey := strings.TrimSpace(os.Getenv("DADIARY_OPENAI_API_KEY"))
	if apiKey == "" {
		t.Skip("DADIARY_OPENAI_API_KEY not set — live eval needs a real vision call")
	}

	cfg := &config.Config{OpenAI: config.OpenAIConfig{APIKey: apiKey}}
	httpClient := &http.Client{Timeout: 5 * time.Minute}

	type groupStat struct{ hit, total int }
	stats := map[string]*groupStat{}
	var misses []string

	for _, c := range cases {
		path := filepath.Join(morphologyEvalDir, c.File)
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Errorf("read %s: %v", path, rerr)
			continue
		}
		want := c.label()
		if want == "concern:" {
			t.Errorf("%s: manifest entry has neither want_group nor want_concern", c.File)
			continue
		}
		if stats[want] == nil {
			stats[want] = &groupStat{}
		}
		stats[want].total++

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		analysis, _, aerr := AdminSkinReviewAnalyzeWithContext(
			ctx, cfg, httpClient, []ImageBytes{{Data: data}}, "vi", c.Question, c.SkinContext,
		)
		cancel()
		if aerr != nil {
			misses = append(misses, fmt.Sprintf("  %-34s ERROR %v", c.File, aerr))
			continue
		}

		area, _ := adminPrimaryProblemArea(analysis)
		got := strings.TrimSpace(analysis.MorphologyGroup)
		if strings.TrimSpace(c.WantGroup) == "" {
			// Concern-scored case: compare the enum the reviewer actually corrected.
			got = "concern:" + strings.ToLower(strings.TrimSpace(area.Concern))
		}
		if got == want {
			stats[want].hit++
			continue
		}
		misses = append(misses, fmt.Sprintf("  %-34s want=%-24s got=%-24s concern=%-13s conf=%s ai_before=%s\n      note: %s",
			c.File, want, got, area.Concern, analysis.Confidence, c.AIConcern, firstNRunes(area.Note, 150)))
	}

	groups := make([]string, 0, len(stats))
	for g := range stats {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	totalHit, totalAll := 0, 0
	var report strings.Builder
	report.WriteString("\n=== MORPHOLOGY EVAL (real photos) ===\n")
	for _, g := range groups {
		s := stats[g]
		totalHit += s.hit
		totalAll += s.total
		report.WriteString(fmt.Sprintf("  %-24s %d/%d\n", g, s.hit, s.total))
	}
	report.WriteString(fmt.Sprintf("  %-24s %d/%d\n", "TOTAL", totalHit, totalAll))
	if len(misses) > 0 {
		report.WriteString("\n--- misses ---\n")
		report.WriteString(strings.Join(misses, "\n"))
		report.WriteString("\n")
	}
	t.Log(report.String())

	if totalAll > 0 && totalHit == 0 {
		t.Fatal("every labeled photo was classified into the wrong group — check the manifest labels and the rules in vision_morphology.go")
	}
}

func firstNRunes(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
