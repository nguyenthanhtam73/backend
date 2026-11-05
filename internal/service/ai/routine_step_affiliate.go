package ai

import (
	"net/url"
	"strings"
	"unicode"

	"github.com/dadiary/backend/internal/dto"
	"golang.org/x/text/unicode/norm"
)

// routineAffiliateStopwords are generic routine words. They never identify a
// catalog product on their own (a step titled "Kem chống nắng" must not pick
// one of the two SPF rows).
var routineAffiliateStopwords = map[string]struct{}{
	"and": {}, "the": {}, "for": {}, "with": {}, "plus": {}, "from": {}, "your": {},
	"sua": {}, "rua": {}, "mat": {}, "kem": {}, "gel": {}, "spf": {}, "uv": {},
	"sun": {}, "sang": {}, "toi": {}, "diu": {}, "nhe": {}, "duong": {}, "chong": {},
	"nang": {}, "tinh": {}, "chat": {}, "nuoc": {}, "hoa": {}, "hong": {},
	"toner": {}, "serum": {}, "cream": {}, "lotion": {}, "mask": {}, "face": {},
	"skin": {}, "gentle": {}, "light": {}, "water": {}, "advanced": {}, "days": {},
	"step": {}, "routine": {}, "other": {}, "cleanser": {}, "moisturizer": {},
	"sunscreen": {}, "treatment": {}, "moisturize": {}, "cleanse": {}, "treat": {},
	"soothe": {}, "buoi": {}, "dung": {}, "thoa": {}, "lop": {}, "mong": {},
	"acne": {}, "mun": {}, "glow": {}, "beauty": {}, "rich": {}, "premium": {},
	"master": {}, "daily": {}, "soft": {}, "care": {},
}

type affiliatePickStatus int

const (
	affiliatePickNone affiliatePickStatus = iota
	affiliatePickOne
	affiliatePickAmbiguous
)

// MatchRoutineStepAffiliate resolves one embedded-catalog product for a routine
// step. The link is the catalog affiliate_link and only when it is https.
// Category narrows which rows are eligible; it is never enough on its own.
func MatchRoutineStepAffiliate(category, title, notes string) (productID, productName, link string, ok bool) {
	rows, err := loadAffiliateCatalog()
	if err != nil || len(rows) == 0 {
		return "", "", "", false
	}
	raw := strings.TrimSpace(strings.TrimSpace(title) + " " + strings.TrimSpace(notes))
	text := foldMatch(raw)
	if text == "" {
		return "", "", "", false
	}
	cands := eligibleAffiliateRows(rows, category)
	if len(cands) == 0 {
		return "", "", "", false
	}
	if e, st := pickByProductName(cands, text); st != affiliatePickNone {
		if st != affiliatePickOne {
			return "", "", "", false
		}
		return emitAffiliate(e)
	}
	if e, st := pickByCatalogID(cands, text); st != affiliatePickNone {
		if st != affiliatePickOne {
			return "", "", "", false
		}
		return emitAffiliate(e)
	}
	if e, st := pickByBrand(cands, text, raw); st != affiliatePickNone {
		if st != affiliatePickOne {
			return "", "", "", false
		}
		return emitAffiliate(e)
	}
	if e, st := pickByTokens(cands, raw); st == affiliatePickOne {
		return emitAffiliate(e)
	}
	return "", "", "", false
}

// AttachRoutineStepAffiliates sets affiliate fields from the catalog.
// Client-supplied values on the input steps are discarded.
func AttachRoutineStepAffiliates(steps []dto.RoutineStep) []dto.RoutineStep {
	if len(steps) == 0 {
		return steps
	}
	out := make([]dto.RoutineStep, len(steps))
	for i, s := range steps {
		s.AffiliateLink = nil
		s.AffiliateProductID = nil
		s.ProductName = nil
		if id, name, link, ok := MatchRoutineStepAffiliate(s.Category, s.Title, s.Notes); ok {
			s.AffiliateProductID = stringPtr(id)
			s.ProductName = stringPtr(name)
			s.AffiliateLink = stringPtr(link)
		}
		out[i] = s
	}
	return out
}

