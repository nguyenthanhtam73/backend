package dto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Cabinet card copy is plain Vietnamese. The frontend /cabinet card reads these
// keys as-is: what_it_does, fit.verdict (yes|maybe|no), fit.reason,
// buy.advice (nên mua|chưa nên), buy.why, actives[].name, actives[].gloss,
// disclaimer.
//
// buy.advice stays the machine tokens "nên mua" and "chưa nên". The live
// cabinet UI maps "nên mua" → "Nên dùng tiếp" and "chưa nên" → "Chưa nên dùng tiếp"
// (frontend ownedInsightUse). Free-text fields explain that keep-using
// decision and must not contain the word "mua".

const (
	// WardrobeInsightDisclaimer is always set by the server. The model cannot replace it.
	WardrobeInsightDisclaimer = "không thay bác sĩ da liễu"

	WardrobeFitYes   = "yes"
	WardrobeFitMaybe = "maybe"
	WardrobeFitNo    = "no"

	WardrobeBuyYes = "nên mua"
	WardrobeBuyNo  = "chưa nên"
)

const (
	insightUnknownFitReason = "Chưa có loại da hoặc check-in gần đây để so."
	insightUnknownBuyWhy    = "Chưa đủ thông tin da để biết có nên dùng tiếp."
	insightFallbackReason   = "Chưa đủ chi tiết để chắc hơn."
	insightFallbackBuyYes   = "Hướng hợp với da đang có."
	insightFallbackBuyNo    = "Nên chờ thêm trước khi dùng tiếp."
	insightFallbackWhat     = "Sản phẩm đang có trong tủ đồ."

	maxInsightJSON      = 8192
	maxWhatItDoesRunes  = 160
	maxFitReasonRunes   = 220
	maxBuyWhyRunes      = 180
	maxActiveNameRunes  = 48
	maxActiveGlossRunes = 90
	maxWardrobeActives  = 5
)

// WardrobeProductInsight is one cabinet product card.
type WardrobeProductInsight struct {
	WhatItDoes string                  `json:"what_it_does"`
	Fit        WardrobeProductFit      `json:"fit"`
	Buy        WardrobeProductBuy      `json:"buy"`
	Actives    []WardrobeProductActive `json:"actives,omitempty"`
	Disclaimer string                  `json:"disclaimer"`
}

// WardrobeProductFit is whether this product suits the user's skin.
type WardrobeProductFit struct {
	Verdict string `json:"verdict"` // yes | maybe | no
	Reason  string `json:"reason"`
}

// WardrobeProductBuy is the keep-using line on an owned-product card.
// Advice is still the machine token "nên mua" | "chưa nên" so existing clients
// can map it. Why is the plain-language sentence under that label.
type WardrobeProductBuy struct {
	Advice string `json:"advice"` // nên mua | chưa nên
	Why    string `json:"why"`
}

// WardrobeProductActive is one ingredient plus a plain-language gloss.
type WardrobeProductActive struct {
	Name  string `json:"name"`
	Gloss string `json:"gloss"`
}

type wardrobeInsightPayload struct {
	WhatItDoes string `json:"what_it_does"`
	Fit        struct {
		Verdict string `json:"verdict"`
		Reason  string `json:"reason"`
	} `json:"fit"`
	Buy struct {
		Advice string `json:"advice"`
		Why    string `json:"why"`
	} `json:"buy"`
	Actives []struct {
		Name  string `json:"name"`
		Gloss string `json:"gloss"`
	} `json:"actives"`
	Disclaimer string `json:"disclaimer"`
}

// ParseWardrobeProductInsight turns model JSON into the cabinet card.
// skinKnown is false when the profile has no skin type / notes / concerns and
// there are no recent check-ins — the card then stays "maybe" / "chưa nên"
// instead of inventing a fit.
func ParseWardrobeProductInsight(raw []byte, skinKnown bool) (WardrobeProductInsight, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return WardrobeProductInsight{}, fmt.Errorf("empty insight json")
	}
	if len(raw) > maxInsightJSON {
		return WardrobeProductInsight{}, fmt.Errorf("insight json too large")
	}
	var payload wardrobeInsightPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return WardrobeProductInsight{}, fmt.Errorf("parse insight json: %w", err)
	}
	out, ok := normalizeWardrobeProductInsight(payload, skinKnown)
	if !ok {
		return WardrobeProductInsight{}, fmt.Errorf("insight missing what_it_does")
	}
	return out, nil
}

