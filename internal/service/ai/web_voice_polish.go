package ai

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/dto"
	"golang.org/x/text/unicode/norm"
)

func init() {
	dto.PolishWebCoachDetail = polishWebCoachDetail
}

// polishWebCoachDetail is the read path for a web screen. Android checks skip it.
func polishWebCoachDetail(d *dto.SkinCoachDetail) {
	if d == nil {
		return
	}
	fields := detailFields(d)
	polishFields(fields, webQA{})
	d.ClarifyQuestions = capFollowUpQuestions(d.ClarifyQuestions)
}

// polishWebCoachScreen rewrites one generated web coach result. Android does not call it.
func polishWebCoachScreen(out *CoachStructuredOutput) bool {
	if out == nil {
		return false
	}
	return sanitizeWebCoachOutput(out, webQA{})
}

func sanitizeWebCoachOutput(out *CoachStructuredOutput, qa webQA) bool {
	if out == nil {
		return false
	}
	before := coachSnapshot(out)
	fields := coachFields(out)
	zones := decodePolishZones(out.ZoneNotesRaw)
	for i := range zones {
		skip := qa.KeepZone != "" && strings.EqualFold(zones[i].zone, qa.KeepZone)
		fields = append(fields, polishField{text: &zones[i].note, zoneNote: true, skipCheek: skip})
	}
	scores := decodePolishScores(out.SkinScoreNotesRaw)
	for i := range scores {
		fields = append(fields, polishField{text: &scores[i].val})
	}
	polishFields(fields, qa)
	out.ZoneNotesRaw = encodePolishZones(out.ZoneNotesRaw, zones, qa)
	out.SkinScoreNotesRaw = encodePolishScores(out.SkinScoreNotesRaw, scores)
	return coachSnapshot(out) != before
}

type polishZone struct {
	zone string
	note string
}

type polishScore struct {
	key string
	val string
}

func decodePolishZones(raw json.RawMessage) []polishZone {
	var items []map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &items) != nil {
		return nil
	}
	out := make([]polishZone, len(items))
	for i, item := range items {
		out[i].zone, _ = item["zone"].(string)
		out[i].note, _ = item["note"].(string)
	}
	return out
}

func encodePolishZones(raw json.RawMessage, zones []polishZone, qa webQA) json.RawMessage {
	if len(zones) == 0 {
		return raw
	}
	var items []map[string]any
	if json.Unmarshal(raw, &items) != nil || len(items) != len(zones) {
		return raw
	}
	changed := false
	for i := range items {
		prevNote, _ := items[i]["note"].(string)
		prevZone, _ := items[i]["zone"].(string)
		zone := zones[i].zone
		if qa.SwapCheeks && !strings.EqualFold(zone, qa.KeepZone) {
			zone = flipZoneKey(zone)
		}
		if prevNote != zones[i].note {
			items[i]["note"] = zones[i].note
			changed = true
		}
		if zone != prevZone {
			items[i]["zone"] = zone
			changed = true
		}
	}
	if !changed {
		return raw
	}
	buf, err := json.Marshal(items)
	if err != nil {
		return raw
	}
	return buf
}

func decodePolishScores(raw json.RawMessage) []polishScore {
	var obj map[string]string
	if len(raw) == 0 || json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	out := make([]polishScore, 0, len(obj))
	for k, v := range obj {
		out = append(out, polishScore{key: k, val: v})
	}
	return out
}

func encodePolishScores(raw json.RawMessage, scores []polishScore) json.RawMessage {
	if len(scores) == 0 {
		return raw
	}
	var obj map[string]string
	if json.Unmarshal(raw, &obj) != nil {
		return raw
	}
	changed := false
	for _, s := range scores {
		if obj[s.key] != s.val {
			obj[s.key] = s.val
			changed = true
		}
	}
	if !changed {
		return raw
	}
	buf, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return buf
}

type polishField struct {
	text      *string
	keepMay   bool
	opening   bool
	main      bool
	zoneNote  bool
	skipCheek bool
}

// neutralMainSentence stands in when the only sentence of the main paragraph
// was removed. An empty situation analysis is dropped at save time.
const neutralMainSentence = "Da hôm nay trông vậy."

func polishFields(fields []polishField, qa webQA) {
	original := make([]string, len(fields))
	for i, f := range fields {
		if f.text != nil {
			original[i] = *f.text
		}
	}
	for _, f := range fields {
		if f.text == nil {
			continue
		}
		*f.text = polishOneSentence(*f.text)
		if f.zoneNote {
			*f.text = ToVietnameseSkinWords(*f.text)
		}
	}
	applyFirstGlosses(fields)
	applyWebQA(fields, qa)
	for i, f := range fields {
		if f.text == nil || !f.main {
			continue
		}
		if strings.TrimSpace(original[i]) != "" && strings.TrimSpace(*f.text) == "" {
			*f.text = neutralMainSentence
		}
	}
}

