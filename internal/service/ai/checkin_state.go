package ai

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
)

// checkInStatePromptRule is shared by the web and Android coach system prompts.
// It is a no-op when the user message has no CHECK_IN_STATE block.
const checkInStatePromptRule = `
## Khớp trạng thái thật (BẮT BUỘC khi có CHECK_IN_STATE)
- Đọc CHECK_IN_STATE. Không bịa số lần check-in. history: none hoặc first_check_in=yes thì không nói "so với lần trước".
- first_check_in=yes: CẤM khen sự đều. Cấm "chăm sóc da đều đặn", "đều đặn", "kiên trì", "duy trì đều", "quay lại đều", streak. Lần đầu chưa có lịch sử. Bỏ qua lời khen đều trong COACH_ACTION. Chỉ nhắc việc hôm nay (gửi ảnh, ghi chú).
- today_skin=fine hoặc today_skin=good: CẤM hỏi da có dịu hơn không. Cấm "muốn xem vùng đó dịu hơn không", "dịu hơn", "đỡ đỏ", "bớt viêm" khi đó là câu hỏi theo dõi. Da đang ổn thì khuyên giữ mức này.
- today_skin=uneasy, hoặc today_skin=unknown mà ảnh đang đỏ hoặc viêm: được hỏi vùng đó dịu hơn không.
- Có score trong history thì so với số đó. Không có history thì không bịa xu hướng.
Không có block CHECK_IN_STATE thì bỏ qua mục này.
`

// CheckInHistoryFact is one older check-in the coach may compare against.
type CheckInHistoryFact struct {
	Date  string
	Score *float64
	Tags  string
	Note  string
}

// CheckInCoachFacts is the user's real check-in state. Zero value means
// "unknown": the coach is not told this is a first visit, and the guard
// does not rewrite advice.
type CheckInCoachFacts struct {
	// CountKnown is true when the lifetime count was loaded. The count
	// includes the check-in being coached, which is already saved.
	CountKnown         bool
	TotalIncludesToday int64
	History            []CheckInHistoryFact
	// TodayRating is "", "fine", "good", or "uneasy".
	TodayRating string
	// TodayScoreText is the raw rating or number for the prompt, such as
	// "good" or "4". Empty when the user did not rate today.
	TodayScoreText string
}

// FirstEver is true only when we know there is no older check-in.
// A non-empty history wins over a stale count of 1.
func (f CheckInCoachFacts) FirstEver() bool {
	if len(f.History) > 0 {
		return false
	}
	return f.CountKnown && f.TotalIncludesToday <= 1
}

// SkinFineOrGood is true when today's own rating is fine or good.
func (f CheckInCoachFacts) SkinFineOrGood() bool {
	return f.TodayRating == "fine" || f.TodayRating == "good"
}

// DeriveCheckInCoachFacts reads today's self-rating from the check.
// Count and history are filled by the caller, which owns the repositories.
func DeriveCheckInCoachFacts(check *domain.SkinCheck) CheckInCoachFacts {
	var facts CheckInCoachFacts
	if check == nil {
		return facts
	}
	if rating, text, ok := ratingFromClimate(check.ClimateContext); ok {
		facts.TodayRating = rating
		facts.TodayScoreText = text
		return facts
	}
	if rating, text, ok := ratingFromNote(check.UserNote); ok {
		facts.TodayRating = rating
		facts.TodayScoreText = text
		return facts
	}
	if rating, text, ok := ratingFromTags(check); ok {
		facts.TodayRating = rating
		facts.TodayScoreText = text
	}
	return facts
}

// HistoryFactsFromChecks copies prior rows into prompt facts, including the
// saved overall score when the analysis finished.
func HistoryFactsFromChecks(rows []domain.SkinCheck) []CheckInHistoryFact {
	if len(rows) == 0 {
		return nil
	}
	out := make([]CheckInHistoryFact, 0, len(rows))
	for _, row := range rows {
		fact := CheckInHistoryFact{
			Date: row.CheckDate.UTC().Format("2006-01-02"),
			Note: truncateRunes(row.UserNote, 80),
		}
		if conds, err := dto.DecodeStringSlice(row.Conditions); err == nil && len(conds) > 0 {
			fact.Tags = strings.Join(conds, ", ")
		}
		if score, ok := overallScore(row.Analysis); ok {
			fact.Score = &score
		}
		out = append(out, fact)
	}
	return out
}

