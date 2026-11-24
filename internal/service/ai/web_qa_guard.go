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
	UserSaidHomQua     bool
	Locale             string
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
		UserSaidHomQua:     strings.Contains(strings.ToLower(note), "hôm qua"),
		Locale:             CheckLocale(check),
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
			s = stripInvalidComparisonWhen(s, qa.UserSaidHomQua)
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

// indexFakeNoteClaim finds «mày/bạn (có|đã)? ghi|ghi chú|nói|bảo|kể là|rằng».
// «bạn bè» does not match, and «mày nhớ ghi chú» does not either.
func indexFakeNoteClaim(s string) int {
	low := strings.ToLower(s)
	best := -1
	for _, who := range []string{"mày", "bạn"} {
		from := 0
		for {
			rel, size := findPhrase(low[from:], who)
			if rel < 0 {
				break
			}
			idx := from + rel
			rest := strings.TrimLeft(low[idx+size:], " \t")
			if next, ok := skipWholeWord(rest, "có"); ok {
				rest = next
			} else if next, ok := skipWholeWord(rest, "đã"); ok {
				rest = next
			}
			for _, verb := range []string{"ghi chú", "ghi", "nói", "bảo", "kể"} {
				next, ok := skipWholeWord(rest, verb)
				if !ok {
					continue
				}
				if _, ok := skipWholeWord(next, "là"); ok {
					if best < 0 || idx < best {
						best = idx
					}
					break
				}
				if _, ok := skipWholeWord(next, "rằng"); ok {
					if best < 0 || idx < best {
						best = idx
					}
					break
				}
			}
			from = idx + size
		}
	}
	return best
}

func skipWholeWord(s, word string) (string, bool) {
	if !strings.HasPrefix(s, word) {
		return s, false
	}
	after := s[len(word):]
	if after != "" {
		r, _ := utf8.DecodeRuneInString(after)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return s, false
		}
	}
	return strings.TrimLeft(after, " \t"), true
}

// stripInvalidComparison removes a whole sentence that compares the skin with
// last time: "so với lần trước", "hơn/kém lần trước", "đỡ … hơn lần trước".
// Advice such as "Lần sau chụp giống lần trước…" stays.
func stripInvalidComparison(s string) string {
	return stripInvalidComparisonWhen(s, false)
}

func stripInvalidComparisonWhen(s string, userSaidHomQua bool) string {
	parts := splitSentences(s)
	kept := make([]string, 0, len(parts))
	for _, sentence := range parts {
		if skinComparisonSentence(strings.ToLower(sentence), userSaidHomQua) {
			continue
		}
		kept = append(kept, sentence)
	}
	return strings.TrimSpace(strings.Join(kept, ""))
}

func skinComparisonSentence(low string, userSaidHomQua bool) bool {
	// These are fabricated comparisons even when the sentence ends in "nha".
	if strings.Contains(low, "so với lần trước") || strings.Contains(low, "kém lần trước") {
		return true
	}
	if userSaidHomQua && strings.Contains(low, "hôm qua") && !otherTimeAnchor(low) {
		return false
	}
	if adviceSentence(low) && !skinSelfComparison(low) {
		return false
	}
	if questionSentence(low) || photoTakingSentence(low) || keepLikeLastTimeAdvice(low) {
		return false
	}
	if !comparisonTimeWord(low) {
		return false
	}
	return strings.Contains(low, "hơn") || stillLikeBefore(low)
}

// adviceSentence is care advice, not a trailing "nha" on a skin comparison.
// The marker has to sit with a user-action verb.
func adviceSentence(low string) bool {
	if !adviceMarker(low) {
		return false
	}
	for _, verb := range []string{"rửa", "thoa", "bôi", "ngủ", "uống", "dùng", "đắp", "tránh", "chụp", "giữ"} {
		if _, n := findPhrase(low, verb); n > 0 {
			return true
		}
	}
	return false
}

func adviceMarker(low string) bool {
	if _, n := findPhrase(low, "nha"); n > 0 {
		return true
	}
	_, n := findPhrase(low, "là được")
	return n > 0
}

func skinSelfComparison(low string) bool {
	if !strings.Contains(low, "hơn") {
		if _, n := findPhrase(low, "đỡ"); n == 0 {
			return false
		}
	}
	for _, part := range []string{"da", "má", "nốt", "mụn", "vùng"} {
		if _, n := findPhrase(low, part); n > 0 {
			return true
		}
	}
	return false
}

