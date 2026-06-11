package ai

import "strings"

var friendlySkinTypeVI = map[string]string{
	"dry": "da khô", "oily": "da dầu", "combo": "da hỗn hợp", "combination": "da hỗn hợp",
	"normal": "da thường", "sensitive": "da nhạy cảm", "prefer_not": "chưa rõ",
}

var friendlySkinTypeEN = map[string]string{
	"dry": "dry skin", "oily": "oily skin", "combo": "combination skin", "combination": "combination skin",
	"normal": "normal skin", "sensitive": "sensitive skin", "prefer_not": "unclear",
}

var friendlyUndertoneVI = map[string]string{
	"warm": "tone ấm", "cool": "tone lạnh", "neutral": "tone trung tính",
	"deep": "da sẫm", "fair": "da sáng", "prefer_not": "chưa rõ",
}

var friendlyUndertoneEN = map[string]string{
	"warm": "warm undertone", "cool": "cool undertone", "neutral": "neutral undertone",
	"deep": "deeper skin tone", "fair": "fair skin tone", "prefer_not": "not specified",
}

var friendlyConcernVI = map[string]string{
	"acne": "mụn", "hyperpigmentation": "thâm / sạm màu", "dryness": "da khô",
	"redness": "da đỏ / dễ kích ứng", "large_pores": "lỗ chân lông to",
	"weak_barrier": "lớp bảo vệ da yếu", "dullness": "da xỉn màu",
	"dehydration": "da khô bên trong", "uneven_texture": "bề mặt da không đều",
}

var friendlyConcernEN = map[string]string{
	"acne": "breakouts", "hyperpigmentation": "dark spots", "dryness": "dryness",
	"redness": "redness", "large_pores": "visible pores", "weak_barrier": "a weakened skin barrier",
	"dullness": "dullness", "dehydration": "dehydration", "uneven_texture": "uneven texture",
}

func friendlySkinType(raw, locale string) string {
	key := normLower(raw)
	if strings.EqualFold(strings.TrimSpace(locale), "en") {
		if s, ok := friendlySkinTypeEN[key]; ok {
			return s
		}
	} else if s, ok := friendlySkinTypeVI[key]; ok {
		return s
	}
	if key == "" {
		return ternaryLocale(locale, "unclear", "chưa rõ")
	}
	return strings.TrimSpace(raw)
}

func friendlyUndertone(raw, locale string) string {
	key := normLower(raw)
	if strings.EqualFold(strings.TrimSpace(locale), "en") {
		if s, ok := friendlyUndertoneEN[key]; ok {
			return s
		}
	} else if s, ok := friendlyUndertoneVI[key]; ok {
		return s
	}
	if key == "" || key == "unknown" {
		return ternaryLocale(locale, "not clear from the photo", "chưa rõ từ ảnh")
	}
	return strings.TrimSpace(raw)
}

func friendlyConcern(raw, locale string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ternaryLocale(locale, "your main skin worry", "vấn đề da chính")
	}
	key := normLower(raw)
	if strings.EqualFold(strings.TrimSpace(locale), "en") {
		if s, ok := friendlyConcernEN[key]; ok {
			return s
		}
	} else if s, ok := friendlyConcernVI[key]; ok {
		return s
	}
	// Already a human label from vision (e.g. "mụn viêm").
	return raw
}

func ternaryLocale(locale, en, vi string) string {
	if strings.EqualFold(strings.TrimSpace(locale), "en") {
		return en
	}
	return vi
}
