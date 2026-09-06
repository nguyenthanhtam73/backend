package ai

// coach_knowledge.go — curated PUBLIC-source knowledge for the daily Coach.
//
// Edit coach_knowledge.json (not this file) when adding a theme or updating a
// citation. The pack is injected into the Coach user message when today's
// note / profile / vision text matches a theme. It is never built from user
// diaries, Facebook, skin-reviews, or selling data.
//
// Sources (see JSON): AAD public pages, DermNet, NCBI StatPearls, DailyMed.
// Claims stay at triage + uncertainty + when to see a doctor.

import (
	_ "embed"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

//go:embed coach_knowledge.json
var coachKnowledgeJSON []byte

// AllowedCoachKnowledgeHosts is the allow-list for pack citations.
// Tests fail if a theme cites anything else.
var AllowedCoachKnowledgeHosts = []string{
	"www.aad.org",
	"aad.org",
	"dermnetnz.org",
	"www.dermnetnz.org",
	"www.ncbi.nlm.nih.gov",
	"ncbi.nlm.nih.gov",
	"dailymed.nlm.nih.gov",
}

const coachKnowledgeBlockHeader = `## COACH_KNOWLEDGE (nội bộ — nguồn công khai)
Chỉ dùng khi khớp theme bên dưới. Không chẩn đoán. Không đọc URL cho user. Không bịa claim ngoài block này. Giọng vẫn bạn thân / tiếng Việt đời thường.`

// CoachPublicKnowledgeGuard is the always-on system-prompt pointer so the
// daily Coach follows the pack when the user message includes COACH_KNOWLEDGE.
func CoachPublicKnowledgeGuard() string {
	return `## Kiến thức công khai (khi có COACH_KNOWLEDGE)
Nếu user message có ## COACH_KNOWLEDGE — bám đúng hướng chăm sóc ở đó.
Không chẩn đoán bệnh danh. Không hứa hết thâm/nám. Không khuyên nặn / lấy nhân tại nhà.
- Kích ứng sau adapalene / BHA / retinoid: dịu + dưỡng ẩm + chống nắng; giảm tần suất hoặc tạm nghỉ chất mạnh; sưng phù / rát nặng → bảo khám.
- Thâm vs nám: nói dấu thường gặp, không chốt nám từ ảnh; chống nắng; khám nếu lo / loang rộng / liên quan thai–hormone.
- Da dầu / mụn ẩn: BHA chậm khi da yên; đang đỏ thì dịu trước; CẤM nặn.
Nguồn là trang da liễu công khai (AAD, DermNet, monograph) — không phải nhật ký user.`
}

type coachKnowledgePack struct {
	ID            string                `json:"id"`
	Version       int                   `json:"version"`
	Updated       string                `json:"updated"`
	Scope         string                `json:"scope"`
	GlobalRulesVI []string              `json:"global_rules_vi"`
	Themes        []coachKnowledgeTheme `json:"themes"`
}

type coachKnowledgeTheme struct {
	ID          string                 `json:"id"`
	TitleVI     string                 `json:"title_vi"`
	SummaryVI   string                 `json:"summary_vi"`
	DoVI        []string               `json:"do_vi"`
	DontVI      []string               `json:"dont_vi"`
	SeeDoctorVI []string               `json:"see_doctor_vi"`
	MatchAny    []string               `json:"match_any"`
	MatchPairs  []coachKnowledgePair   `json:"match_pairs"`
	Sources     []coachKnowledgeSource `json:"sources"`
}

type coachKnowledgePair struct {
	Any    []string `json:"any"`
	AndAny []string `json:"and_any"`
}

type coachKnowledgeSource struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Accessed string `json:"accessed"`
}

var (
	coachKnowledgeOnce sync.Once
	coachKnowledgeData *coachKnowledgePack
	coachKnowledgeErr  error
)

// loadCoachKnowledgePack parses the embedded JSON once.
func loadCoachKnowledgePack() (*coachKnowledgePack, error) {
	coachKnowledgeOnce.Do(func() {
		var pack coachKnowledgePack
		if err := json.Unmarshal(coachKnowledgeJSON, &pack); err != nil {
			coachKnowledgeErr = err
			return
		}
		coachKnowledgeData = &pack
	})
	return coachKnowledgeData, coachKnowledgeErr
}

// CoachKnowledgeThemeIDs is the stable set the daily Coach is wired for.
var CoachKnowledgeThemeIDs = []string{
	"irritation_after_adapalene_bha",
	"post_acne_marks_vs_melasma",
	"oily_closed_comedones",
}

