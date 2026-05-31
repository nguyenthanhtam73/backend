package ai

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/dto"
)

func TestEvaluateAffiliateSuggestions_GoodFixture(t *testing.T) {
	rows, _ := loadAffiliateCatalog()
	if len(rows) == 0 {
		t.Fatal("empty catalog")
	}
	spf := findCatalogByCategory(rows, "spf")
	if spf == nil {
		t.Fatal("no spf in catalog")
	}
	sc := affiliateMissingSPF()
	out := AffiliateTurnOutput{
		Suggestions: []dto.ProductSuggestion{
			{
				ProductName:   spf.ProductName,
				Brand:         spf.Brand,
				Reason:        "Hôm qua bạn ra nắng 2 tiếng mà chưa có kem chống nắng trong tủ — Anthelios giúp bảo vệ vùng má khỏi thâm thêm. Link affiliate có thể giúp DaDiary duy trì app (hoa hồng nhỏ).",
				AffiliateLink: spf.AffiliateLink,
				Priority:      "high",
			},
		},
	}
	res := EvaluateAffiliateSuggestions(sc, "daily_feedback", out)
	if res.Count != 1 {
		t.Fatalf("count=%d want 1", res.Count)
	}
	if !res.AllInCatalog || !res.HasTransparency || !res.ReasonSpecific || !res.RespectsWardrobe {
		t.Fatalf("expected pass: %+v issues=%v", res, res.Issues)
	}
	if res.Score < 0.85 {
		t.Fatalf("score too low: %.2f", res.Score)
	}
}

func TestEvaluateAffiliateSuggestions_MissingTransparency(t *testing.T) {
	rows, _ := loadAffiliateCatalog()
	if len(rows) == 0 {
		t.Fatal("empty catalog")
	}
	sc := affiliateBeginnerOilyAcne()
	res := EvaluateAffiliateSuggestions(sc, "routine_suggest", AffiliateTurnOutput{
		Suggestions: []dto.ProductSuggestion{
			{
				ProductName:   rows[0].ProductName,
				Brand:         rows[0].Brand,
				Reason:        "Vùng trán có vài nốt mụn và T-zone bóng dầu — sữa rửa mặt dịu giúp làm sạch mà không căng da.",
				AffiliateLink: rows[0].AffiliateLink,
				Priority:      "high",
			},
		},
	})
	if res.HasTransparency {
		t.Fatal("expected transparency failure")
	}
	if len(res.Issues) == 0 || !strings.Contains(res.Issues[0], "transparency") {
		t.Fatalf("expected transparency issue, got %v", res.Issues)
	}
}

func TestEvaluateAffiliateSuggestions_WardrobeHit(t *testing.T) {
	sc := affiliateWardrobeFull()
	owned := sc.Wardrobe[0]
	res := EvaluateAffiliateSuggestions(sc, "daily_feedback", AffiliateTurnOutput{
		Suggestions: []dto.ProductSuggestion{
			{
				ProductName:   owned.Name,
				Brand:         owned.Brand,
				Reason:        "Má căng nhẹ hôm nay — sữa rửa mặt dịu phù hợp. Link affiliate hoa hồng nhỏ.",
				AffiliateLink: "https://s.shopee.vn/affiliate/cerave-foaming-cleanser",
				Priority:      "medium",
			},
		},
	})
	if res.Count != 0 {
		t.Fatalf("owned product should be dropped, count=%d", res.Count)
	}
	if !res.RespectsWardrobe {
		t.Fatalf("expected wardrobe respect")
	}
}

func TestFinalizeProductSuggestions_ParsesWardrobe(t *testing.T) {
	ctx := `USER_MEMORY:
## Wardrobe (products user already owns — DO NOT re-recommend these)
- Kem chống nắng Anthelios SPF50+ | brand: La Roche-Posay | category: spf
`
	rows, _ := loadAffiliateCatalog()
	spf := findCatalogByCategory(rows, "spf")
	if spf == nil {
		t.Fatal("no spf")
	}
	out := FinalizeProductSuggestions([]dto.ProductSuggestion{
		{ProductName: spf.ProductName, Brand: spf.Brand, Reason: "test", AffiliateLink: spf.AffiliateLink},
	}, ctx)
	if len(out) != 0 {
		t.Fatalf("should drop owned SPF, got %d", len(out))
	}
}

