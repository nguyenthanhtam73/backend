package ai

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
)

// webQA is what the web voice guard may use from the request.
// A zero value does not strip notes, flip cheeks, or remove comparisons.
type webQA struct {
	NoteKnown          bool
	HasUserNote        bool
	Camera             string
	SwapCheeks         bool
	KeepZone           string
	GuardComparison    bool
	PreviousComparable bool
	PhotoLimited       bool
	WorseThanLast      bool
}

func webQAForCheck(check *domain.SkinCheck, facts CheckInCoachFacts, visionStatus, visionRaw string, out *CoachStructuredOutput) webQA {
	note := ""
	doc := dto.StoredPhotoContext{}
	if check != nil {
		note = strings.TrimSpace(check.UserNote)
		doc = dto.DecodePhotoContext(check.PhotoContext)
	}
	ev := ClassifyCheckInPhotoEvidence(visionStatus, visionRaw)
	limited := ev.Kind != PhotoEvidenceOK
	swap := ShouldSwapCheeks(doc)
	keep := ""
	if !swap {
		keep = UserPickedCloseupZone(doc)
	}
	todayZones := userSideZones(zonesInOutput(out), swap, keep)
	prevOK := previousComparable(facts, todayZones)
	return webQA{
		NoteKnown:          true,
		HasUserNote:        note != "",
		Camera:             ResolveCamera(doc),
		SwapCheeks:         swap,
		KeepZone:           keep,
		GuardComparison:    true,
		PreviousComparable: prevOK,
		PhotoLimited:       limited,
		WorseThanLast:      skinTrendWorse(facts, scoreOf(out)) && prevOK && !limited,
	}
}

func userSideZones(zones []string, swap bool, keep string) []string {
	if !swap {
		return zones
	}
	out := make([]string, len(zones))
	for i, zone := range zones {
		if keep != "" && strings.EqualFold(zone, keep) {
			out[i] = zone
			continue
		}
		out[i] = flipZoneKey(zone)
	}
	return out
}

func flipZoneKey(zone string) string {
	switch strings.ToLower(strings.TrimSpace(zone)) {
	case "left_cheek":
		return "right_cheek"
	case "right_cheek":
		return "left_cheek"
	default:
		return zone
	}
}

func scoreOf(out *CoachStructuredOutput) float64 {
	if out == nil {
		return 0
	}
	return out.Score
}

func zonesInOutput(out *CoachStructuredOutput) []string {
	if out == nil || len(out.ZoneNotesRaw) == 0 {
		return nil
	}
	var items []map[string]any
	if json.Unmarshal(out.ZoneNotesRaw, &items) != nil {
		return nil
	}
	var zones []string
	for _, item := range items {
		zone, _ := item["zone"].(string)
		zone = strings.TrimSpace(zone)
		if zone != "" {
			zones = append(zones, zone)
		}
	}
	return zones
}

func previousComparable(facts CheckInCoachFacts, todayZones []string) bool {
	if len(facts.History) == 0 {
		return false
	}
	prev := facts.History[0]
	if prev.PhotoEvidence != PhotoEvidenceOK {
		return false
	}
	if len(prev.Zones) == 0 || len(todayZones) == 0 {
		return false
	}
	have := map[string]struct{}{}
	for _, z := range prev.Zones {
		have[strings.ToLower(z)] = struct{}{}
	}
	for _, z := range todayZones {
		if _, ok := have[strings.ToLower(z)]; ok {
			return true
		}
	}
	return false
}

func applyWebQA(fields []polishField, qa webQA) {
	for _, f := range fields {
		if f.text == nil {
			continue
		}
		s := *f.text
		if qa.NoteKnown && !qa.HasUserNote {
			s = stripInventedUserNote(s)
		}
		if qa.GuardComparison && !qa.PreviousComparable {
			s = stripInvalidComparison(s)
		}
		if qa.SwapCheeks && !f.skipCheek {
			s = FlipFrameCheeks(s)
		}
		s = replaceUncleanJudgement(s)
		s = plainLoanwords(s)
		s = moveHedgeToClauseEnd(s)
		*f.text = s
	}
	dedupeReassurance(fields, reassuranceAllowed(qa))
}

func reassuranceAllowed(qa webQA) bool {
	return qa.WorseThanLast && qa.PreviousComparable && !qa.PhotoLimited
}

// stripInventedUserNote drops only a claim that the user already wrote a note.
// Advice such as "Nhớ ghi chú lại…" stays.
func stripInventedUserNote(s string) string {
	parts := splitSentences(s)
	kept := make([]string, 0, len(parts))
	for _, sentence := range parts {
		if inventedNoteClaim(strings.ToLower(sentence)) {
			continue
		}
		kept = append(kept, sentence)
	}
	return strings.TrimSpace(strings.Join(kept, ""))
}