func polishOneSentence(s string) string {
	s = applyTypos(s)
	s = replaceChungTa(s)
	s = preferBenSide(s)
	s = softenSmoothSkinClaim(s)
	return s
}

func applyTypos(s string) string {
	for _, pair := range []struct{ from, to string }{
		{"mịn mặn", "mịn màng"},
	} {
		s = replaceFold(s, pair.from, pair.to)
	}
	return s
}

// ToVietnameseSkinWords maps common English skin words into Vietnamese.
// The leaked vision note "Trông giống red, swollen bump indicating mụn viêm"
// becomes "Trông giống nốt đỏ sưng mụn viêm".
func ToVietnameseSkinWords(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	phrases := []struct{ from, to string }{
		{"small red bump", "nốt đỏ nhỏ"},
		{"red, swollen bump indicating", "nốt đỏ sưng"},
		{"red swollen bump indicating", "nốt đỏ sưng"},
		{"red, swollen bump", "nốt đỏ sưng"},
		{"red swollen bump", "nốt đỏ sưng"},
		{"whitehead", "đầu trắng"},
		{"blackhead", "đầu đen"},
		{"papule", "nốt đỏ"},
		{"pustule", "nốt có mủ"},
		{"inflamed", "viêm"},
		{"redness", "da đỏ"},
		{"swollen", "sưng"},
		{"indicating", ""},
		{"bump", "nốt"},
		{"red", "đỏ"},
	}
	for _, p := range phrases {
		s = replacePhraseSkipCapital(s, p.from, p.to)
	}
	return squeezeSpaces(s)
}