func otherTimeAnchor(low string) bool {
	for _, word := range []string{"lần trước", "hôm trước", "tuần trước"} {
		if strings.Contains(low, word) {
			return true
		}
	}
	return false
}

func comparisonTimeWord(low string) bool {
	for _, word := range []string{"hôm qua", "hôm trước", "tuần trước", "lần trước"} {
		if strings.Contains(low, word) {
			return true
		}
	}
	return false
}

func stillLikeBefore(low string) bool {
	i := strings.Index(low, "vẫn")
	return i >= 0 && strings.Contains(low[i:], "như")
}

func photoTakingSentence(low string) bool {
	return strings.Contains(low, "chụp")
}

func keepLikeLastTimeAdvice(low string) bool {
	i := strings.Index(low, "giữ")
	return i >= 0 && strings.Contains(low[i:], "như lần trước")
}

func questionSentence(low string) bool {
	trimmed := strings.TrimSpace(low)
	if strings.Contains(trimmed, "?") || strings.Contains(trimmed, "？") {
		return true
	}
	end := strings.TrimRight(trimmed, " \t.!…")
	for _, suf := range []string{"gì vậy", "gì thế", "gì không", "chưa"} {
		if strings.HasSuffix(end, suf) {
			return true
		}
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
		repl := "Da dễ bóng dầu"
		if strings.Contains(strings.ToLower(sentence), "không sao") {
			repl = "Da dễ bóng dầu, nhưng không sao"
		}
		parts[i] = repl + "."
		changed = true
	}
	if !changed {
		return s
	}
	return strings.Join(parts, "")
}