// FormatCheckInStateBlock is the CHECK_IN_STATE section appended to the coach
// user message. It always names count, history, and score, using "unknown"
// when a fact was not loaded.
func FormatCheckInStateBlock(f CheckInCoachFacts) string {
	var b strings.Builder
	b.WriteString("\nCHECK_IN_STATE (authoritative facts — advice must match; do not invent a streak, a prior visit, or a skin problem):\n")
	if f.CountKnown {
		fmt.Fprintf(&b, "- check_in_count_including_today: %d\n", f.TotalIncludesToday)
		prior := f.TotalIncludesToday - 1
		if prior < 0 {
			prior = 0
		}
		if len(f.History) > 0 && prior < int64(len(f.History)) {
			prior = int64(len(f.History))
		}
		fmt.Fprintf(&b, "- prior_check_ins: %d\n", prior)
	} else {
		b.WriteString("- check_in_count_including_today: unknown\n")
		if len(f.History) > 0 {
			fmt.Fprintf(&b, "- prior_check_ins: %d\n", len(f.History))
		} else {
			b.WriteString("- prior_check_ins: unknown\n")
		}
	}
	if f.FirstEver() {
		b.WriteString("- first_check_in: yes\n")
	} else if f.CountKnown || len(f.History) > 0 {
		b.WriteString("- first_check_in: no\n")
	} else {
		b.WriteString("- first_check_in: unknown\n")
	}
	skin := f.TodayRating
	if skin == "" {
		skin = "unknown"
	}
	fmt.Fprintf(&b, "- today_skin: %s\n", skin)
	if strings.TrimSpace(f.TodayScoreText) != "" {
		fmt.Fprintf(&b, "- today_score: %s\n", strings.TrimSpace(f.TodayScoreText))
	} else {
		b.WriteString("- today_score: unknown\n")
	}
	if len(f.History) == 0 {
		b.WriteString("- history: none\n")
	} else {
		b.WriteString("- history (older than today, newest first):\n")
		for _, h := range f.History {
			fmt.Fprintf(&b, "  - %s", h.Date)
			if h.Score != nil {
				fmt.Fprintf(&b, " | score: %s", formatScoreNumber(*h.Score))
			}
			if h.Tags != "" {
				fmt.Fprintf(&b, " | tags: %s", h.Tags)
			}
			if h.Note != "" {
				fmt.Fprintf(&b, " | note: %s", h.Note)
			}
			b.WriteString("\n")
		}
	}
	if f.FirstEver() {
		b.WriteString("GUARDRAIL: first check-in — never praise consistency (no \"chăm sóc da đều đặn\", \"đều đặn\", \"kiên trì\", \"duy trì đều\").\n")
	}
	if f.SkinFineOrGood() {
		b.WriteString("GUARDRAIL: skin is fine/good — do not ask \"muốn xem vùng đó dịu hơn không\" or whether a zone will \"dịu hơn\" / \"đỡ đỏ\" / \"bớt viêm\".\n")
	}
	return b.String()
}