// MapStoredWardrobeInsight reads a card already saved on the product.
// Invalid JSON is omitted so a bad row does not break GET /wardrobe.
func MapStoredWardrobeInsight(raw json.RawMessage) *WardrobeProductInsight {
	// Already normalized at write time. skinKnown true keeps the stored verdict.
	out, err := ParseWardrobeProductInsight(raw, true)
	if err != nil {
		return nil
	}
	return &out
}

func normalizeWardrobeProductInsight(payload wardrobeInsightPayload, skinKnown bool) (WardrobeProductInsight, bool) {
	what := clipInsight(stripDisclaimer(payload.WhatItDoes), maxWhatItDoesRunes)
	if what == "" {
		return WardrobeProductInsight{}, false
	}
	verdict := normalizeFitVerdict(payload.Fit.Verdict)
	if verdict == "" {
		verdict = WardrobeFitMaybe
	}
	advice := normalizeBuyAdvice(payload.Buy.Advice)
	reason := clipInsight(stripDisclaimer(payload.Fit.Reason), maxFitReasonRunes)
	why := clipInsight(stripDisclaimer(payload.Buy.Why), maxBuyWhyRunes)

	if !skinKnown {
		verdict = WardrobeFitMaybe
		advice = WardrobeBuyNo
		reason = insightUnknownFitReason
		why = insightUnknownBuyWhy
	}
	if verdict == WardrobeFitNo {
		advice = WardrobeBuyNo
	}
	if advice == "" {
		if verdict == WardrobeFitYes {
			advice = WardrobeBuyYes
		} else {
			advice = WardrobeBuyNo
		}
	}
	if reason == "" {
		reason = insightFallbackReason
	}
	if why == "" {
		why = insightWhyFallback(advice)
	}

	keepUsing := advice == WardrobeBuyYes
	what = polishOwnedLine(what, insightFallbackWhat, true, maxWhatItDoesRunes)
	reason = polishOwnedLine(reason, insightFallbackReason, keepUsing, maxFitReasonRunes)
	why = polishOwnedLine(why, insightWhyFallback(advice), keepUsing, maxBuyWhyRunes)
	if what == "" {
		return WardrobeProductInsight{}, false
	}

	return WardrobeProductInsight{
		WhatItDoes: what,
		Fit: WardrobeProductFit{
			Verdict: verdict,
			Reason:  reason,
		},
		Buy: WardrobeProductBuy{
			Advice: advice,
			Why:    why,
		},
		Actives:    normalizeActives(payload.Actives),
		Disclaimer: WardrobeInsightDisclaimer,
	}, true
}

