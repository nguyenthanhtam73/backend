package ai

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
)

// TestPromptExamplesPassWardrobeValidation is the bug class PR #19 introduced:
// a prompt example the model copies must pass the server checker for the
// profile that example is about. Coconut oil and body products are the cases
// where a hedged sentence used to fail for missing "mặt" or for saying "bít".
func TestPromptExamplesPassWardrobeValidation(t *testing.T) {
	prompt := WardrobeProductInsightSystemPrompt()
	examples := map[string]string{}
	re := regexp.MustCompile(`EXAMPLE\s+([a-z.]+):\s+"([^"]+)"`)
	for _, m := range re.FindAllStringSubmatch(prompt, -1) {
		if _, dup := examples[m[1]]; dup {
			t.Fatalf("duplicate EXAMPLE %s", m[1])
		}
		examples[m[1]] = m[2]
	}
	required := []string{
		"coconut.what", "coconut.reason", "coconut.why", "coconut.gloss",
		"body.what", "body.reason", "body.why",
		"cleanser.what", "keep.reason", "keep.why", "pause.why",
	}
	for _, key := range required {
		if strings.TrimSpace(examples[key]) == "" {
			t.Fatalf("prompt missing EXAMPLE %s", key)
		}
	}
	if len(examples) != len(required) {
		t.Fatalf("EXAMPLE keys: %v", examples)
	}

	values := map[string]bool{}
	for _, v := range examples {
		values[v] = true
	}
	quoteRe := regexp.MustCompile(`"([^"]+)"`)
	for _, m := range quoteRe.FindAllStringSubmatch(prompt, -1) {
		q := m[1]
		if len([]rune(q)) < 20 || !hasVietnameseLetter(q) {
			continue
		}
		if values[q] || quoteIsPromptProhibition(q) || quoteFragmentOfExample(q, examples) {
			continue
		}
		t.Fatalf("quoted sentence is not a validated EXAMPLE or a prohibition: %q", q)
	}

	combo := reportedComboProfile()
	for _, profile := range []*domain.SkinProfile{combo, reportedComboProfileIDs()} {
		coconutFacts := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{
			Name:    "Dầu dừa nguyên chất",
			Profile: profile,
		})
		coconut := dto.WardrobeProductInsight{
			WhatItDoes: examples["coconut.what"],
			Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitNo, Reason: examples["coconut.reason"]},
			Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyNo, Why: examples["coconut.why"]},
			Actives:    []dto.WardrobeProductActive{{Name: coconutActiveName, Gloss: examples["coconut.gloss"]}},
			Disclaimer: dto.WardrobeInsightDisclaimer,
		}
		if problems := validateWardrobeProductInsight(coconut, coconutFacts); len(problems) != 0 {
			t.Fatalf("coconut prompt examples: %v\n%+v", problems, coconut)
		}
		if coconut.Fit.Reason != exampleCoconutReason {
			t.Fatalf("prompt coconut reason drifted from the spec sentence: %q", coconut.Fit.Reason)
		}

		bodyFacts := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{
			Name:    "Kem dưỡng thể",
			Profile: profile,
		})
		body := dto.WardrobeProductInsight{
			WhatItDoes: examples["body.what"],
			Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitNo, Reason: examples["body.reason"]},
			Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyNo, Why: examples["body.why"]},
			Disclaimer: dto.WardrobeInsightDisclaimer,
		}
		if problems := validateWardrobeProductInsight(body, bodyFacts); len(problems) != 0 {
			t.Fatalf("body prompt examples: %v\n%+v", problems, body)
		}
	}

	cleanserFacts := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{
		Name:     "Sữa rửa mặt tạo bọt",
		Brand:    "CeraVe",
		Category: "cleanser",
		Profile:  combo,
	})
	cleanser := dto.WardrobeProductInsight{
		WhatItDoes: examples["cleanser.what"],
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitYes, Reason: examples["keep.reason"]},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyYes, Why: examples["keep.why"]},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
	if problems := validateWardrobeProductInsight(cleanser, cleanserFacts); len(problems) != 0 {
		t.Fatalf("cleanser prompt examples: %v\n%+v", problems, cleanser)
	}

	pauseFacts := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{
		Name:    "Sữa rửa mặt tạo bọt",
		Profile: combo,
		Recent:  []domain.SkinCheck{{UserNote: "má đang rát"}},
	})
	pause := dto.WardrobeProductInsight{
		WhatItDoes: examples["cleanser.what"],
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitMaybe, Reason: "Da hỗn hợp có dấu hiệu rát gần đây, bạn cân nhắc tạm dừng."},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyNo, Why: examples["pause.why"]},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
	if problems := validateWardrobeProductInsight(pause, pauseFacts); len(problems) != 0 {
		t.Fatalf("pause prompt example: %v", problems)
	}

	for _, text := range []string{examples["coconut.what"], examples["cleanser.what"], examples["body.what"]} {
		if len([]rune(text)) > 160 {
			t.Fatalf("what_it_does too long (%d): %s", len([]rune(text)), text)
		}
	}
	for _, text := range []string{examples["coconut.reason"], examples["body.reason"], examples["keep.reason"]} {
		if len([]rune(text)) > 220 {
			t.Fatalf("reason too long (%d): %s", len([]rune(text)), text)
		}
	}
	for _, text := range []string{examples["coconut.why"], examples["body.why"], examples["keep.why"], examples["pause.why"]} {
		if len([]rune(text)) > 180 {
			t.Fatalf("why too long (%d): %s", len([]rune(text)), text)
		}
	}
	if n := len([]rune(examples["coconut.gloss"])); n > 90 {
		t.Fatalf("gloss too long (%d)", n)
	}
}