// ApplyCheckInAdviceGuard rewrites coach text that contradicts CHECK_IN_STATE.
// First check-ins lose consistency praise. Fine/good ratings lose questions
// that assume a sore spot should calm down. Returns whether any field changed.
func ApplyCheckInAdviceGuard(out *CoachStructuredOutput, facts CheckInCoachFacts, clientKind string) bool {
	if out == nil || (!facts.FirstEver() && !facts.SkinFineOrGood()) {
		return false
	}
	polite := IsAndroidCoachVoice(clientKind)
	drop := func(sentence string) bool {
		if facts.FirstEver() && isConsistencyPraise(sentence) {
			return true
		}
		return facts.SkinFineOrGood() && isCalmDownQuestion(sentence)
	}
	summaryFallback := adviceFallback(facts, polite)
	changed := false
	if next, ok := rewriteAdviceField(out.SituationAnalysis, drop, summaryFallback); ok {
		out.SituationAnalysis = next
		changed = true
	}
	if next, ok := rewriteAdviceField(out.ConcernAlignment, drop, ""); ok {
		out.ConcernAlignment = next
		changed = true
	}
	if next, ok := rewriteAdviceField(out.SummaryNotes, drop, summaryFallback); ok {
		out.SummaryNotes = next
		changed = true
	}
	if next, ok := rewriteStringList(out.Strengths, drop, summaryFallback); ok {
		out.Strengths = next
		changed = true
	}
	if next, ok := rewriteStringList(out.RoutineHints, drop, ""); ok {
		out.RoutineHints = next
		changed = true
	}
	if next, ok := rewriteStringList(out.AvoidOrPatch, drop, ""); ok {
		out.AvoidOrPatch = next
		changed = true
	}
	if next, ok := rewriteStringList(out.SafetyReminders, drop, ""); ok {
		out.SafetyReminders = next
		changed = true
	}
	if next, ok := salvageTipList(out.Improvements, drop, polite); ok {
		out.Improvements = next
		changed = true
	}
	if next, ok := salvageCareSuggestions(out.CareSuggestions, drop, polite); ok {
		out.CareSuggestions = next
		changed = true
	}
	for i := range out.ProductSuggestions {
		if next, ok := polishRewrittenTip(out.ProductSuggestions[i].Reason, drop, polite, true); ok {
			out.ProductSuggestions[i].Reason = next
			changed = true
		}
		if next, ok := polishRewrittenTip(out.ProductSuggestions[i].HowToUse, drop, polite, false); ok {
			out.ProductSuggestions[i].HowToUse = next
			changed = true
		}
		if next, ok := polishRewrittenTip(out.ProductSuggestions[i].Caution, drop, polite, false); ok {
			out.ProductSuggestions[i].Caution = next
			changed = true
		}
		if next, ok := rewriteStringList(out.ProductSuggestions[i].Benefits, drop, ""); ok {
			out.ProductSuggestions[i].Benefits = next
			changed = true
		}
	}
	for i := range out.ProductGuidance {
		if next, ok := polishRewrittenTip(out.ProductGuidance[i].Why, drop, polite, true); ok {
			out.ProductGuidance[i].Why = next
			changed = true
		}
		if next, ok := polishRewrittenTip(out.ProductGuidance[i].HowToUse, drop, polite, true); ok {
			out.ProductGuidance[i].HowToUse = next
			changed = true
		}
		if next, ok := polishRewrittenTip(out.ProductGuidance[i].Caution, drop, polite, false); ok {
			out.ProductGuidance[i].Caution = next
			changed = true
		}
		if next, ok := rewriteStringList(out.ProductGuidance[i].Benefits, drop, ""); ok {
			out.ProductGuidance[i].Benefits = next
			changed = true
		}
	}
	return changed
}

// neutralCareTip is a safe line used when stripping a banned phrase leaves a
// tip with nothing real to say. Web and Android each get their own voice.
func neutralCareTip(polite bool) string {
	if polite {
		return "Hôm nay bạn giữ bước dịu: rửa mặt nhẹ rồi dưỡng ẩm."
	}
	return "Hôm nay giữ bước dịu: rửa mặt nhẹ rồi dưỡng ẩm."
}

func adviceFallback(facts CheckInCoachFacts, polite bool) string {
	var parts []string
	if facts.FirstEver() {
		if polite {
			parts = append(parts, "Hôm nay là lần check-in đầu tiên, mình chỉ nhìn ngày hôm nay.")
		} else {
			parts = append(parts, "Hôm nay là lần check-in đầu, tao chỉ nhìn ngày hôm nay.")
		}
	}
	if facts.SkinFineOrGood() {
		if polite {
			parts = append(parts, "Mai chụp cùng góc nhé — mình muốn xem da bạn vẫn giữ được mức này không.")
		} else {
			parts = append(parts, "Mai chụp cùng góc nhé — tao muốn xem mày giữ được mức này không.")
		}
	}
	return strings.Join(parts, " ")
}

