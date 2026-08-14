package ai

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

// public_share_title.go — titles for published skin reviews.
//
// Every /share/skin-review page used to fall back to one generic title string, so 150
// published pages shipped the identical <title> and OG title. That wastes the whole point
// of publishing them: the title tag is the strongest on-page signal, and each page is
// supposed to answer one specific long-tail question. Identical titles also make the
// Facebook shares look like spam.
//
// Priority: the operator's own title, then the user's question (the exact words people
// search), then a description built from the morphology group + region.

const maxShareTitleRunes = 70

// Polite tails and lead-ins Vietnamese questions usually carry; they add no search value.
var shareTitleLeadIns = []string{
	"cho em hỏi với ạ", "cho em hỏi ạ", "cho em hỏi", "cho mình hỏi", "cho hỏi",
	"mn cho hỏi", "mọi người cho hỏi", "ad ơi", "admin ơi", "mn ơi", "mọi người ơi",
	"bác sĩ ơi", "chị ơi", "anh ơi",
}

// Sorted longest-first by init so "em cảm ơn" is stripped before "cảm ơn" leaves a
// dangling "em" behind.
var shareTitleTails = []string{
	"em cảm ơn mọi người", "em cám ơn mọi người", "cảm ơn mọi người", "cám ơn mọi người",
	"em cảm ơn ạ", "em cám ơn ạ", "em cảm ơn", "em cám ơn",
	"cảm ơn ad", "cám ơn ad", "cảm ơn nhiều", "cám ơn nhiều", "cảm ơn ạ", "cám ơn ạ",
	"cảm ơn", "cám ơn", "thanks mn", "thanks", "hic hic", "huhu", "hix",
	"mong mn giúp", "mong được giúp", "giúp em với", "giúp mình với", "giúp với",
	"với ạ", "vậy ạ", "thế ạ", "ạ", "vậy", "nhé", "nha",
}

// Pronoun-only leftovers: a trailing ", em" carries no search value.
var shareTitleFillers = []string{"em", "mình", "tớ", "mn", "ad", "ạ", "e", "t"}

func init() {
	sort.SliceStable(shareTitleTails, func(i, j int) bool {
		return len(shareTitleTails[i]) > len(shareTitleTails[j])
	})
}

var shareRegionLabelsVI = map[string]string{
	"cheeks": "má", "cheek": "má", "forehead": "trán", "nose": "mũi", "chin": "cằm",
	"neck": "cổ", "t_zone": "trán–mũi–cằm", "jawline": "vùng hàm", "jaw": "vùng hàm",
	"under_eyes": "dưới mắt", "perioral": "quanh miệng", "temples": "thái dương",
	"other": "vùng da này",
}

var shareRegionLabelsEN = map[string]string{
	"cheeks": "cheeks", "cheek": "cheeks", "forehead": "forehead", "nose": "nose",
	"chin": "chin", "neck": "neck", "t_zone": "forehead, nose and chin",
	"jawline": "jawline", "jaw": "jawline", "under_eyes": "under-eye area",
	"perioral": "area around the mouth", "temples": "temples", "other": "this area",
}

// Fallback wording when no morphology group was recorded (older reviews).
var shareConcernLabelsVI = map[string]string{
	"acne": "mụn", "papules": "nốt đỏ sưng", "pustules": "mụn có mủ",
	"redness": "da đỏ", "irritation": "da kích ứng",
	"pigmentation": "thâm", "dark_spots": "đốm thâm",
	"pores": "lỗ chân lông to", "dryness": "da khô", "oiliness": "da bóng dầu",
	"texture": "da sần", "other": "nốt trên da",
}

// PublicShareTitle returns the title for a published review, or "" when nothing usable
// can be derived (the frontend then keeps its generic fallback).
func PublicShareTitle(a *dto.AdminSkinReviewAnalysis, userQuestion, locale string) string {
	if t := TitleFromUserQuestion(userQuestion, locale); t != "" {
		return t
	}
	return titleFromAnalysis(a, locale)
}

// TitleFromUserQuestion turns a real question into a title: their words are what other
// people type into Google.
func TitleFromUserQuestion(q, locale string) string {
	q = strings.Join(strings.Fields(q), " ")
	if q == "" {
		return ""
	}
	// Work on the first sentence only — questions often trail into life story.
	if i := strings.IndexAny(q, ".\n"); i > 12 {
		q = strings.TrimSpace(q[:i])
	}
	q = trimShareLeadIns(q)
	q = trimShareTails(q)
	q = expandShareShorthand(q)
	if q == "" {
		return ""
	}
	wasQuestion := strings.Contains(q, "?") ||
		looksLikeQuestion(strings.ToLower(q))
	q = strings.TrimRight(q, "?!., ")
	q = clipRunesAtWord(q, maxShareTitleRunes)
	if q == "" {
		return ""
	}
	q = upperFirst(q)
	if wasQuestion {
		q += "?"
	}
	return q
}