func inventedNoteClaim(low string) bool {
	if strings.Contains(low, "theo ghi chú") {
		return true
	}
	for _, who := range []string{"mày", "bạn"} {
		if strings.Contains(low, who+" có ghi là") || strings.Contains(low, who+" có ghi rằng") {
			return true
		}
		if strings.Contains(low, who+" ghi chú là") || strings.Contains(low, who+" ghi chú rằng") {
			return true
		}
	}
	return false
}

// stripInvalidComparison removes a whole sentence that compares the skin with
// last time: "so với lần trước", "hơn/kém lần trước", "đỡ … hơn lần trước".
// Advice such as "Lần sau chụp giống lần trước…" stays.
func stripInvalidComparison(s string) string {
	parts := splitSentences(s)
	kept := make([]string, 0, len(parts))
	for _, sentence := range parts {
		if skinComparisonSentence(strings.ToLower(sentence)) {
			continue
		}
		kept = append(kept, sentence)
	}
	return strings.TrimSpace(strings.Join(kept, ""))
}

func skinComparisonSentence(low string) bool {
	if strings.Contains(low, "so với lần trước") {
		return true
	}
	if strings.Contains(low, "hơn lần trước") || strings.Contains(low, "kém lần trước") {
		return true
	}
	return false
}

// replaceUncleanJudgement replaces a whole sentence that says the skin looks
// unclean. Splicing "dễ bóng dầu" after an oil subject repeats the word
// ("Dầu dễ bóng dầu"), so the sentence becomes "Da dễ bóng dầu."
func replaceUncleanJudgement(s string) string {
	parts := splitSentences(s)
	changed := false
	for i, sentence := range parts {
		if !strings.Contains(strings.ToLower(sentence), "làm da trông không sạch") {
			continue
		}
		punct := ""
		trimmed := strings.TrimRight(sentence, " \t")
		if trimmed != "" {
			last := trimmed[len(trimmed)-1]
			if last == '.' || last == '!' || last == '?' {
				punct = string(last)
			}
		}
		parts[i] = "Da dễ bóng dầu" + punct
		changed = true
	}
	if !changed {
		return s
	}
	return strings.Join(parts, "")
}

func plainLoanwords(s string) string {
	for _, pair := range []struct{ from, to string }{
		{"makeup", "trang điểm"},
		{"toner", "nước cân bằng da"},
		{"serum", "tinh chất"},
		{"stressed", "căng thẳng"},
		{"stress", "căng thẳng"},
		{"filter", "bộ lọc màu"},
	} {
		s = replacePhraseSkipCapital(s, pair.from, pair.to)
	}
	return replaceFlash(s)
}

func replaceFlash(s string) string {
	var b strings.Builder
	for {
		idx, size := findPhrase(s, "flash")
		if idx < 0 {
			b.WriteString(s)
			return b.String()
		}
		r, _ := utf8.DecodeRuneInString(s[idx:])
		prefix := strings.ToLower(s[:idx])
		if unicode.IsUpper(r) || strings.HasSuffix(strings.TrimRight(prefix, " "), "đèn") {
			b.WriteString(s[:idx+size])
			s = s[idx+size:]
			continue
		}
		b.WriteString(s[:idx])
		b.WriteString("đèn flash")
		s = s[idx+size:]
	}
}

// moveHedgeToClauseEnd turns "có vài nốt nhỏ, chưa chắc ở trán"
// into "có vài nốt nhỏ ở trán, chưa chắc".
func moveHedgeToClauseEnd(s string) string {
	const mark = ", chưa chắc ở "
	low := strings.ToLower(s)
	idx := strings.Index(low, mark)
	if idx < 0 {
		return s
	}
	placeStart := idx + len(mark)
	placeEnd := placeStart
	for placeEnd < len(s) {
		r, size := utf8.DecodeRuneInString(s[placeEnd:])
		if r == ',' || r == '.' || r == '!' || r == '?' || r == ';' {
			break
		}
		placeEnd += size
	}
	place := strings.TrimSpace(s[placeStart:placeEnd])
	if place == "" {
		return s
	}
	return s[:idx] + " ở " + place + ", chưa chắc" + s[placeEnd:]
}

func dedupeReassurance(fields []polishField, allow bool) {
	const phrase = "da có hôm lên hôm xuống"
	seen := false
	for _, f := range fields {
		if f.text == nil {
			continue
		}
		parts := splitSentences(*f.text)
		kept := make([]string, 0, len(parts))
		for _, sentence := range parts {
			if !strings.Contains(strings.ToLower(sentence), phrase) {
				kept = append(kept, sentence)
				continue
			}
			if !allow || seen {
				continue
			}
			seen = true
			kept = append(kept, sentence)
		}
		*f.text = strings.TrimSpace(strings.Join(kept, ""))
	}
	if allow && !seen {
		for _, f := range fields {
			if f.text == nil || !f.opening {
				continue
			}
			cur := strings.TrimSpace(*f.text)
			line := "Không sao đâu, da có hôm lên hôm xuống."
			if cur == "" {
				*f.text = line
			} else {
				*f.text = line + " " + cur
			}
			return
		}
	}
}