func normalizeActives(in []struct {
	Name  string `json:"name"`
	Gloss string `json:"gloss"`
}) []WardrobeProductActive {
	if len(in) == 0 {
		return nil
	}
	out := make([]WardrobeProductActive, 0, len(in))
	seen := map[string]struct{}{}
	for _, a := range in {
		name := clipInsight(a.Name, maxActiveNameRunes)
		gloss := polishOwnedLine(clipInsight(stripDisclaimer(a.Gloss), maxActiveGlossRunes), "", true, maxActiveGlossRunes)
		if name == "" || gloss == "" || copyContainsMua(name) || copyContainsMua(gloss) {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, WardrobeProductActive{Name: name, Gloss: gloss})
		if len(out) == maxWardrobeActives {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func insightWhyFallback(advice string) string {
	if advice == WardrobeBuyYes {
		return insightFallbackBuyYes
	}
	return insightFallbackBuyNo
}

// muaToken matches the shopping word "mua" in any ASCII case.
// "mùa" and "mụn" do not match: the vowel is different.
var muaToken = regexp.MustCompile(`(?i)mua`)

// leadingKeepUsing matches a sentence that opens by telling the user to continue.
var leadingKeepUsing = regexp.MustCompile(`(?i)^\s*nên dùng tiếp\b`)

func copyContainsMua(s string) bool {
	return muaToken.MatchString(s)
}

// rewriteMuaAsDungTiep replaces the word "mua" with "dùng tiếp".
// "Nên mua vì ..." becomes "Nên dùng tiếp vì ...".
func rewriteMuaAsDungTiep(s string) string {
	if !copyContainsMua(s) {
		return s
	}
	s = muaToken.ReplaceAllStringFunc(s, func(m string) string {
		if m != "" && m[0] >= 'A' && m[0] <= 'Z' {
			return "Dùng tiếp"
		}
		return "dùng tiếp"
	})
	return strings.Join(strings.Fields(s), " ")
}

// polishOwnedLine removes shopping language from cabinet free text.
// keepUsing false means the card is "chưa nên" (pause). A sentence that still
// opens with "nên dùng tiếp" is replaced by fallback so it does not contradict
// the pause label.
func polishOwnedLine(s, fallback string, keepUsing bool, max int) string {
	s = clipInsight(s, max)
	if s == "" || !copyContainsMua(s) {
		return s
	}
	rewritten := clipInsight(rewriteMuaAsDungTiep(s), max)
	if copyContainsMua(rewritten) || (!keepUsing && leadingKeepUsing.MatchString(rewritten)) {
		if copyContainsMua(fallback) {
			fallback = insightFallbackBuyNo
		}
		return clipInsight(fallback, max)
	}
	return rewritten
}

// WardrobeInsightMuaHit is one stored free-text field that still says "mua".
// buy.advice is not a hit: that token is the cabinet UI's keep-using switch.
type WardrobeInsightMuaHit struct {
	Field string
	Text  string
}

// WardrobeInsightMuaHits lists human-readable cabinet-card strings that contain
// "mua" (case-insensitive). The machine token buy.advice ("nên mua" / "chưa nên")
// is ignored, so a card whose only "mua" is that token is not selected.
// Invalid JSON returns no hits — those rows are left untouched.
func WardrobeInsightMuaHits(raw json.RawMessage) []WardrobeInsightMuaHit {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var payload wardrobeInsightPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	var hits []WardrobeInsightMuaHit
	add := func(field, text string) {
		if copyContainsMua(text) {
			hits = append(hits, WardrobeInsightMuaHit{Field: field, Text: oneInsightLine(text)})
		}
	}
	add("what_it_does", payload.WhatItDoes)
	add("fit.reason", payload.Fit.Reason)
	add("buy.why", payload.Buy.Why)
	for i, a := range payload.Actives {
		add(fmt.Sprintf("actives[%d].name", i), a.Name)
		add(fmt.Sprintf("actives[%d].gloss", i), a.Gloss)
	}
	return hits
}

func normalizeFitVerdict(raw string) string {
	s := strings.Trim(strings.ToLower(oneInsightLine(raw)), " .!…")
	switch s {
	case "yes", "y", "phù hợp", "phu hop", "hợp", "hop", "có", "co":
		return WardrobeFitYes
	case "no", "n", "không", "khong", "không hợp", "khong hop":
		return WardrobeFitNo
	case "maybe", "có thể", "co the", "chưa chắc", "chua chac":
		return WardrobeFitMaybe
	default:
		return ""
	}
}

func normalizeBuyAdvice(raw string) string {
	s := strings.Trim(strings.ToLower(oneInsightLine(raw)), " .!…")
	switch s {
	case "nên mua", "nen mua", "should_buy", "should buy", "buy", "yes":
		return WardrobeBuyYes
	case "nên dùng tiếp", "nen dung tiep", "dùng tiếp", "dung tiep", "keep using":
		// Model wrote the display phrase. Store the token the cabinet UI maps.
		return WardrobeBuyYes
	case "chưa nên", "chua nen", "chưa nên dùng tiếp", "chua nen dung tiep", "not_yet", "wait", "skip", "no":
		return WardrobeBuyNo
	}
	if strings.Contains(s, "chưa nên") || strings.Contains(s, "chua nen") {
		return WardrobeBuyNo
	}
	if strings.Contains(s, "nên mua") || strings.Contains(s, "nen mua") {
		return WardrobeBuyYes
	}
	return ""
}

func stripDisclaimer(s string) string {
	s = strings.ReplaceAll(s, WardrobeInsightDisclaimer, "")
	return s
}

func clipInsight(s string, max int) string {
	s = oneInsightLine(s)
	if s == "" || max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max]))
}

func oneInsightLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