func isConsistencyPraise(s string) bool {
	low := strings.ToLower(s)
	for _, phrase := range []string{
		"chăm sóc da đều đặn",
		"chăm sóc da đều",
		"đều đặn",
		"kiên trì",
		"duy trì đều",
		"duy trì tốt",
		"quay lại đều",
		"check-in đều",
		"check in đều",
		"ghi nhật ký đều",
		"chăm đều",
		"thói quen đều",
	} {
		if strings.Contains(low, phrase) {
			return true
		}
	}
	return false
}

func isCalmDownQuestion(s string) bool {
	low := strings.ToLower(s)
	if !strings.Contains(low, "dịu hơn") &&
		!strings.Contains(low, "đỡ đỏ") &&
		!strings.Contains(low, "bớt đỏ") &&
		!strings.Contains(low, "bớt viêm") &&
		!strings.Contains(low, "đỡ viêm") &&
		!strings.Contains(low, "hết đỏ") {
		return false
	}
	if strings.Contains(low, "muốn xem") || strings.Contains(low, "xem vùng") || strings.Contains(low, "xem da") {
		return true
	}
	if strings.Contains(s, "?") {
		return true
	}
	return strings.Contains(low, "hơn không")
}

func rewriteAdviceField(s string, drop func(string) bool, fallback string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return s, false
	}
	parts := splitSentences(s)
	kept := make([]string, 0, len(parts))
	dropped := 0
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if drop(part) {
			dropped++
			continue
		}
		kept = append(kept, part)
	}
	if dropped == 0 {
		return s, false
	}
	meaningful := make([]string, 0, len(kept))
	for _, part := range kept {
		// "Bạn quá." or "!!!" next to a removed sentence is not a tip.
		if isMeaninglessAdvice(part) {
			continue
		}
		meaningful = append(meaningful, part)
	}
	joined := strings.TrimSpace(strings.Join(meaningful, " "))
	if isMeaninglessAdvice(joined) {
		joined = ""
	}
	// A dropped sentence still needs a replacement when other sentences stay,
	// so "Da hôm nay ổn." is not left without the keep-level follow-up.
	if fallback != "" && !strings.Contains(joined, fallback) {
		if joined == "" {
			return fallback, fallback != s
		}
		joined = joined + " " + fallback
	}
	return joined, joined != s
}

// polishRewrittenTip rewrites one tip field. A meaningless remainder is
// replaced with a neutral line when replace is set, and cleared otherwise
// (callers drop that item). Text that did not contain a banned phrase is
// left as-is.
func polishRewrittenTip(s string, drop func(string) bool, polite bool, replace bool) (string, bool) {
	next, ok := rewriteAdviceField(s, drop, "")
	if !ok {
		return s, false
	}
	if !isMeaninglessAdvice(next) {
		return next, true
	}
	if replace {
		return neutralCareTip(polite), true
	}
	return "", true
}

func salvageTipList(items []struct {
	Tip string `json:"tip"`
	Why string `json:"why"`
}, drop func(string) bool, polite bool) ([]struct {
	Tip string `json:"tip"`
	Why string `json:"why"`
}, bool) {
	if len(items) == 0 {
		return items, false
	}
	out := make([]struct {
		Tip string `json:"tip"`
		Why string `json:"why"`
	}, 0, len(items))
	changed := false
	for _, item := range items {
		tip, tipOK := rewriteAdviceField(item.Tip, drop, "")
		why, whyOK := rewriteAdviceField(item.Why, drop, "")
		if !tipOK && !whyOK {
			out = append(out, item)
			continue
		}
		changed = true
		if isMeaninglessAdvice(tip) {
			tip = neutralCareTip(polite)
		}
		if isMeaninglessAdvice(why) {
			why = neutralCareTip(polite)
		}
		item.Tip = tip
		item.Why = why
		out = append(out, item)
	}
	if !changed {
		return items, false
	}
	return out, true
}

