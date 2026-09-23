package dto

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestParseWardrobeProductInsight_LocksCardFields(t *testing.T) {
	raw := []byte(`{
		"what_it_does": "  Sữa rửa mặt dịu, lấy dầu thừa mà không kéo căng.  ",
		"fit": {"verdict": "YES", "reason": "Da dầu, check-in gần đây còn bóng vùng chữ T."},
		"buy": {"advice": "Should buy", "why": "Tủ chưa có sữa rửa mặt. không thay bác sĩ da liễu"},
		"actives": [
			{"name": " Ceramide ", "gloss": "giữ lớp bảo vệ da khỏi khô rát"},
			{"name": "Ceramide", "gloss": "trùng"},
			{"name": "BHA", "gloss": ""},
			{"name": "", "gloss": "bỏ"}
		],
		"disclaimer": "tự chữa được"
	}`)
	got, err := ParseWardrobeProductInsight(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.WhatItDoes != "Sữa rửa mặt dịu, lấy dầu thừa mà không kéo căng." {
		t.Fatalf("what: %q", got.WhatItDoes)
	}
	if got.Fit.Verdict != WardrobeFitYes || got.Fit.Reason == "" {
		t.Fatalf("fit: %+v", got.Fit)
	}
	if got.Buy.Advice != WardrobeBuyYes || got.Buy.Why != "Tủ chưa có sữa rửa mặt." {
		t.Fatalf("buy: %+v", got.Buy)
	}
	if got.Disclaimer != WardrobeInsightDisclaimer {
		t.Fatalf("disclaimer: %q", got.Disclaimer)
	}
	if len(got.Actives) != 1 || got.Actives[0].Name != "Ceramide" || got.Actives[0].Gloss == "" {
		t.Fatalf("actives: %+v", got.Actives)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(encoded, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"what_it_does", "fit", "buy", "actives", "disclaimer"} {
		if _, ok := m[key]; !ok {
			t.Fatalf("FE field %q missing in %s", key, encoded)
		}
	}
	fit, _ := m["fit"].(map[string]any)
	if fit["verdict"] != "yes" || strings.TrimSpace(fit["reason"].(string)) == "" {
		t.Fatalf("fit json: %#v", fit)
	}
	buy, _ := m["buy"].(map[string]any)
	if buy["advice"] != "nên mua" || strings.TrimSpace(buy["why"].(string)) == "" {
		t.Fatalf("buy json: %#v", buy)
	}
}

func TestParseWardrobeProductInsight_NoFitForcesWait(t *testing.T) {
	raw := []byte(`{
		"what_it_does": "Toner cân bằng.",
		"fit": {"verdict": "không hợp", "reason": "Da đang rát sau check-in hôm qua."},
		"buy": {"advice": "nên mua", "why": "Giá ổn."}
	}`)
	got, err := ParseWardrobeProductInsight(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fit.Verdict != WardrobeFitNo {
		t.Fatalf("verdict: %q", got.Fit.Verdict)
	}
	if got.Buy.Advice != WardrobeBuyNo {
		t.Fatalf("advice should wait when fit is no, got %q", got.Buy.Advice)
	}
	if got.Buy.Why != "Giá ổn." {
		t.Fatalf("why kept: %q", got.Buy.Why)
	}
	if len(got.Actives) != 0 {
		t.Fatalf("actives: %+v", got.Actives)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"actives"`) {
		t.Fatalf("empty actives should be omitted: %s", encoded)
	}
}

func TestParseWardrobeProductInsight_UnknownSkinDoesNotInventFit(t *testing.T) {
	raw := []byte(`{
		"what_it_does": "Kem chống nắng.",
		"fit": {"verdict": "yes", "reason": "Da dầu nên hợp."},
		"buy": {"advice": "nên mua", "why": "Nên mua ngay."}
	}`)
	got, err := ParseWardrobeProductInsight(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fit.Verdict != WardrobeFitMaybe || got.Fit.Reason != insightUnknownFitReason {
		t.Fatalf("fit: %+v", got.Fit)
	}
	if got.Buy.Advice != WardrobeBuyNo || got.Buy.Why != insightUnknownBuyWhy {
		t.Fatalf("buy: %+v", got.Buy)
	}
	if got.Disclaimer != WardrobeInsightDisclaimer {
		t.Fatalf("disclaimer: %q", got.Disclaimer)
	}
}

func TestParseWardrobeProductInsight_RejectsEmptyAndHuge(t *testing.T) {
	if _, err := ParseWardrobeProductInsight([]byte(`{"fit":{"verdict":"yes"}}`), true); err == nil {
		t.Fatal("expected missing what_it_does")
	}
	if _, err := ParseWardrobeProductInsight([]byte(`not-json`), true); err == nil {
		t.Fatal("expected parse error")
	}
	huge := []byte(`{"what_it_does":"` + strings.Repeat("a", maxInsightJSON) + `"}`)
	if _, err := ParseWardrobeProductInsight(huge, true); err == nil {
		t.Fatal("expected size error")
	}
}

func TestParseWardrobeProductInsight_CapsActives(t *testing.T) {
	var actives []string
	for i := 0; i < 8; i++ {
		actives = append(actives, `{"name":"A`+string(rune('0'+i))+`","gloss":"một ý ngắn"}`)
	}
	raw := []byte(`{"what_it_does":"Serum.","fit":{"verdict":"maybe","reason":"Tùy da."},"buy":{"advice":"chưa nên","why":"Chưa rõ nồng độ."},"actives":[` + strings.Join(actives, ",") + `]}`)
	got, err := ParseWardrobeProductInsight(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Actives) != maxWardrobeActives {
		t.Fatalf("got %d actives", len(got.Actives))
	}
	if got.Buy.Advice != WardrobeBuyNo || got.Fit.Verdict != WardrobeFitMaybe {
		t.Fatalf("got %+v", got)
	}
}

func TestWardrobeProductFromDomain_MapsInsight(t *testing.T) {
	at := time.Date(2026, 9, 23, 1, 2, 3, 0, time.UTC)
	card := WardrobeProductInsight{
		WhatItDoes: "Kem dưỡng nhẹ.",
		Fit:        WardrobeProductFit{Verdict: WardrobeFitMaybe, Reason: "Da hỗn hợp."},
		Buy:        WardrobeProductBuy{Advice: WardrobeBuyNo, Why: "Đang có kem tương tự."},
		Disclaimer: "bỏ dòng này",
	}
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	p := &domain.SkincareProduct{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		Name:      "Kem dưỡng",
		CreatedAt: at,
		UpdatedAt: at,
		Insight:   raw,
		InsightAt: &at,
	}
	got := WardrobeProductFromDomain(p)
	if got.Insight == nil {
		t.Fatal("expected insight")
	}
	if got.Insight.Disclaimer != WardrobeInsightDisclaimer {
		t.Fatalf("stored disclaimer must be replaced, got %q", got.Insight.Disclaimer)
	}
	if got.Insight.WhatItDoes != "Kem dưỡng nhẹ." || got.InsightAt != "2026-09-23T01:02:03Z" {
		t.Fatalf("mapped: %+v at %q", got.Insight, got.InsightAt)
	}

	p.Insight = []byte(`{not json`)
	got = WardrobeProductFromDomain(p)
	if got.Insight != nil || got.InsightAt != "" {
		t.Fatalf("bad json should omit the card, got %+v %q", got.Insight, got.InsightAt)
	}

	p.Insight = nil
	p.InsightAt = &at
	got = WardrobeProductFromDomain(p)
	if got.Insight != nil || got.InsightAt != "" {
		t.Fatal("nil insight should omit timestamp")
	}
}