func findCatalogByCategory(rows []affiliateCatalogEntry, cat string) *affiliateCatalogEntry {
	for i := range rows {
		if rows[i].Category == cat {
			return &rows[i]
		}
	}
	return nil
}

// TestAffiliateScenarioLive runs real LLM calls for all 6 affiliate scenarios.
// Run: go test ./internal/service/ai/... -run TestAffiliateScenarioLive -v -count=1
func TestAffiliateScenarioLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live affiliate test in short mode")
	}
	cfg := loadCoachTestConfig(t)
	if strings.TrimSpace(cfg.Anthropic.APIKey) == "" && strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		t.Skip("set DADIARY_ANTHROPIC_API_KEY or DADIARY_OPENAI_API_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	var report []AffiliateEvalResult
	for _, sc := range AffiliateScenarios() {
		t.Run(sc.ID+"/daily_feedback", func(t *testing.T) {
			out, err := GenerateDailyFeedback(ctx, cfg, sc.FullContextWithMemory(), sc.SkillLevel)
			if err != nil {
				t.Fatalf("daily feedback: %v", err)
			}
			res := EvaluateAffiliateSuggestions(sc, "daily_feedback", AffiliateTurnOutput{
				Suggestions: out.ProductSuggestions,
				Rationale:   out.SituationAnalysis,
			})
			report = append(report, res)
			t.Logf("products=%v score=%.0f%% issues=%v", res.Products, res.Score*100, res.Issues)
			logAffiliateFailures(t, res)
		})
		t.Run(sc.ID+"/routine_suggest", func(t *testing.T) {
			r, err := GenerateSuggestedRoutine(ctx, cfg, SuggestRoutineInput{
				Profile:    sc.Profile,
				LastCheck:  sc.TodayCheck,
				Locale:     "vi",
				SkillMode:  sc.SkillLevel,
				UserMemory: sc.Memory,
			})
			if err != nil {
				t.Fatalf("routine suggest: %v", err)
			}
			res := EvaluateAffiliateSuggestions(sc, "routine_suggest", AffiliateTurnOutput{
				Suggestions: r.ProductSuggestions,
				Rationale:   r.Rationale,
			})
			report = append(report, res)
			t.Logf("products=%v score=%.0f%% issues=%v", res.Products, res.Score*100, res.Issues)
			logAffiliateFailures(t, res)
		})
	}
	t.Log("\n--- Affiliate eval report ---\n" + FormatAffiliateEvalReport(report))
	summarizeAffiliateReport(t, report)
}

func logAffiliateFailures(t *testing.T, res AffiliateEvalResult) {
	t.Helper()
	if res.Score >= 0.85 && len(res.Issues) == 0 {
		return
	}
	for _, issue := range res.Issues {
		t.Logf("WARN [%s/%s]: %s", res.ScenarioID, res.Pipeline, issue)
	}
}

func summarizeAffiliateReport(t *testing.T, rows []AffiliateEvalResult) {
	t.Helper()
	if len(rows) == 0 {
		return
	}
	var sum float64
	fail := 0
	for _, r := range rows {
		sum += r.Score
		if r.Score < 0.85 || len(r.Issues) > 0 {
			fail++
		}
	}
	avg := sum / float64(len(rows))
	t.Logf("SUMMARY: %d runs, avg score %.0f%%, below-target runs=%d", len(rows), avg*100, fail)
	if fail > len(rows)/2 {
		t.Errorf("more than half of affiliate runs below target (%.0f%% avg) — review prompts/catalog", avg*100)
	}
}