func salvageCareSuggestions(items []CoachCareSuggestion, drop func(string) bool, polite bool) ([]CoachCareSuggestion, bool) {
	if len(items) == 0 {
		return items, false
	}
	out := make([]CoachCareSuggestion, 0, len(items))
	changed := false
	for _, item := range items {
		step, stepOK := rewriteAdviceField(item.Step, drop, "")
		why, whyOK := rewriteAdviceField(item.Why, drop, "")
		note, noteOK := rewriteAdviceField(item.SafetyNote, drop, "")
		if !stepOK && !whyOK && !noteOK {
			out = append(out, item)
			continue
		}
		changed = true
		if isMeaninglessAdvice(step) {
			step = neutralCareTip(polite)
		}
		if isMeaninglessAdvice(why) {
			why = neutralCareTip(polite)
		}
		if isMeaninglessAdvice(note) {
			note = ""
		}
		item.Step = step
		item.Why = why
		item.SafetyNote = note
		out = append(out, item)
	}
	if !changed {
		return items, false
	}
	return out, true
}

// isMeaninglessAdvice reports text that is empty, only punctuation or
// whitespace, or a few leftover words with no real care content.
func isMeaninglessAdvice(s string) bool {
	return len(adviceContentWords(s)) < 2
}

func adviceContentWords(s string) []string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteByte(' ')
		}
	}
	var out []string
	for _, w := range strings.Fields(b.String()) {
		if adviceFillerWords[w] {
			continue
		}
		out = append(out, w)
	}
	return out
}

// adviceFillerWords are glue left behind after a banned phrase is removed
// ("bạn quá", "nhớ nhé"). They are not a skin-care tip on their own.
var adviceFillerWords = map[string]bool{
	"ạ": true, "à": true, "ấy": true, "bạn": true, "các": true, "cái": true,
	"cho": true, "cơ": true, "cũng": true, "của": true, "có": true, "đã": true,
	"đang": true, "để": true, "được": true, "đi": true, "hay": true, "ha": true,
	"hả": true, "hey": true, "khi": true, "là": true, "lại": true, "luôn": true,
	"mà": true, "mai": true, "mày": true, "mình": true, "một": true, "này": true,
	"nữa": true, "nè": true, "nha": true, "nhé": true, "nhớ": true, "nếu": true,
	"ok": true, "okay": true, "ơi": true, "ôi": true, "quá": true, "rất": true,
	"rồi": true, "sẽ": true, "tao": true, "thật": true, "thì": true, "thôi": true,
	"tớ": true, "tuyệt": true, "và": true, "vậy": true, "với": true, "vừa": true,
	"vời": true, "wow": true, "uh": true, "um": true, "ừ": true,
}

func rewriteStringList(items []string, drop func(string) bool, fallback string) ([]string, bool) {
	if len(items) == 0 {
		return items, false
	}
	changed := false
	out := make([]string, 0, len(items))
	usedFallback := false
	for _, item := range items {
		next, ok := rewriteAdviceField(item, drop, "")
		if ok {
			changed = true
		}
		next = strings.TrimSpace(next)
		if next == "" {
			if fallback != "" && !usedFallback {
				out = append(out, fallback)
				usedFallback = true
				changed = true
			}
			continue
		}
		out = append(out, next)
	}
	if !changed {
		return items, false
	}
	return out, true
}

func splitSentences(s string) []string {
	var out []string
	var b strings.Builder
	for _, r := range s {
		b.WriteRune(r)
		if r == '.' || r == '!' || r == '?' || r == '…' {
			out = append(out, b.String())
			b.Reset()
		}
	}
	if strings.TrimSpace(b.String()) != "" {
		out = append(out, b.String())
	}
	return out
}