func stringPtr(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

func emitAffiliate(e affiliateCatalogEntry) (string, string, string, bool) {
	link, ok := catalogHTTPSLink(e.AffiliateLink)
	if !ok || strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.ProductName) == "" {
		return "", "", "", false
	}
	return strings.TrimSpace(e.ID), strings.TrimSpace(e.ProductName), link, true
}

func eligibleAffiliateRows(rows []affiliateCatalogEntry, category string) []affiliateCatalogEntry {
	out := make([]affiliateCatalogEntry, 0, len(rows))
	for _, r := range rows {
		if _, ok := catalogHTTPSLink(r.AffiliateLink); !ok {
			continue
		}
		if strings.TrimSpace(r.ID) == "" || strings.TrimSpace(r.ProductName) == "" {
			continue
		}
		if !acceptsRoutineCategory(category, r) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// acceptsRoutineCategory reports whether a catalog row may be attached to a
// step of this category. Empty, "other", and unknown labels do not filter.
// mask and eye have no catalog product.
func acceptsRoutineCategory(category string, e affiliateCatalogEntry) bool {
	switch normLower(category) {
	case "", "other":
		return true
	case "cleanser", "cleanse":
		return normLower(e.Category) == "cleanser" || catalogStep(e) == "cleanse"
	case "moisturizer", "moisturize", "moisturiser":
		return normLower(e.Category) == "moisturizer" || catalogStep(e) == "moisturize"
	case "spf", "sunscreen":
		return normLower(e.Category) == "spf" || catalogStep(e) == "spf"
	case "toner":
		return normLower(e.Category) == "toner"
	case "serum", "essence", "ampoule":
		return normLower(e.Category) == "serum"
	case "treatment", "treat", "active":
		return normLower(e.Category) == "treatment" || catalogStep(e) == "treat"
	case "mask", "eye":
		return false
	default:
		return true
	}
}

func pickByProductName(cands []affiliateCatalogEntry, text string) (affiliateCatalogEntry, affiliatePickStatus) {
	var hits []affiliateCatalogEntry
	for _, e := range cands {
		name := foldMatch(e.ProductName)
		if name == "" {
			continue
		}
		if containsPhrase(text, name) {
			hits = append(hits, e)
		}
	}
	return pickLongestContainedName(hits)
}

func pickLongestContainedName(hits []affiliateCatalogEntry) (affiliateCatalogEntry, affiliatePickStatus) {
	if len(hits) == 0 {
		return affiliateCatalogEntry{}, affiliatePickNone
	}
	if len(hits) == 1 {
		return hits[0], affiliatePickOne
	}
	best := hits[0]
	bestName := foldMatch(best.ProductName)
	for _, h := range hits[1:] {
		n := foldMatch(h.ProductName)
		if len(n) > len(bestName) {
			best = h
			bestName = n
		}
	}
	for _, h := range hits {
		if h.ID == best.ID {
			continue
		}
		if !containsPhrase(bestName, foldMatch(h.ProductName)) {
			return affiliateCatalogEntry{}, affiliatePickAmbiguous
		}
	}
	return best, affiliatePickOne
}

func pickByCatalogID(cands []affiliateCatalogEntry, text string) (affiliateCatalogEntry, affiliatePickStatus) {
	var hits []affiliateCatalogEntry
	for _, e := range cands {
		id := foldMatch(e.ID)
		if len(strings.ReplaceAll(id, " ", "")) < 3 {
			continue
		}
		if containsPhrase(text, id) {
			hits = append(hits, e)
		}
	}
	switch len(hits) {
	case 0:
		return affiliateCatalogEntry{}, affiliatePickNone
	case 1:
		return hits[0], affiliatePickOne
	default:
		return affiliateCatalogEntry{}, affiliatePickAmbiguous
	}
}

func pickByBrand(cands []affiliateCatalogEntry, text, raw string) (affiliateCatalogEntry, affiliatePickStatus) {
	var hits []affiliateCatalogEntry
	for _, e := range cands {
		brand := foldMatch(e.Brand)
		if len(strings.ReplaceAll(brand, " ", "")) < 3 {
			continue
		}
		if containsPhrase(text, brand) {
			hits = append(hits, e)
		}
	}
	switch len(hits) {
	case 0:
		return affiliateCatalogEntry{}, affiliatePickNone
	case 1:
		return hits[0], affiliatePickOne
	default:
		e, st := pickByTokens(hits, raw)
		if st == affiliatePickOne {
			return e, affiliatePickOne
		}
		return affiliateCatalogEntry{}, affiliatePickAmbiguous
	}
}

func pickByTokens(cands []affiliateCatalogEntry, raw string) (affiliateCatalogEntry, affiliatePickStatus) {
	toks := tokensFrom(raw)
	if len(toks) == 0 || len(cands) == 0 {
		return affiliateCatalogEntry{}, affiliatePickNone
	}
	sets := make([]map[string]struct{}, len(cands))
	for i := range cands {
		sets[i] = entryTokenSet(cands[i])
	}
	found := make(map[string]affiliateCatalogEntry)
	for _, tok := range toks {
		var owners []affiliateCatalogEntry
		for i := range cands {
			if _, ok := sets[i][tok]; ok {
				owners = append(owners, cands[i])
			}
		}
		if len(owners) != 1 {
			continue
		}
		found[owners[0].ID] = owners[0]
	}
	if len(found) == 0 {
		return affiliateCatalogEntry{}, affiliatePickNone
	}
	if len(found) > 1 {
		return affiliateCatalogEntry{}, affiliatePickAmbiguous
	}
	for _, e := range found {
		return e, affiliatePickOne
	}
	return affiliateCatalogEntry{}, affiliatePickNone
}

func entryTokenSet(e affiliateCatalogEntry) map[string]struct{} {
	set := make(map[string]struct{})
	for _, t := range tokensFrom(e.ProductName) {
		set[t] = struct{}{}
	}
	for _, t := range tokensFrom(e.Brand) {
		set[t] = struct{}{}
	}
	for _, t := range tokensFromID(e.ID) {
		set[t] = struct{}{}
	}
	return set
}

// tokensFrom keeps distinctive words and 3-letter acronyms (BHA, AHA).
// Generic step words are dropped via routineAffiliateStopwords.
func tokensFrom(raw string) []string {
	acronyms := map[string]struct{}{}
	for _, a := range findAcronyms(raw) {
		acronyms[strings.ToLower(a)] = struct{}{}
	}
	var out []string
	seen := map[string]struct{}{}
	for _, tok := range strings.Fields(foldMatch(raw)) {
		if _, ok := seen[tok]; ok {
			continue
		}
		if _, stop := routineAffiliateStopwords[tok]; stop || isAllDigits(tok) {
			continue
		}
		_, acro := acronyms[tok]
		if len(tok) < 4 && !acro {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

// tokensFromID keeps short id fragments such as "boj" and "lrp".
func tokensFromID(id string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, tok := range strings.Fields(foldMatch(id)) {
		if len(tok) < 3 || isAllDigits(tok) {
			continue
		}
		if _, stop := routineAffiliateStopwords[tok]; stop {
			continue
		}
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	return out
}

func findAcronyms(raw string) []string {
	runes := []rune(raw)
	var out []string
	for i := 0; i < len(runes); {
		if !isUpperASCII(runes[i]) {
			i++
			continue
		}
		j := i
		for j < len(runes) && isUpperASCII(runes[j]) {
			j++
		}
		if j-i == 3 {
			prevOK := i == 0 || !unicode.IsLetter(runes[i-1])
			nextOK := j == len(runes) || !unicode.IsLetter(runes[j])
			if prevOK && nextOK {
				out = append(out, string(runes[i:j]))
			}
		}
		i = j
	}
	return out
}

func isUpperASCII(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func containsPhrase(text, phrase string) bool {
	phrase = strings.TrimSpace(phrase)
	if phrase == "" || text == "" {
		return false
	}
	return strings.Contains(" "+text+" ", " "+phrase+" ")
}

func foldMatch(s string) string {
	s = strings.ReplaceAll(s, "đ", "d")
	s = strings.ReplaceAll(s, "Đ", "d")
	s = norm.NFD.String(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		r = unicode.ToLower(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteByte(' ')
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// catalogHTTPSLink returns the catalog URL unchanged when its scheme is https.
func catalogHTTPSLink(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return "", false
	}
	return raw, true
}