func TestPreFixCoconutExampleFailsValidation(t *testing.T) {
	facts := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{
		Name:    "Dầu dừa nguyên chất",
		Profile: reportedComboProfile(),
	})
	// PR #19 taught this shape. It hedges, but it has no "mặt", no plain why,
	// and what_it_does is only the name, so the checker rejected it twice and
	// the fallback card was stored.
	card := dto.WardrobeProductInsight{
		WhatItDoes: "Dầu dừa.",
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitNo, Reason: "Dầu dừa có thể chưa hợp với da bạn vì da hỗn hợp dễ nổi mụn."},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyNo, Why: "Có thể chưa hợp với da hỗn hợp dễ nổi mụn, bạn cân nhắc tạm dừng."},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
	if problems := validateWardrobeProductInsight(card, facts); len(problems) == 0 {
		t.Fatal("the old coconut example must fail validation")
	}
}

func TestFallbackCardLogsValidationProblems(t *testing.T) {
	var msgs []string
	var argBlobs []string
	prev := wardrobeInsightWarn
	wardrobeInsightWarn = func(msg string, args ...any) {
		msgs = append(msgs, msg)
		argBlobs = append(argBlobs, fmt.Sprint(args))
	}
	t.Cleanup(func() { wardrobeInsightWarn = prev })

	facts := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{
		Name:    "Dầu dừa nguyên chất",
		Profile: reportedComboProfile(),
	})
	bad := dto.WardrobeProductInsight{
		WhatItDoes: "Dầu dừa.",
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitNo, Reason: "Dầu dừa có thể chưa hợp với da bạn vì da hỗn hợp dễ nổi mụn."},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyNo, Why: "Bạn cân nhắc tạm dừng."},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
	problems := validateWardrobeProductInsight(bad, facts)
	if len(problems) == 0 {
		t.Fatal("expected problems")
	}
	got := finishWardrobeProductInsight(facts, problems, bad, nil)
	if got.WhatItDoes != exampleCoconutWhat || got.Fit.Reason != exampleCoconutReason || len(got.Actives) != 1 {
		t.Fatalf("fallback card: %+v", got)
	}
	if len(msgs) != 1 || msgs[0] != "wardrobe product insight: fallback card used" {
		t.Fatalf("log messages: %v", msgs)
	}
	if !strings.Contains(argBlobs[0], "problems") || !strings.Contains(argBlobs[0], "Dầu dừa nguyên chất") {
		t.Fatalf("log args: %s", argBlobs[0])
	}
	if !strings.Contains(argBlobs[0], problems[0][:24]) {
		t.Fatalf("log missing a validation problem: %s", argBlobs[0])
	}

	msgs = nil
	good := wardrobeInsightFallback(facts)
	kept := finishWardrobeProductInsight(facts, problems, good, nil)
	if kept.Fit.Reason != good.Fit.Reason {
		t.Fatalf("retry that passes should be kept: %+v", kept)
	}
	if len(msgs) != 0 {
		t.Fatalf("passing retry must not log a fallback: %v", msgs)
	}
}

