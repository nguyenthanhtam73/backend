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
	GuardComparison    bool
	PreviousComparable bool
	PhotoLimited       bool
	WorseThanLast      bool
	BalanceVoice       bool
}

func webQAForCheck(check *domain.SkinCheck, facts CheckInCoachFacts, visionStatus, visionRaw string, out *CoachStructuredOutput) webQA {
	note := ""
	cam := "front"
	if check != nil {
		note = strings.TrimSpace(check.UserNote)
		cam = ResolveCamera(dto.DecodePhotoContext(check.PhotoContext))
	}
	ev := ClassifyCheckInPhotoEvidence(visionStatus, visionRaw)
	limited := ev.Kind != PhotoEvidenceOK
	todayZones := zonesInOutput(out)
	if cam == "front" {
		for i, zone := range todayZones {
			todayZones[i] = flipZoneKey(zone)
		}
	}
	prevOK := previousComparable(facts, todayZones)
	return webQA{
		NoteKnown:          true,
		HasUserNote:        note != "",
		Camera:             cam,
		GuardComparison:    true,
		PreviousComparable: prevOK,
		PhotoLimited:       limited,
		WorseThanLast:      skinTrendWorse(facts, scoreOf(out)) && prevOK && !limited,
		BalanceVoice:       true,
	}
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
		if qa.Camera == "front" {
			s = FlipFrameCheeks(s)
		}
		if qa.GuardComparison && !qa.PreviousComparable {
			s = stripInvalidComparison(s)
		}
		s = replaceUncleanJudgement(s)
		s = plainLoanwords(s)
		s = moveHedgeToClauseEnd(s)
		*f.text = s
	}
	if qa.BalanceVoice {
		balanceMay(fields)
	}
	dedupeReassurance(fields, reassuranceAllowed(qa))
}

func reassuranceAllowed(qa webQA) bool {
	return qa.WorseThanLast && qa.PreviousComparable && !qa.PhotoLimited
}

// stripInventedUserNote removes a sentence that claims the user wrote a note.
func stripInventedUserNote(s string) string {
	parts := splitSentences(s)
	kept := make([]string, 0, len(parts))
	for _, sentence := range parts {
		low := strings.ToLower(sentence)
		if strings.Contains(low, "ghi chú") || strings.Contains(low, "mày ghi") || strings.Contains(low, "bạn ghi") {
			continue
		}
		kept = append(kept, sentence)
	}
	return strings.TrimSpace(strings.Join(kept, ""))
}

// stripInvalidComparison drops "so với lần trước" style claims.
func stripInvalidComparison(s string) string {
	for _, phrase := range []string{
		" so với lần trước",
		" hơn lần trước",
		" kém lần trước",
	} {
		s = replaceFold(s, phrase, "")
	}
	return squeezeSpaces(s)
}

func replaceUncleanJudgement(s string) string {
	return replaceFold(s, "làm da trông không sạch", "làm nốt dễ thấy hơn")
}

func plainLoanwords(s string) string {
	for _, pair := range []struct{ from, to string }{
		{"makeup", "trang điểm"},
		{"toner", "nước cân bằng da"},
		{"serum", "tinh chất"},
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

func balanceMay(fields []polishField) {
	if countPronounMay(fields) == 0 && !screenHasTao(fields) {
		for _, f := range fields {
			if f.text == nil || !f.opening {
				continue
			}
			cur := strings.TrimSpace(*f.text)
			if cur == "" || strings.HasPrefix(cur, "Tao ") || strings.HasPrefix(cur, "Tao,") {
				return
			}
			*f.text = "Tao thấy " + cur
			return
		}
	}
	for countPronounMay(fields) > 2 {
		if !softenOneDaMay(fields) {
			return
		}
	}
}

func screenHasTao(fields []polishField) bool {
	for _, f := range fields {
		if f.text == nil {
			continue
		}
		parts := splitVoiceChunks(*f.text)
		for _, part := range parts {
			if part.word && strings.EqualFold(part.text, "tao") {
				return true
			}
		}
	}
	return false
}

func countPronounMay(fields []polishField) int {
	n := 0
	for _, f := range fields {
		if f.text == nil {
			continue
		}
		parts := splitVoiceChunks(*f.text)
		next, _ := voiceNextWords(parts)
		prev := ""
		for i, part := range parts {
			if !part.word {
				continue
			}
			key := strings.ToLower(part.text)
			if key == "mày" && !keepEyebrowMay(key, prev, next[i]) {
				n++
			}
			prev = key
		}
	}
	return n
}

func softenOneDaMay(fields []polishField) bool {
	for i := len(fields) - 1; i >= 0; i-- {
		f := fields[i]
		if f.text == nil {
			continue
		}
		if next, ok := replaceLastDaMay(*f.text); ok {
			*f.text = next
			return true
		}
	}
	return false
}

func replaceLastDaMay(s string) (string, bool) {
	low := strings.ToLower(s)
	for _, from := range []string{"da của mày", "da mày"} {
		i := strings.LastIndex(low, from)
		if i < 0 {
			continue
		}
		repl := "da"
		if i < len(s) && s[i] == 'D' {
			repl = "Da"
		}
		return s[:i] + repl + s[i+len(from):], true
	}
	return s, false
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
