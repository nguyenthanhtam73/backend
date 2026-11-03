package lobby

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// exactWords match a whole folded token, or the nickname once separators
// and leetspeak are removed ("d.i.t", "l0n"). Short words are not substrings,
// so "addition" and "salon" stay usable.
var exactWords = []string{
	"dit", "du", "lon", "loz", "cac", "buoi",
	"dm", "dcm", "dmm", "dkm", "vcl", "vkl", "vl", "clgt", "cmm", "cmn", "cmnr",
	"deo", "dech",
	"fuck", "fck", "fuk", "fock", "phuck", "fuq",
	"shit", "sht", "bitch", "cunt", "dick", "cock", "pussy", "slut", "whore",
	"nigger", "nigga", "fag", "faggot", "asshole", "bastard", "retard",
	"ngu", "vailon", "ditme", "duma",
}

// phraseWords match two neighboring folded tokens ("đụ má" → "du ma").
var phraseWords = []string{
	"du ma", "du me", "dit me", "dit bo", "dit con",
	"me may", "con cho", "thang cho", "do cho",
	"cai lon", "con cac", "thang ngu", "vai lon",
}

// substringWords match inside the fully collapsed nickname. They are long
// enough that ordinary words ("classic", "cocktail", "peacock") do not hit.
var substringWords = []string{
	"fuck", "fck", "fuk", "fock", "phuck", "fuq",
	"shit", "sht", "bitch", "cunt", "pussy", "slut", "whore",
	"nigger", "nigga", "faggot", "asshole", "bastard", "retard",
	"motherfucker", "dickhead", "bullshit", "cocksucker",
	"ditme", "ditbo", "ditcon", "ditmemay",
	"duma", "dume", "ducho", "vailon", "concac", "concak",
	"thangcho", "doccho", "clgt", "dcm", "dmm", "vcl", "vkl",
}

var (
	exactSet   map[string]struct{}
	phraseSet  map[string]struct{}
	substrings []string
)

func init() {
	exactSet = make(map[string]struct{}, len(exactWords))
	for _, w := range exactWords {
		exactSet[collapseRunes(w)] = struct{}{}
	}
	phraseSet = make(map[string]struct{}, len(phraseWords))
	for _, p := range phraseWords {
		parts := strings.Fields(p)
		if len(parts) != 2 {
			continue
		}
		phraseSet[collapseRunes(parts[0])+" "+collapseRunes(parts[1])] = struct{}{}
	}
	substrings = make([]string, 0, len(substringWords))
	for _, w := range substringWords {
		if c := collapseRunes(w); c != "" {
			substrings = append(substrings, c)
		}
	}
}

// Profane reports whether s contains a blocked Vietnamese or English word
// after diacritics, leetspeak, and separator stripping.
func Profane(s string) bool {
	tokens, flat := foldNickname(s)
	if flat == "" {
		return false
	}
	if _, ok := exactSet[flat]; ok {
		return true
	}
	for _, tok := range tokens {
		if _, ok := exactSet[tok]; ok {
			return true
		}
	}
	for i := 0; i+1 < len(tokens); i++ {
		if _, ok := phraseSet[tokens[i]+" "+tokens[i+1]]; ok {
			return true
		}
	}
	for _, w := range substrings {
		if strings.Contains(flat, w) {
			return true
		}
	}
	return false
}

// CleanNickname trims a display nickname. ok is false when it is empty,
// too long, has a disallowed character, or is profane. The rejected text is
// not returned to logs.
func CleanNickname(raw string) (string, bool) {
	s := stripInvisible(raw)
	s = strings.ReplaceAll(s, "\u2019", "'")
	s = strings.TrimSpace(s)
	s = collapseSpaces(s)
	if s == "" || utf8.RuneCountInString(s) > MaxNicknameRunes {
		return "", false
	}
	alnum := false
	for _, r := range s {
		if !allowedNickRune(r) {
			return "", false
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			alnum = true
		}
	}
	if !alnum || Profane(s) {
		return "", false
	}
	return s, true
}

func allowedNickRune(r rune) bool {
	switch r {
	case ' ', '\'', '-', '.':
		return true
	default:
		return unicode.IsLetter(r) || unicode.IsNumber(r)
	}
}

func collapseSpaces(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if r == ' ' {
			if prevSpace {
				continue
			}
			prevSpace = true
			b.WriteByte(' ')
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return b.String()
}

func stripInvisible(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\ufeff', '\u2060', '\u180e',
			'\u202a', '\u202b', '\u202c', '\u202d', '\u202e',
			'\u2066', '\u2067', '\u2068', '\u2069':
			return -1
		default:
			return r
		}
	}, s)
}

// foldNickname returns collapsed tokens and the collapsed letters-only form.
func foldNickname(s string) (tokens []string, flat string) {
	s = norm.NFKC.String(s)
	s = strings.ToLower(s)
	s = stripMarks(norm.NFD.String(s))

	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		tokens = append(tokens, collapseRunes(cur.String()))
		cur.Reset()
	}
	for _, r := range s {
		if mapped, ok := foldRune(r); ok {
			cur.WriteRune(mapped)
			continue
		}
		flush()
	}
	flush()
	flat = collapseRunes(strings.Join(tokens, ""))
	return tokens, flat
}

func stripMarks(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func foldRune(r rune) (rune, bool) {
	if r == 'đ' {
		return 'd', true
	}
	switch r {
	case '0', 'º', '°':
		return 'o', true
	case '1', '!', '|', '¡':
		return 'i', true
	case '3':
		return 'e', true
	case '4', '@', 'ª':
		return 'a', true
	case '5', '$':
		return 's', true
	case '7', '+':
		return 't', true
	case '8':
		return 'b', true
	case '9':
		return 'g', true
	}
	// Cyrillic lookalikes that survive lowercasing.
	switch r {
	case 'а':
		return 'a', true
	case 'е':
		return 'e', true
	case 'о':
		return 'o', true
	case 'р':
		return 'p', true
	case 'с':
		return 'c', true
	case 'у':
		return 'y', true
	case 'х':
		return 'x', true
	case 'і':
		return 'i', true
	case 'ѕ':
		return 's', true
	case 'ј':
		return 'j', true
	}
	if unicode.IsLetter(r) {
		return r, true
	}
	return 0, false
}

func collapseRunes(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	var prev rune
	first := true
	for _, r := range s {
		if !first && r == prev {
			continue
		}
		first = false
		prev = r
		b.WriteRune(r)
	}
	return b.String()
}