func TestPorePhraseStaysHedgedAndSkinLimited(t *testing.T) {
	if hasVietnameseWord("dễ bít lỗ chân lông", "bí") {
		t.Fatal("bí matched inside bít")
	}
	if !hasVietnameseWord("kem này bí và nặng", "bí") {
		t.Fatal("standalone bí should match")
	}

	dry := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{
		Name:    "Kem dưỡng",
		Profile: &domain.SkinProfile{SkinType: "dry"},
	})
	if dry.AcneProne || dry.ClogProne {
		t.Fatalf("dry skin: acne %v clog %v", dry.AcneProne, dry.ClogProne)
	}
	plain := dto.WardrobeProductInsight{
		WhatItDoes: "Kem dưỡng thường dùng để dưỡng ẩm cho da.",
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitYes, Reason: "Hợp với da khô."},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyYes, Why: "Nên dùng tiếp vì hợp với da khô."},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
	if problems := validateWardrobeProductInsight(plain, dry); len(problems) != 0 {
		t.Fatalf("dry-skin keep card: %v", problems)
	}
	clogTalk := plain
	clogTalk.Fit.Reason = "Kem này khá đặc, có thể bít lỗ chân lông với da khô."
	if problems := validateWardrobeProductInsight(clogTalk, dry); len(problems) == 0 {
		t.Fatal("pore phrase on skin that is not acne-prone or clog-prone must fail")
	}

	concerns := []byte(`["comedone"]`)
	clogFacts := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{
		Name: "Kem dưỡng",
		Profile: &domain.SkinProfile{
			SkinType: "dry",
			Concerns: concerns,
		},
	})
	if clogFacts.AcneProne || !clogFacts.ClogProne {
		t.Fatalf("clog profile: acne %v clog %v concerns %v", clogFacts.AcneProne, clogFacts.ClogProne, clogFacts.Concerns)
	}
	allowed := plain
	allowed.Fit.Reason = "Kem này khá đặc, có thể bít lỗ chân lông với da khô."
	if problems := validateWardrobeProductInsight(allowed, clogFacts); len(problems) != 0 {
		t.Fatalf("hedged pore phrase should be allowed for clogged pores: %v", problems)
	}
}

func hasVietnameseLetter(s string) bool {
	for _, r := range s {
		if r > 127 {
			return true
		}
	}
	return false
}

func quoteFragmentOfExample(q string, examples map[string]string) bool {
	if len([]rune(q)) >= 40 {
		return false
	}
	for _, v := range examples {
		if strings.Contains(v, q) {
			return true
		}
	}
	return false
}

func quoteIsPromptProhibition(q string) bool {
	switch q {
	case "không nên bôi", "không nên bôi lên mặt", "đừng bôi", "cấm", "phải ngừng",
		"chắc chắn", "tuyệt đối", "sẽ gây mụn", "không phù hợp", "gây bệnh",
		"làm sạch mụn", "trị mụn", "hết mụn",
		"chưa có thông tin đầy đủ", "không có thông tin", "chưa đủ thông tin", "chưa có thông tin",
		"Sữa rửa mặt tạo bọt giúp làm sạch da dầu và dễ nổi mụn.",
		"dành cho da dầu và dễ nổi mụn",
		"mục tiêu giảm mụn",
		"có thể chưa hợp", "có thể khiến", "bạn cân nhắc", "có thể":
		return true
	default:
		return false
	}
}