func ratingFromClimate(raw json.RawMessage) (rating, text string, ok bool) {
	if len(raw) == 0 {
		return "", "", false
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || len(m) == 0 {
		return "", "", false
	}
	for _, key := range []string{
		"skin_rating", "today_skin", "skin_feeling", "feeling", "mood",
		"self_score", "skin_score", "score",
	} {
		v, exists := m[key]
		if !exists || v == nil {
			continue
		}
		if rating, text, ok = classifyRatingValue(v); ok {
			return rating, text, true
		}
	}
	return "", "", false
}

func ratingFromNote(note string) (rating, text string, ok bool) {
	low := strings.ToLower(strings.TrimSpace(note))
	if low == "" {
		return "", "", false
	}
	for _, bad := range []string{"không ổn", "chưa ổn", "không tốt", "chưa tốt", "da đỏ", "viêm", "mụn", "ngứa", "kích ứng"} {
		if strings.Contains(low, bad) {
			return "", "", false
		}
	}
	for _, good := range []string{"rất tốt", "da tốt", "khá tốt"} {
		if strings.Contains(low, good) {
			return "good", good, true
		}
	}
	for _, fine := range []string{"da ổn", "khá ổn", "rất ổn", "ổn áp", "feeling fine", "feels fine", "skin is good", "skin looks good"} {
		if strings.Contains(low, fine) {
			if strings.Contains(fine, "good") {
				return "good", fine, true
			}
			return "fine", fine, true
		}
	}
	return "", "", false
}

func ratingFromTags(check *domain.SkinCheck) (rating, text string, ok bool) {
	conds, _ := dto.DecodeStringSlice(check.Conditions)
	syms, _ := dto.DecodeStringSlice(check.Symptoms)
	if len(conds) == 0 {
		return "", "", false
	}
	for _, c := range conds {
		if c != string(domain.SkinConditionNormal) {
			return "", "", false
		}
	}
	for _, s := range syms {
		switch domain.SkinSymptom(s) {
		case domain.SymptomItching, domain.SymptomStinging, domain.SymptomNewBreakouts:
			return "", "", false
		}
	}
	if pain := skinPain(check); pain == "itchy" || pain == "sore" {
		return "", "", false
	}
	return "fine", "normal", true
}

func skinPain(check *domain.SkinCheck) string {
	if check == nil || len(check.PhotoContext) == 0 {
		return ""
	}
	doc := dto.DecodePhotoContext(check.PhotoContext)
	if doc.SkinContext == nil {
		return ""
	}
	return doc.SkinContext.Pain
}

func classifyRatingValue(v any) (rating, text string, ok bool) {
	switch n := v.(type) {
	case string:
		return classifyRatingWord(n)
	case float64:
		return classifyNumericScore(n), formatScoreNumber(n), true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return "", "", false
		}
		return classifyNumericScore(f), formatScoreNumber(f), true
	default:
		return "", "", false
	}
}

func classifyRatingWord(raw string) (rating, text string, ok bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", " ")
	if s == "" {
		return "", "", false
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return classifyNumericScore(f), formatScoreNumber(f), true
	}
	switch s {
	case "good", "great", "excellent", "tot", "tốt", "rat tot", "rất tốt", "very good":
		return "good", strings.TrimSpace(raw), true
	case "fine", "ok", "okay", "on", "ổn", "binh thuong", "bình thường", "fair", "stable":
		return "fine", strings.TrimSpace(raw), true
	case "bad", "poor", "worse", "irritated", "uneasy", "te", "tệ", "khong on", "không ổn", "do", "đỏ", "inflamed":
		return "uneasy", strings.TrimSpace(raw), true
	default:
		return "", "", false
	}
}

// classifyNumericScore maps a self-rating onto fine / good / uneasy.
// Integers 1–5 are chips (1 tệ … 5 tốt). Other values in (0, 1] are a
// 0–1 gauge where 1 is best. 6–10 and 11–100 follow the same idea.
func classifyNumericScore(v float64) string {
	if math.IsNaN(v) || v < 0 {
		return "uneasy"
	}
	if v == math.Trunc(v) && v >= 1 && v <= 5 {
		switch {
		case v >= 4:
			return "good"
		case v >= 3:
			return "fine"
		default:
			return "uneasy"
		}
	}
	if v <= 1 {
		switch {
		case v >= 0.75:
			return "good"
		case v >= 0.6:
			return "fine"
		default:
			return "uneasy"
		}
	}
	if v <= 10 {
		switch {
		case v >= 8:
			return "good"
		case v >= 6:
			return "fine"
		default:
			return "uneasy"
		}
	}
	switch {
	case v >= 75:
		return "good"
	case v >= 60:
		return "fine"
	default:
		return "uneasy"
	}
}

func formatScoreNumber(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

func overallScore(a *domain.SkinAnalysis) (float64, bool) {
	if a == nil || len(a.SkinScores) == 0 || a.Status != domain.AnalysisStatusCompleted {
		return 0, false
	}
	var m map[string]any
	if err := json.Unmarshal(a.SkinScores, &m); err != nil {
		return 0, false
	}
	return floatFromAny(m["overall"])
}

func floatFromAny(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
