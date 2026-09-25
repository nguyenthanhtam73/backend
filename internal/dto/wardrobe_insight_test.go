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

func TestParseWardrobeProductInsight_OwnedReasonDoesNotSayMua(t *testing.T) {
	// Live cabinet example: the model explained a buy even though the user
	// already owns the cleanser. The stored/API sentence keeps the skin reason
	// and drops the shopping word. buy.advice stays the token the UI maps.
	const before = "Nên mua vì phù hợp với loại da và mục tiêu làm sạch mụn."
	const after = "Nên dùng tiếp vì phù hợp với loại da và mục tiêu làm sạch mụn."
	raw := []byte(`{
		"what_it_does": "Sữa rửa mặt tạo bọt, làm sạch dầu thừa.",
		"fit": {"verdict": "yes", "reason": "Da dầu và mục tiêu làm sạch mụn, check-in không thấy rát."},
		"buy": {"advice": "nên mua", "why": "` + before + `"},
		"actives": [{"name": "Ceramide", "gloss": "nên mua khi da khô"}]
	}`)
	if hits := WardrobeInsightMuaHits(raw); len(hits) != 2 {
		t.Fatalf("stored hits: %+v", hits)
	}
	got, err := ParseWardrobeProductInsight(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Buy.Advice != WardrobeBuyYes {
		t.Fatalf("advice token: %q", got.Buy.Advice)
	}
	if got.Buy.Why != after {
		t.Fatalf("why: %q", got.Buy.Why)
	}
	if strings.Contains(strings.ToLower(got.Fit.Reason), "mua") || strings.Contains(strings.ToLower(got.WhatItDoes), "mua") {
		t.Fatalf("free text still says mua: %+v", got)
	}
	if len(got.Actives) != 1 || got.Actives[0].Gloss != "nên dùng tiếp khi da khô" {
		t.Fatalf("gloss: %+v", got.Actives)
	}
	for _, s := range []string{got.WhatItDoes, got.Fit.Reason, got.Buy.Why, got.Actives[0].Gloss} {
		if copyContainsMua(s) {
			t.Fatalf("visible copy contains mua: %q", s)
		}
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if hits := WardrobeInsightMuaHits(encoded); len(hits) != 0 {
		t.Fatalf("rewritten card still flagged: %+v", hits)
	}
	again, err := ParseWardrobeProductInsight(encoded, true)
	if err != nil {
		t.Fatal(err)
	}
	if again.Buy.Why != got.Buy.Why || again.Buy.Advice != WardrobeBuyYes {
		t.Fatalf("second pass changed the card: %+v", again)
	}
}

func TestParseWardrobeProductInsight_PauseDoesNotBecomeKeepUsing(t *testing.T) {
	raw := []byte(`{
		"what_it_does": "Toner cân bằng.",
		"fit": {"verdict": "no", "reason": "Nên mua vì giá ổn."},
		"buy": {"advice": "nên mua", "why": "Nên mua vì giá ổn."}
	}`)
	got, err := ParseWardrobeProductInsight(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Buy.Advice != WardrobeBuyNo || got.Fit.Verdict != WardrobeFitNo {
		t.Fatalf("card: %+v", got)
	}
	if got.Buy.Why != insightFallbackBuyNo || got.Fit.Reason != insightFallbackReason {
		t.Fatalf("pause copy: reason %q why %q", got.Fit.Reason, got.Buy.Why)
	}
	if copyContainsMua(got.Buy.Why) || copyContainsMua(got.Fit.Reason) {
		t.Fatalf("pause copy still says mua: %+v", got)
	}
}

func TestParseWardrobeProductInsight_MuaRewriteKeepsMuaSeasonWord(t *testing.T) {
	raw := []byte(`{
		"what_it_does": "Kem dưỡng cho mùa hanh.",
		"fit": {"verdict": "yes", "reason": "Da khô vào mùa hanh, hợp kem dưỡng."},
		"buy": {"advice": "nên mua", "why": "Nên dùng tiếp vào mùa lạnh."}
	}`)
	got, err := ParseWardrobeProductInsight(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.WhatItDoes != "Kem dưỡng cho mùa hanh." || got.Fit.Reason != "Da khô vào mùa hanh, hợp kem dưỡng." {
		t.Fatalf("season words changed: %+v", got)
	}
	if got.Buy.Why != "Nên dùng tiếp vào mùa lạnh." || got.Buy.Advice != WardrobeBuyYes {
		t.Fatalf("buy: %+v", got.Buy)
	}
	if hits := WardrobeInsightMuaHits(raw); len(hits) != 0 {
		t.Fatalf("mùa must not count as mua: %+v", hits)
	}
}

func TestParseWardrobeProductInsight_DisplayPhraseMapsToAdviceToken(t *testing.T) {
	raw := []byte(`{
		"what_it_does": "Kem chống nắng.",
		"fit": {"verdict": "yes", "reason": "Da đang ổn."},
		"buy": {"advice": "nên dùng tiếp", "why": "Hợp với da dầu."}
	}`)
	got, err := ParseWardrobeProductInsight(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Buy.Advice != WardrobeBuyYes {
		t.Fatalf("keep phrase must store the UI token, got %q", got.Buy.Advice)
	}

	raw = []byte(`{
		"what_it_does": "Kem chống nắng.",
		"fit": {"verdict": "yes", "reason": "Da đang ổn."},
		"buy": {"advice": "Chưa nên dùng tiếp", "why": "Da đang rát."}
	}`)
	got, err = ParseWardrobeProductInsight(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Buy.Advice != WardrobeBuyNo {
		t.Fatalf("pause phrase must stay pause even when fit is yes, got %q", got.Buy.Advice)
	}
}

func TestWardrobeInsightMuaHits_IgnoresAdviceToken(t *testing.T) {
	clean := []byte(`{
		"what_it_does": "Sữa rửa mặt.",
		"fit": {"verdict": "yes", "reason": "Da dầu."},
		"buy": {"advice": "nên mua", "why": "Hợp với da dầu."}
	}`)
	if hits := WardrobeInsightMuaHits(clean); len(hits) != 0 {
		t.Fatalf("advice token alone must not match: %+v", hits)
	}
	dirty := []byte(`{
		"what_it_does": "Sữa rửa mặt.",
		"fit": {"verdict": "yes", "reason": "Da dầu."},
		"buy": {"advice": "nên mua", "why": "Nên MUA vì hợp da."},
		"disclaimer": "không khuyên mua"
	}`)
	hits := WardrobeInsightMuaHits(dirty)
	if len(hits) != 1 || hits[0].Field != "buy.why" || !strings.Contains(strings.ToLower(hits[0].Text), "mua") {
		t.Fatalf("hits: %+v", hits)
	}
	if hits := WardrobeInsightMuaHits([]byte(`{not json`)); hits != nil {
		t.Fatalf("bad json: %+v", hits)
	}
}

func TestServerInsightCopyDoesNotSayMua(t *testing.T) {
	for _, s := range []string{
		insightUnknownFitReason,
		insightUnknownBuyWhy,
		insightFallbackReason,
		insightFallbackBuyYes,
		insightFallbackBuyNo,
		insightFallbackWhat,
		WardrobeInsightDisclaimer,
	} {
		if copyContainsMua(s) {
			t.Fatalf("server copy contains mua: %q", s)
		}
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