func plainLoanwords(s string) string {
	s = replaceTreatWord(s)
	s = replaceCheckWord(s)
	s = replacePhraseKeepCase(s, "tự lẻ", "tự chích")
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

// replaceTreatWord rewrites the English step name in Vietnamese web copy.
// "sau treat" becomes "sau đó là đặc trị". "treatment", "Routine", "Toner",
// and "check-in" are left alone.
func replaceTreatWord(s string) string {
	s = replacePhraseKeepCase(s, "sau treat", "sau đó là đặc trị")
	return replacePhraseKeepCase(s, "treat", "đặc trị")
}

// replaceCheckWord maps a standalone "check" to "kiểm tra".
// "check-in" stays a single word.
func replaceCheckWord(s string) string {
	var b strings.Builder
	for {
		idx, size := findPhrase(s, "check")
		if idx < 0 {
			b.WriteString(s)
			return b.String()
		}
		after := s[idx+size:]
		if after != "" {
			next, _ := utf8.DecodeRuneInString(after)
			if next == '-' || tokenHasLinkMark(s, idx, size) {
				b.WriteString(s[:idx+size])
				s = s[idx+size:]
				continue
			}
		} else if tokenHasLinkMark(s, idx, size) {
			b.WriteString(s[:idx+size])
			s = s[idx+size:]
			continue
		}
		b.WriteString(s[:idx])
		b.WriteString(matchVoiceCase(s[idx:idx+size], "kiểm tra"))
		s = s[idx+size:]
	}
}

func tokenHasLinkMark(s string, idx, size int) bool {
	start := idx
	for start > 0 {
		r, n := utf8.DecodeLastRuneInString(s[:start])
		if unicode.IsSpace(r) {
			break
		}
		start -= n
	}
	end := idx + size
	for end < len(s) {
		r, n := utf8.DecodeRuneInString(s[end:])
		if unicode.IsSpace(r) {
			break
		}
		end += n
	}
	token := s[start:end]
	return strings.Contains(token, "/") || strings.Contains(token, ".")
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

// applyWebVIPost is the Vietnamese web pass. English and Android never call it.
func applyWebVIPost(out *CoachStructuredOutput, zones []polishZone, scores []polishScore, qa webQA) {
	if out == nil {
		return
	}
	rewrite := func(s string) string {
		return viRewrite(s, false, qa)
	}
	hadSituation := strings.TrimSpace(out.SituationAnalysis) != ""
	out.SituationAnalysis = rewrite(out.SituationAnalysis)
	out.SummaryNotes = rewrite(out.SummaryNotes)
	out.ConcernAlignment = rewrite(out.ConcernAlignment)
	out.MedicalDisclaimer = rewrite(out.MedicalDisclaimer)
	for i := range out.Strengths {
		out.Strengths[i] = rewrite(out.Strengths[i])
	}
	for i := range out.RoutineHints {
		out.RoutineHints[i] = rewrite(out.RoutineHints[i])
	}
	for i := range out.AvoidOrPatch {
		out.AvoidOrPatch[i] = rewrite(out.AvoidOrPatch[i])
	}
	for i := range out.SafetyReminders {
		out.SafetyReminders[i] = rewrite(out.SafetyReminders[i])
	}
	for i := range out.Improvements {
		out.Improvements[i].Tip = rewrite(out.Improvements[i].Tip)
		out.Improvements[i].Why = rewrite(out.Improvements[i].Why)
	}
	for i := range out.CareSuggestions {
		out.CareSuggestions[i].Step = rewrite(out.CareSuggestions[i].Step)
		out.CareSuggestions[i].Why = rewrite(out.CareSuggestions[i].Why)
		out.CareSuggestions[i].SafetyNote = rewrite(out.CareSuggestions[i].SafetyNote)
	}
	for i := range zones {
		zones[i].note = viRewrite(zones[i].note, zoneSevere(zones[i].severity), qa)
	}
	for i := range scores {
		scores[i].val = rewrite(scores[i].val)
	}
	explained := false
	out.ConcernAlignment = glossMiliaOnce(out.ConcernAlignment, &explained)
	explained = false
	for i := range zones {
		zones[i].note = glossMiliaOnce(zones[i].note, &explained)
	}
	out.SituationAnalysis = insertCuaMay(out.SituationAnalysis, screenMayCount(out, zones, scores))
	if hadSituation && strings.TrimSpace(out.SituationAnalysis) == "" {
		out.SituationAnalysis = neutralMainSentence
	}
}

func applyWebVIDetail(d *dto.SkinCoachDetail, qa webQA) {
	if d == nil {
		return
	}
	rewrite := func(s string) string {
		return viRewrite(s, false, qa)
	}
	hadSituation := strings.TrimSpace(d.SituationSummary) != ""
	d.SituationSummary = rewrite(d.SituationSummary)
	d.SummaryNotes = rewrite(d.SummaryNotes)
	d.ConcernAlignment = rewrite(d.ConcernAlignment)
	d.MedicalDisclaimer = rewrite(d.MedicalDisclaimer)
	d.PhotoLimitedNote = rewrite(d.PhotoLimitedNote)
	d.Strengths = rewriteList(d.Strengths, rewrite)
	d.RoutineHints = rewriteList(d.RoutineHints, rewrite)
	d.AvoidOrPatch = rewriteList(d.AvoidOrPatch, rewrite)
	d.SafetyReminders = rewriteList(d.SafetyReminders, rewrite)
	for i := range d.Improvements {
		d.Improvements[i].Tip = rewrite(d.Improvements[i].Tip)
		d.Improvements[i].Why = rewrite(d.Improvements[i].Why)
	}
	for i := range d.CareSuggestions {
		d.CareSuggestions[i].Step = rewrite(d.CareSuggestions[i].Step)
		d.CareSuggestions[i].Why = rewrite(d.CareSuggestions[i].Why)
		d.CareSuggestions[i].SafetyNote = rewrite(d.CareSuggestions[i].SafetyNote)
	}
	for i := range d.ZoneNotes {
		d.ZoneNotes[i].Note = viRewrite(d.ZoneNotes[i].Note, zoneSevere(d.ZoneNotes[i].Severity), qa)
	}
	if d.SkinScoreNotes != nil {
		n := d.SkinScoreNotes
		n.Overall = rewrite(n.Overall)
		n.Hydration = rewrite(n.Hydration)
		n.Clarity = rewrite(n.Clarity)
		n.Barrier = rewrite(n.Barrier)
	}
	explained := false
	d.ConcernAlignment = glossMiliaOnce(d.ConcernAlignment, &explained)
	explained = false
	for i := range d.ZoneNotes {
		d.ZoneNotes[i].Note = glossMiliaOnce(d.ZoneNotes[i].Note, &explained)
	}
	d.SituationSummary = insertCuaMay(d.SituationSummary, detailMayCount(d))
	if hadSituation && strings.TrimSpace(d.SituationSummary) == "" {
		d.SituationSummary = neutralMainSentence
	}
}

func rewriteList(items []string, rewrite func(string) string) []string {
	for i := range items {
		items[i] = rewrite(items[i])
	}
	return items
}

func detailMayCount(d *dto.SkinCoachDetail) int {
	n := countWord(d.SituationSummary, "mày")
	n += countWord(d.ConcernAlignment, "mày")
	n += countWord(d.MedicalDisclaimer, "mày")
	for _, s := range d.RoutineHints {
		n += countWord(s, "mày")
	}
	for _, s := range d.AvoidOrPatch {
		n += countWord(s, "mày")
	}
	for _, s := range d.SafetyReminders {
		n += countWord(s, "mày")
	}
	for _, item := range d.Improvements {
		n += countWord(item.Tip, "mày") + countWord(item.Why, "mày")
	}
	for _, item := range d.CareSuggestions {
		n += countWord(item.Step, "mày") + countWord(item.Why, "mày") + countWord(item.SafetyNote, "mày")
	}
	for _, z := range d.ZoneNotes {
		n += countWord(z.Note, "mày")
	}
	if d.SkinScoreNotes != nil {
		n += countWord(d.SkinScoreNotes.Overall, "mày")
		n += countWord(d.SkinScoreNotes.Hydration, "mày")
		n += countWord(d.SkinScoreNotes.Clarity, "mày")
		n += countWord(d.SkinScoreNotes.Barrier, "mày")
	}
	return n
}

func viRewrite(s string, severe bool, qa webQA) string {
	if qa.NoteKnown && !qa.HasUserNote {
		s = stripVIInventedNote(s)
	}
	return dropFalseUnclear(softenAssertedGroup(plainPatchTest(s)), severe)
}

func stripVIInventedNote(s string) string {
	parts := splitSentences(s)
	kept := make([]string, 0, len(parts))
	for _, sentence := range parts {
		next, drop := trimFakeNoteClause(sentence)
		if drop {
			continue
		}
		kept = append(kept, next)
	}
	return strings.TrimSpace(strings.Join(kept, ""))
}

// trimFakeNoteClause removes a made-up user quote.
// A leading clause that ends with «, và» or «, nhưng» is cut and the rest is
// capitalized. Any other sentence that matches is dropped. «Bác sĩ nói là…» stays.
func trimFakeNoteClause(sentence string) (string, bool) {
	if strings.Contains(strings.ToLower(sentence), "bác sĩ nói là") {
		return sentence, false
	}
	idx := indexFakeNoteClaim(sentence)
	if idx < 0 {
		return sentence, false
	}
	lead := len(sentence) - len(strings.TrimLeft(sentence, " \t"))
	if idx == lead {
		if next, ok := cutLeadingFakeClause(sentence, idx); ok {
			if strings.TrimSpace(next) == "" {
				return "", true
			}
			return next, false
		}
	}
	return "", true
}

func cutLeadingFakeClause(sentence string, from int) (string, bool) {
	rest := sentence[from:]
	low := strings.ToLower(rest)
	best, bestEnd := -1, 0
	for _, conj := range []string{"và", "nhưng"} {
		search := 0
		for search < len(low) {
			i := strings.Index(low[search:], ",")
			if i < 0 {
				break
			}
			i += search
			afterComma := low[i+len(","):]
			spaces := len(afterComma) - len(strings.TrimLeft(afterComma, " \t"))
			word := strings.TrimLeft(afterComma, " \t")
			next, ok := skipWholeWord(word, conj)
			if ok {
				end := i + len(",") + spaces + len(conj)
				if best < 0 || i < best {
					best, bestEnd = i, end
				}
				break
			}
			_ = next
			search = i + 1
		}
	}
	if best < 0 {
		return "", false
	}
	after := strings.TrimLeft(rest[bestEnd:], " \t")
	return sentence[:from] + capitalizeFirst(after), true
}

func zoneSevere(sev string) bool {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "moderate", "pronounced":
		return true
	default:
		return false
	}
}

func screenMayCount(out *CoachStructuredOutput, zones []polishZone, scores []polishScore) int {
	n := countWord(out.SituationAnalysis, "mày")
	n += countWord(out.ConcernAlignment, "mày")
	n += countWord(out.MedicalDisclaimer, "mày")
	for _, s := range out.RoutineHints {
		n += countWord(s, "mày")
	}
	for _, s := range out.AvoidOrPatch {
		n += countWord(s, "mày")
	}
	for _, s := range out.SafetyReminders {
		n += countWord(s, "mày")
	}
	for _, item := range out.Improvements {
		n += countWord(item.Tip, "mày") + countWord(item.Why, "mày")
	}
	for _, item := range out.CareSuggestions {
		n += countWord(item.Step, "mày") + countWord(item.Why, "mày") + countWord(item.SafetyNote, "mày")
	}
	for _, z := range zones {
		n += countWord(z.note, "mày")
	}
	for _, sc := range scores {
		n += countWord(sc.val, "mày")
	}
	return n
}

func countWord(s, word string) int {
	n := 0
	for {
		idx, size := findPhrase(s, word)
		if idx < 0 {
			return n
		}
		n++
		s = s[idx+size:]
	}
}

func insertCuaMay(s string, screenMay int) string {
	parts := splitSentences(s)
	if len(parts) == 0 || screenMay >= 2 || countWord(parts[0], "mày") > 0 {
		return s
	}
	first := parts[0]
	if idx, size, ok := firstZoneName(first); ok {
		if followingWordIs(first[idx+size:], "của") {
			return s
		}
		parts[0] = first[:idx+size] + " của mày" + first[idx+size:]
		return strings.Join(parts, "")
	}
	if idx, size := findPhrase(first, "da mặt"); idx >= 0 {
		if followingWordIs(first[idx+size:], "của") {
			return s
		}
		parts[0] = first[:idx+size] + " của mày" + first[idx+size:]
		return strings.Join(parts, "")
	}
	idx, size := findPhrase(first, "hôm nay da")
	if idx < 0 || followingWordIs(first[idx+size:], "của") {
		return s
	}
	parts[0] = first[:idx+size] + " của mày" + first[idx+size:]
	return strings.Join(parts, "")
}

func firstZoneName(s string) (int, int, bool) {
	bestI, bestN := -1, 0
	for _, name := range []string{
		"hai bên má",
		"vùng má trái", "vùng má phải",
		"má trái", "má phải",
		"quanh miệng", "vùng chữ t",
		"vùng trán", "vùng mũi", "vùng cằm",
		"trán", "mũi", "cằm", "má",
	} {
		idx, size := findPhrase(s, name)
		if idx < 0 {
			continue
		}
		if bestI < 0 || idx < bestI || (idx == bestI && size > bestN) {
			bestI, bestN = idx, size
		}
	}
	if bestI < 0 {
		return 0, 0, false
	}
	return bestI, bestN, true
}

func followingWordIs(s, word string) bool {
	s = strings.TrimLeft(s, " \t")
	idx, size := findPhrase(s, word)
	return idx == 0 && size > 0
}

func dropFalseUnclear(s string, severe bool) string {
	parts := splitSentences(s)
	changed := false
	for i, sentence := range parts {
		before, punct, ok := trailingUnclear(sentence)
		if !ok {
			continue
		}
		if !severe && !sentenceHasVisibleSign(strings.ToLower(before)) {
			continue
		}
		parts[i] = before + punct
		changed = true
	}
	if !changed {
		return s
	}
	return strings.Join(parts, "")
}

func sentenceHasVisibleSign(low string) bool {
	for _, sign := range []string{"mụn viêm", "ửng đỏ", "nốt", "đỏ", "sưng", "mụn", "thâm", "sần"} {
		if strings.Contains(low, sign) {
			return true
		}
	}
	return false
}

// trailingUnclear matches only ", chưa rõ ràng" at the end of a sentence.
// Mid-sentence, a sentence that starts with the phrase, or a phrase with no
// comma stays as written. punct is the period or exclamation that was kept.
func trailingUnclear(sentence string) (before, punct string, ok bool) {
	trimmed := strings.TrimRight(sentence, " \t")
	low := strings.ToLower(trimmed)
	idx := strings.LastIndex(low, "chưa rõ ràng")
	if idx < 0 {
		return "", "", false
	}
	after := strings.TrimSpace(low[idx+len("chưa rõ ràng"):])
	if after != "" && after != "." && after != "!" {
		return "", "", false
	}
	head := strings.TrimRight(trimmed[:idx], " \t")
	if !strings.HasSuffix(head, ",") {
		return "", "", false
	}
	head = strings.TrimRight(strings.TrimSuffix(head, ","), " \t")
	if after == "!" {
		punct = "!"
	} else if after == "." {
		punct = "."
	}
	return head, punct, true
}

func softenAssertedGroup(s string) string {
	type pair struct{ from string }
	prefixes := []string{"đó là dấu hiệu của ", "đó là dấu hiệu ", "đó là "}
	low := strings.ToLower(s)
	for _, prefix := range prefixes {
		from := 0
		for {
			idx := strings.Index(low[from:], prefix)
			if idx < 0 {
				break
			}
			idx += from
			xStart := idx + len(prefix)
			xEnd := xStart
			for xEnd < len(s) {
				r, n := utf8.DecodeRuneInString(s[xEnd:])
				if r == ',' || r == '.' || r == '!' || r == '?' || r == ';' || r == '—' {
					break
				}
				xEnd += n
			}
			x := strings.ToLower(s[xStart:xEnd])
			if !strings.Contains(x, "mụn ẩn") && !strings.Contains(x, "milia") {
				from = xEnd
				continue
			}
			repl := "trông giống " + strings.TrimSpace(s[xStart:xEnd])
			if strings.HasPrefix(prefix, "đó là dấu hiệu") {
				repl = "trông giống dấu hiệu " + strings.TrimSpace(s[xStart:xEnd])
			}
			if atSentenceStart(s, idx) {
				repl = capitalizeFirst(repl)
			}
			s = s[:idx] + repl + s[xEnd:]
			low = strings.ToLower(s)
			from = idx + len(repl)
		}
	}
	return s
}

func glossMiliaOnce(s string, done *bool) string {
	if done == nil || *done || strings.TrimSpace(s) == "" {
		return s
	}
	idx, size := findPhrase(s, "milia")
	if idx < 0 {
		return s
	}
	*done = true
	if alreadyExplained(s, idx, size, "hạt nhỏ trắng dưới da") || strings.Contains(strings.ToLower(s), "hạt nhỏ dưới da") {
		return s
	}
	return s[:idx+size] + " (hạt nhỏ trắng dưới da)" + s[idx+size:]
}

func plainPatchTest(s string) string {
	low := strings.ToLower(s)
	for {
		iPatch := indexFold(low, "patch-test")
		iSpace := indexFold(low, "patch test")
		idx, n := iPatch, len("patch-test")
		if iPatch < 0 || (iSpace >= 0 && iSpace < iPatch) {
			idx, n = iSpace, len("patch test")
		}
		if idx < 0 {
			break
		}
		rest := s[idx+n:]
		trimmed := strings.TrimLeft(rest, " ")
		if strings.HasPrefix(strings.ToLower(trimmed), "(") {
			end := strings.Index(trimmed, ")")
			if end >= 0 {
				inside := strings.TrimSpace(trimmed[1:end])
				if r, _ := utf8.DecodeRuneInString(s[idx:]); unicode.IsUpper(r) {
					inside = capitalizeFirst(inside)
				}
				gap := len(rest) - len(trimmed)
				s = s[:idx] + inside + s[idx+n+gap+end+1:]
				low = strings.ToLower(s)
				continue
			}
		}
		s = s[:idx] + matchVoiceCase(s[idx:idx+n], "thử trước ở một vùng nhỏ") + s[idx+n:]
		low = strings.ToLower(s)
	}
	s = replaceFold(s, "thử trước ở một vùng nhỏ trước ở vùng nhỏ", "thử trước ở một vùng nhỏ")
	s = dropExtraPatchTruoc(s)
	return squeezeSpaces(s)
}

func dropExtraPatchTruoc(s string) string {
	const phrase = "thử trước ở một vùng nhỏ"
	var b strings.Builder
	low := strings.ToLower(s)
	for {
		i := strings.Index(low, phrase)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := i + len(phrase)
		b.WriteString(s[:end])
		rest := s[end:]
		lowRest := low[end:]
		spaces := 0
		for spaces < len(lowRest) && (lowRest[spaces] == ' ' || lowRest[spaces] == '\t') {
			spaces++
		}
		word := lowRest[spaces:]
		next, ok := skipWholeWord(word, "trước")
		followedByO := strings.HasPrefix(strings.TrimLeft(next, " \t"), "ở")
		if ok && !followedByO {
			skipped := spaces + len("trước")
			s = rest[skipped:]
			low = lowRest[skipped:]
			continue
		}
		s = rest
		low = lowRest
	}
}

func atSentenceStart(s string, idx int) bool {
	head := strings.TrimRight(strings.TrimLeft(s[:idx], " \t\"'«“”"), " \t")
	if head == "" {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(head)
	return r == '.' || r == '!' || r == '?' || r == '…'
}

func indexFold(low, phrase string) int {
	return strings.Index(low, strings.ToLower(phrase))
}