func titleFromAnalysis(a *dto.AdminSkinReviewAnalysis, locale string) string {
	if a == nil {
		return ""
	}
	en := strings.EqualFold(strings.TrimSpace(locale), "en")

	area, ok := adminPrimaryProblemArea(a)
	if !ok {
		return ""
	}
	regions := shareRegionLabelsVI
	if en {
		regions = shareRegionLabelsEN
	}
	region := regions[normLower(area.Region)]

	what := ""
	if g := MorphologyGroup(strings.TrimSpace(a.MorphologyGroup)); g != "" && g != GroupUnknown {
		what = MorphologyGroupLabel(g, locale)
	} else if !en {
		what = shareConcernLabelsVI[normLower(area.Concern)]
	}
	if what == "" {
		return ""
	}

	// Some group labels already name the region ("nếp gấp / nếp ngang cổ") — don't repeat it.
	if region == "" || strings.Contains(strings.ToLower(what), strings.ToLower(region)) {
		return upperFirst(what)
	}
	if en {
		return upperFirst(fmt.Sprintf("%s on the %s", what, region))
	}
	return upperFirst(fmt.Sprintf("%s ở %s", what, region))
}

func trimShareLeadIns(s string) string {
	for changed := true; changed; {
		changed = false
		low := strings.ToLower(s)
		for _, lead := range shareTitleLeadIns {
			if strings.HasPrefix(low, lead) {
				s = strings.TrimSpace(strings.TrimLeft(s[len(lead):], " ,:-"))
				changed = true
				break
			}
		}
	}
	return s
}

func trimShareTails(s string) string {
	// Drop whole trailing comma-segments that are only politeness ("…bị gì ạ, em cảm ơn").
	// Stripping phrases alone would leave a dangling ", em".
	parts := strings.Split(s, ",")
	for len(parts) > 1 {
		if !shareSegmentIsFiller(parts[len(parts)-1]) {
			break
		}
		parts = parts[:len(parts)-1]
	}
	s = strings.TrimSpace(strings.Join(parts, ","))

	for changed := true; changed; {
		changed = false
		trimmed := strings.TrimRight(s, " ,.!")
		low := strings.ToLower(trimmed)
		for _, tail := range shareTitleTails {
			// Only strip a whole trailing word/phrase, never part of a word.
			if !strings.HasSuffix(low, tail) {
				continue
			}
			cut := len(trimmed) - len(tail)
			if cut > 0 {
				prev := trimmed[cut-1]
				if prev != ' ' && prev != ',' && prev != '?' {
					continue
				}
			}
			s = strings.TrimSpace(strings.Trim(trimmed[:cut], " ,"))
			changed = true
			break
		}
	}
	return strings.TrimSpace(s)
}

// shareSegmentIsFiller reports a comma-segment that is only thanks/pronouns.
func shareSegmentIsFiller(seg string) bool {
	low := strings.ToLower(strings.TrimSpace(strings.Trim(seg, " .!?")))
	if low == "" {
		return true
	}
	for _, tail := range shareTitleTails {
		low = strings.TrimSpace(strings.ReplaceAll(low, tail, " "))
	}
	for _, w := range strings.Fields(low) {
		filler := false
		for _, f := range shareTitleFillers {
			if w == f {
				filler = true
				break
			}
		}
		if !filler {
			return false
		}
	}
	return true
}

// Whole-token chat shorthand people type on Facebook. Replaced only as a full
// word so "kem" does not become "khôngem".
var shareShorthand = map[string]string{
	"kb":   "không biết",
	"hqua": "hiệu quả",
	"đtri": "điều trị",
	"dtri": "điều trị",
	"ntn":  "như thế nào",
	"ko":   "không",
	"kg":   "không",
	"k":    "không",
	"đc":   "được",
	"dc":   "được",
	"trc":  "trước",
	"mn":   "mọi người",
	"tv":   "tư vấn",
}

func expandShareShorthand(s string) string {
	parts := strings.Fields(s)
	for i, w := range parts {
		core := strings.TrimRight(w, "?!.,:;…")
		if core == "" {
			continue
		}
		repl, ok := shareShorthand[strings.ToLower(core)]
		if !ok {
			continue
		}
		parts[i] = repl + w[len(core):]
	}
	return strings.Join(parts, " ")
}

func looksLikeQuestion(low string) bool {
	for _, w := range []string{
		"là gì", "bị gì", "sao", "thế nào", "như nào", "có phải", "làm gì", "làm sao",
		"tại sao", "vì sao", "bao lâu", "nên dùng", "có nên", "gì không", "không ạ",
		"what", "why", "how", "should i", "is this",
	} {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}

func clipRunesAtWord(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max])
	if i := strings.LastIndex(cut, " "); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(strings.TrimSpace(cut), " ,-–")
}

func upperFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return strings.ToUpper(string(r[0])) + string(r[1:])
}