// MatchCoachKnowledgeThemes returns theme IDs whose keywords hit haystack
// (user note, profile, vision JSON). Order follows the pack file.
func MatchCoachKnowledgeThemes(haystack string) []string {
	pack, err := loadCoachKnowledgePack()
	if err != nil || pack == nil {
		return nil
	}
	text := strings.TrimSpace(haystack)
	if text == "" {
		return nil
	}
	out := make([]string, 0, len(pack.Themes))
	for _, theme := range pack.Themes {
		if themeMatchesHaystack(theme, text) {
			out = append(out, theme.ID)
		}
	}
	return out
}

func themeMatchesHaystack(theme coachKnowledgeTheme, haystack string) bool {
	for _, tok := range theme.MatchAny {
		if knowledgeContainsToken(haystack, tok) {
			return true
		}
	}
	for _, pair := range theme.MatchPairs {
		if knowledgePairHits(haystack, pair) {
			return true
		}
	}
	return false
}

func knowledgePairHits(haystack string, pair coachKnowledgePair) bool {
	hitAny := false
	for _, tok := range pair.Any {
		if knowledgeContainsToken(haystack, tok) {
			hitAny = true
			break
		}
	}
	if !hitAny {
		return false
	}
	for _, tok := range pair.AndAny {
		if knowledgeContainsToken(haystack, tok) {
			return true
		}
	}
	return false
}

// genericFoldedTokens must not match after diacritic folding — "nám" → "nam"
// would otherwise hit "nam giới" / place names.
var genericFoldedTokens = map[string]bool{
	"nam": true,
}

func knowledgeContainsToken(haystack, token string) bool {
	rawT := strings.ToLower(strings.TrimSpace(token))
	if rawT == "" {
		return false
	}
	rawH := strings.ToLower(haystack)
	if knowledgeTokenHit(rawH, rawT) {
		return true
	}
	foldedT := foldKnowledgeText(rawT)
	if foldedT == rawT || genericFoldedTokens[foldedT] {
		return false
	}
	return knowledgeTokenHit(foldKnowledgeText(rawH), foldedT)
}

func knowledgeTokenHit(haystack, token string) bool {
	if token == "" {
		return false
	}
	if utf8.RuneCountInString(token) <= 3 && isASCIILetters(token) {
		return hasKnowledgeWord(haystack, token)
	}
	return strings.Contains(haystack, token)
}