func squeezeSpaces(s string) string {
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

func replaceChungTa(s string) string {
	s = replacePhraseKeepCase(s, "chúng mình", "tao với mày")
	s = replacePhraseKeepCase(s, "chúng ta", "tao với mày")
	return replaceTaSe(s)
}

func replaceTaSe(s string) string {
	parts := splitVoiceChunks(norm.NFC.String(s))
	var b strings.Builder
	prev := ""
	for _, part := range parts {
		if part.word && strings.ToLower(part.text) == "ta" && prev != "tao" && prev != "chúng" {
			// Peek is handled below by rewriting "ta sẽ" only.
			b.WriteString(part.text)
			prev = "ta"
			continue
		}
		b.WriteString(part.text)
		if part.word {
			prev = strings.ToLower(part.text)
		}
	}
	out := b.String()
	parts = splitVoiceChunks(norm.NFC.String(out))
	b.Reset()
	prev = ""
	for i, part := range parts {
		if part.word && strings.ToLower(part.text) == "ta" && prev != "tao" && prev != "chúng" && nextWordIs(parts, i, "sẽ") {
			b.WriteString(matchVoiceCase(part.text, "tao"))
			prev = "tao"
			continue
		}
		b.WriteString(part.text)
		if part.word {
			prev = strings.ToLower(part.text)
		}
	}
	return b.String()
}

func nextWordIs(parts []voiceChunk, i int, want string) bool {
	for j := i + 1; j < len(parts); j++ {
		if !parts[j].word {
			continue
		}
		return strings.ToLower(parts[j].text) == want
	}
	return false
}

func softenSmoothSkinClaim(s string) string {
	sentences := splitSentences(s)
	changed := false
	kept := make([]string, 0, len(sentences))
	for _, sentence := range sentences {
		low := strings.ToLower(sentence)
		smooth := strings.Contains(low, "mịn màng") || strings.Contains(low, "da mịn") || strings.Contains(low, "bề mặt mịn") || strings.Contains(low, "da đang mịn")
		if smooth && positiveBumpClaim(low) && !bumpNegated(low) && !describesVisibleBump(low) {
			changed = true
			continue
		}
		kept = append(kept, sentence)
	}
	if !changed {
		return s
	}
	return strings.TrimSpace(strings.Join(kept, ""))
}

func positiveBumpClaim(low string) bool {
	return strings.Contains(low, "mụn ẩn") || strings.Contains(low, "milia")
}

// describesVisibleBump reports a sentence that names a bump the photo shows.
// Those sentences stay even when the skin is also called smooth.
func describesVisibleBump(low string) bool {
	for _, word := range []string{"nốt", "hạt", "cục", "sần"} {
		if strings.Contains(low, word) {
			return true
		}
	}
	return false
}

func bumpNegated(low string) bool {
	for _, term := range []string{"milia", "mụn ẩn"} {
		idx := strings.Index(low, term)
		if idx < 0 {
			continue
		}
		prefix := low[:idx]
		for _, neg := range []string{"không thấy", "chưa thấy", "không phải", "không có", "không", "chưa", "chẳng"} {
			if strings.Contains(prefix, neg) {
				return true
			}
		}
	}
	return false
}

func applyFirstGlosses(fields []polishField) {
	used := map[string]bool{}
	for _, f := range fields {
		if f.text == nil {
			continue
		}
		*f.text = glossOnce(*f.text, used)
	}
}

func glossOnce(s string, used map[string]bool) string {
	type gloss struct {
		key   string
		terms []string
		say   string
	}
	items := []gloss{
		{"patch", []string{"patch-test", "patch test"}, "thử trước ở một vùng nhỏ"},
		{"milia", []string{"milia"}, "hạt nhỏ trắng dưới da"},
		{"acid", []string{"acid mạnh"}, "chất tẩy da chết mạnh"},
		{"retinoid", []string{"retinoid"}, "chất giúp da thay mới, dễ gây khô rát"},
	}
	for _, item := range items {
		if used[item.key] {
			continue
		}
		for _, term := range item.terms {
			idx, size := findPhrase(s, term)
			if idx < 0 {
				continue
			}
			used[item.key] = true
			if alreadyExplained(s, idx, size, item.say) {
				break
			}
			s = s[:idx+size] + " (" + item.say + ")" + s[idx+size:]
			break
		}
	}
	return s
}

func alreadyExplained(s string, idx, size int, say string) bool {
	if idx > 0 {
		r, _ := utf8.DecodeLastRuneInString(s[:idx])
		if r == '(' {
			return true
		}
	}
	rest := strings.TrimLeft(s[idx+size:], " ")
	if strings.HasPrefix(rest, "(") {
		return true
	}
	window := rest
	if len(window) > 80 {
		window = window[:80]
	}
	return strings.Contains(strings.ToLower(window), strings.ToLower(say))
}

func reassureOpening(fields []polishField) {
	const line = "Không sao đâu, da có hôm lên hôm xuống."
	for _, f := range fields {
		if f.text == nil || !f.opening {
			continue
		}
		cur := strings.TrimSpace(*f.text)
		if strings.HasPrefix(strings.ToLower(cur), "không sao") {
			return
		}
		if cur == "" {
			*f.text = line
			return
		}
		*f.text = line + " " + cur
		return
	}
}

func capFollowUpQuestions(qs []string) []string {
	if len(qs) == 0 {
		return qs
	}
	kept := 0
	out := make([]string, 0, len(qs))
	for _, q := range qs {
		if strings.Contains(q, "?") {
			if kept >= 1 {
				continue
			}
			kept++
		}
		out = append(out, q)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func coachFields(out *CoachStructuredOutput) []polishField {
	fields := []polishField{
		{text: &out.SituationAnalysis, keepMay: true, opening: true, main: true},
		{text: &out.SummaryNotes, keepMay: true},
		{text: &out.ConcernAlignment},
		{text: &out.MedicalDisclaimer},
	}
	fields = append(fields, stringListFields(out.Strengths)...)
	fields = append(fields, stringListFields(out.RoutineHints)...)
	fields = append(fields, stringListFields(out.AvoidOrPatch)...)
	fields = append(fields, stringListFields(out.SafetyReminders)...)
	for i := range out.Improvements {
		fields = append(fields, polishField{text: &out.Improvements[i].Tip}, polishField{text: &out.Improvements[i].Why})
	}
	for i := range out.CareSuggestions {
		fields = append(fields,
			polishField{text: &out.CareSuggestions[i].Step},
			polishField{text: &out.CareSuggestions[i].Why},
			polishField{text: &out.CareSuggestions[i].SafetyNote},
		)
	}
	for i := range out.ProductSuggestions {
		item := &out.ProductSuggestions[i]
		fields = append(fields,
			polishField{text: &item.Reason},
			polishField{text: &item.Step},
			polishField{text: &item.HowToUse},
			polishField{text: &item.Caution},
		)
		fields = append(fields, stringListFields(item.Benefits)...)
	}
	for i := range out.ProductGuidance {
		item := &out.ProductGuidance[i]
		fields = append(fields,
			polishField{text: &item.Why},
			polishField{text: &item.Step},
			polishField{text: &item.HowToUse},
			polishField{text: &item.Caution},
		)
		fields = append(fields, stringListFields(item.Benefits)...)
	}
	return fields
}

func coachTexts(out *CoachStructuredOutput) []string {
	var texts []string
	for _, f := range coachFields(out) {
		if f.text != nil {
			texts = append(texts, *f.text)
		}
	}
	texts = append(texts, string(out.ZoneNotesRaw), string(out.SkinScoreNotesRaw))
	return texts
}

func coachSnapshot(out *CoachStructuredOutput) string {
	raw, _ := json.Marshal(out)
	return string(raw)
}

func detailFields(d *dto.SkinCoachDetail) []polishField {
	fields := []polishField{
		{text: &d.SituationSummary, keepMay: true, opening: true, main: true},
		{text: &d.SummaryNotes, keepMay: true},
		{text: &d.ConcernAlignment},
		{text: &d.MedicalDisclaimer},
		{text: &d.PhotoLimitedNote},
	}
	fields = append(fields, stringListFields(d.Strengths)...)
	fields = append(fields, stringListFields(d.RoutineHints)...)
	fields = append(fields, stringListFields(d.AvoidOrPatch)...)
	fields = append(fields, stringListFields(d.SafetyReminders)...)
	fields = append(fields, stringListFields(d.ClarifyQuestions)...)
	for i := range d.Improvements {
		fields = append(fields, polishField{text: &d.Improvements[i].Tip}, polishField{text: &d.Improvements[i].Why})
	}
	for i := range d.CareSuggestions {
		fields = append(fields,
			polishField{text: &d.CareSuggestions[i].Step},
			polishField{text: &d.CareSuggestions[i].Why},
			polishField{text: &d.CareSuggestions[i].SafetyNote},
		)
	}
	for i := range d.ProductSuggestions {
		item := &d.ProductSuggestions[i]
		fields = append(fields,
			polishField{text: &item.Reason},
			polishField{text: &item.Step},
			polishField{text: &item.HowToUse},
			polishField{text: &item.Caution},
		)
		fields = append(fields, stringListFields(item.Benefits)...)
	}
	for i := range d.ProductGuidance {
		item := &d.ProductGuidance[i]
		fields = append(fields,
			polishField{text: &item.Why},
			polishField{text: &item.Step},
			polishField{text: &item.HowToUse},
			polishField{text: &item.Caution},
		)
		fields = append(fields, stringListFields(item.Benefits)...)
	}
	for i := range d.ZoneNotes {
		fields = append(fields, polishField{text: &d.ZoneNotes[i].Note, zoneNote: true})
	}
	if d.SkinScoreNotes != nil {
		n := d.SkinScoreNotes
		fields = append(fields,
			polishField{text: &n.Overall},
			polishField{text: &n.Hydration},
			polishField{text: &n.Clarity},
			polishField{text: &n.Barrier},
		)
	}
	return fields
}

func detailTexts(d *dto.SkinCoachDetail) []string {
	var texts []string
	for _, f := range detailFields(d) {
		if f.text != nil {
			texts = append(texts, *f.text)
		}
	}
	return texts
}

func stringListFields(items []string) []polishField {
	out := make([]polishField, len(items))
	for i := range items {
		out[i] = polishField{text: &items[i]}
	}
	return out
}

func replaceFold(s, from, to string) string {
	if from == "" {
		return s
	}
	lowS := strings.ToLower(s)
	lowFrom := strings.ToLower(from)
	var b strings.Builder
	for {
		i := strings.Index(lowS, lowFrom)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(to)
		s = s[i+len(from):]
		lowS = lowS[i+len(lowFrom):]
	}
}

func replacePhraseKeepCase(s, phrase, repl string) string {
	if phrase == "" {
		return s
	}
	var b strings.Builder
	for {
		idx, size := findPhrase(s, phrase)
		if idx < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:idx])
		b.WriteString(matchVoiceCase(s[idx:idx+size], repl))
		s = s[idx+size:]
	}
}

func replacePhraseSkipCapital(s, phrase, repl string) string {
	return replacePhraseOpt(s, phrase, repl, true)
}

func replacePhrase(s, phrase, repl string) string {
	return replacePhraseOpt(s, phrase, repl, false)
}

func replacePhraseOpt(s, phrase, repl string, skipCapital bool) string {
	if phrase == "" {
		return s
	}
	var b strings.Builder
	for {
		idx, size := findPhrase(s, phrase)
		if idx < 0 {
			b.WriteString(s)
			return b.String()
		}
		if skipCapital {
			r, _ := utf8.DecodeRuneInString(s[idx:])
			if unicode.IsUpper(r) {
				b.WriteString(s[:idx+size])
				s = s[idx+size:]
				continue
			}
		}
		b.WriteString(s[:idx])
		if repl != "" {
			b.WriteString(repl)
		}
		s = s[idx+size:]
	}
}

func findPhrase(s, phrase string) (int, int) {
	lowS := strings.ToLower(s)
	lowP := strings.ToLower(phrase)
	from := 0
	for {
		i := strings.Index(lowS[from:], lowP)
		if i < 0 {
			return -1, 0
		}
		i += from
		end := i + len(lowP)
		if bounded(lowS, i, end) {
			return i, len(phrase)
		}
		from = i + 1
		if from >= len(lowS) {
			return -1, 0
		}
	}
}

func bounded(low string, start, end int) bool {
	if start > 0 {
		r, _ := utf8.DecodeLastRuneInString(low[:start])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	if end < len(low) {
		r, _ := utf8.DecodeRuneInString(low[end:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