func isASCIILetters(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

func hasKnowledgeWord(haystack, word string) bool {
	start := 0
	for {
		i := strings.Index(haystack[start:], word)
		if i < 0 {
			return false
		}
		i += start
		leftOK := i == 0 || !knowledgeWordRune(rune(haystack[i-1]))
		right := i + len(word)
		rightOK := right == len(haystack) || !knowledgeWordRune(rune(haystack[right]))
		if leftOK && rightOK {
			return true
		}
		start = i + 1
		if start >= len(haystack) {
			return false
		}
	}
}

func knowledgeWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func foldKnowledgeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if repl, ok := knowledgeFold[r]; ok {
			b.WriteRune(repl)
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

var knowledgeFold = map[rune]rune{
	'à': 'a', 'á': 'a', 'ạ': 'a', 'ả': 'a', 'ã': 'a',
	'â': 'a', 'ầ': 'a', 'ấ': 'a', 'ậ': 'a', 'ẩ': 'a', 'ẫ': 'a',
	'ă': 'a', 'ằ': 'a', 'ắ': 'a', 'ặ': 'a', 'ẳ': 'a', 'ẵ': 'a',
	'è': 'e', 'é': 'e', 'ẹ': 'e', 'ẻ': 'e', 'ẽ': 'e',
	'ê': 'e', 'ề': 'e', 'ế': 'e', 'ệ': 'e', 'ể': 'e', 'ễ': 'e',
	'ì': 'i', 'í': 'i', 'ị': 'i', 'ỉ': 'i', 'ĩ': 'i',
	'ò': 'o', 'ó': 'o', 'ọ': 'o', 'ỏ': 'o', 'õ': 'o',
	'ô': 'o', 'ồ': 'o', 'ố': 'o', 'ộ': 'o', 'ổ': 'o', 'ỗ': 'o',
	'ơ': 'o', 'ờ': 'o', 'ớ': 'o', 'ợ': 'o', 'ở': 'o', 'ỡ': 'o',
	'ù': 'u', 'ú': 'u', 'ụ': 'u', 'ủ': 'u', 'ũ': 'u',
	'ư': 'u', 'ừ': 'u', 'ứ': 'u', 'ự': 'u', 'ử': 'u', 'ữ': 'u',
	'ỳ': 'y', 'ý': 'y', 'ỵ': 'y', 'ỷ': 'y', 'ỹ': 'y',
	'đ': 'd',
	'À': 'a', 'Á': 'a', 'Ạ': 'a', 'Ả': 'a', 'Ã': 'a',
	'Â': 'a', 'Ầ': 'a', 'Ấ': 'a', 'Ậ': 'a', 'Ẩ': 'a', 'Ẫ': 'a',
	'Ă': 'a', 'Ằ': 'a', 'Ắ': 'a', 'Ặ': 'a', 'Ẳ': 'a', 'Ẵ': 'a',
	'È': 'e', 'É': 'e', 'Ẹ': 'e', 'Ẻ': 'e', 'Ẽ': 'e',
	'Ê': 'e', 'Ề': 'e', 'Ế': 'e', 'Ệ': 'e', 'Ể': 'e', 'Ễ': 'e',
	'Ì': 'i', 'Í': 'i', 'Ị': 'i', 'Ỉ': 'i', 'Ĩ': 'i',
	'Ò': 'o', 'Ó': 'o', 'Ọ': 'o', 'Ỏ': 'o', 'Õ': 'o',
	'Ô': 'o', 'Ồ': 'o', 'Ố': 'o', 'Ộ': 'o', 'Ổ': 'o', 'Ỗ': 'o',
	'Ơ': 'o', 'Ờ': 'o', 'Ớ': 'o', 'Ợ': 'o', 'Ở': 'o', 'Ỡ': 'o',
	'Ù': 'u', 'Ú': 'u', 'Ụ': 'u', 'Ủ': 'u', 'Ũ': 'u',
	'Ư': 'u', 'Ừ': 'u', 'Ứ': 'u', 'Ự': 'u', 'Ử': 'u', 'Ữ': 'u',
	'Ỳ': 'y', 'Ý': 'y', 'Ỵ': 'y', 'Ỷ': 'y', 'Ỹ': 'y',
	'Đ': 'd',
}

// RenderCoachKnowledgeBlock builds the user-message section for matching themes.
// Empty haystack or no match → "".
func RenderCoachKnowledgeBlock(haystack string) string {
	ids := MatchCoachKnowledgeThemes(haystack)
	if len(ids) == 0 {
		return ""
	}
	pack, err := loadCoachKnowledgePack()
	if err != nil || pack == nil {
		return ""
	}
	byID := make(map[string]coachKnowledgeTheme, len(pack.Themes))
	for _, th := range pack.Themes {
		byID[th.ID] = th
	}

	var b strings.Builder
	b.WriteString(coachKnowledgeBlockHeader)
	b.WriteString("\n")
	for _, rule := range pack.GlobalRulesVI {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(rule)
		b.WriteString("\n")
	}
	for _, id := range ids {
		th, ok := byID[id]
		if !ok {
			continue
		}
		b.WriteString("\n### ")
		b.WriteString(th.ID)
		b.WriteString(" — ")
		b.WriteString(th.TitleVI)
		b.WriteString("\n")
		if s := strings.TrimSpace(th.SummaryVI); s != "" {
			b.WriteString(s)
			b.WriteString("\n")
		}
		writeKnowledgeList(&b, "Làm", th.DoVI)
		writeKnowledgeList(&b, "Không", th.DontVI)
		writeKnowledgeList(&b, "Đi khám khi", th.SeeDoctorVI)
		if names := sourceShortNames(th.Sources); names != "" {
			b.WriteString("Nguồn (nội bộ, đừng đọc cho user): ")
			b.WriteString(names)
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeKnowledgeList(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString(title)
	b.WriteString(":\n")
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(item)
		b.WriteString("\n")
	}
}

func sourceShortNames(sources []coachKnowledgeSource) string {
	seen := make(map[string]bool, len(sources))
	var parts []string
	for _, s := range sources {
		name := strings.TrimSpace(s.Name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		parts = append(parts, name)
	}
	return strings.Join(parts, "; ")
}

// AppendCoachKnowledgeContext injects matching public-source notes into a
// Coach user message. No-op when nothing matches (keeps tokens small).
func AppendCoachKnowledgeContext(b *strings.Builder, haystack string) {
	if b == nil {
		return
	}
	block := RenderCoachKnowledgeBlock(haystack)
	if block == "" {
		return
	}
	b.WriteString("\n\n")
	b.WriteString(block)
}

func coachKnowledgeSourceHostAllowed(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Host)
	for _, allowed := range AllowedCoachKnowledgeHosts {
		if host == allowed {
			return true
		}
	}
	return false
}
