package ai

import (
	"regexp"
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

var (
	reMaPhaiVI = regexp.MustCompile(`(?i)má\s+phải`)
	reMaTraiVI = regexp.MustCompile(`(?i)má\s+trái`)
	reRightCheekEN = regexp.MustCompile(`(?i)\bright\s+cheek\b`)
	reLeftCheekEN  = regexp.MustCompile(`(?i)\bleft\s+cheek\b`)
)

// SoftenCheekLateralityProse replaces guessed left/right cheek labels with a safer phrase.
func SoftenCheekLateralityProse(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	out := reMaPhaiVI.ReplaceAllString(s, "má gần tai")
	out = reMaTraiVI.ReplaceAllString(out, "má gần tai")
	out = reRightCheekEN.ReplaceAllString(out, "cheek near the ear")
	out = reLeftCheekEN.ReplaceAllString(out, "cheek near the ear")
	out = strings.ReplaceAll(out, "má gần tai gần tai", "má gần tai")
	out = strings.ReplaceAll(out, "cheek near the ear near the ear", "cheek near the ear")
	return out
}

func adminSkinLooksCloseUpCheek(a *dto.AdminSkinReviewAnalysis) bool {
	if a == nil {
		return false
	}
	blob := strings.ToLower(strings.TrimSpace(a.PhotoNotes + " " + a.Overview + " " + a.SkinTypeNote))
	needles := []string{
		"close-up", "closeup", "close up",
		"chỉ thấy má", "chỉ xét được má", "thiếu trán", "crop",
		"má gần tai", "chỉ nửa mặt", "chỉ một vùng",
	}
	for _, n := range needles {
		if strings.Contains(blob, n) {
			return true
		}
	}
	// Single visible region = cheeks, others not_visible → treat as cheek close-up.
	visibleCheek := false
	otherVisible := false
	for _, ar := range a.AttentionAreas {
		c := strings.ToLower(strings.TrimSpace(ar.Concern))
		r := strings.ToLower(strings.TrimSpace(ar.Region))
		if c == "" || c == "not_visible" {
			continue
		}
		if r == "cheeks" || r == "cheek" {
			visibleCheek = true
		} else {
			otherVisible = true
		}
	}
	return visibleCheek && !otherVisible
}

func questionImpliesSpotProduct(q string) bool {
	ql := strings.ToLower(q)
	needles := []string{
		"chấm mụn", "bôi chấm", "kem chấm", "spot treatment", "spot cream",
		"đang bôi", "mới bôi", "azelaic", "benzoyl", "bha", "aha", "retinol", "retinoid",
	}
	for _, n := range needles {
		if strings.Contains(ql, n) {
			return true
		}
	}
	return false
}

func questionAsksWrongStep(q string) bool {
	ql := strings.ToLower(q)
	needles := []string{"sai bước", "sai ở bước", "which step", "what step", "doing wrong", "làm sai"}
	for _, n := range needles {
		if strings.Contains(ql, n) {
			return true
		}
	}
	return false
}

func questionClaimsOily(q string) bool {
	ql := strings.ToLower(q)
	return strings.Contains(ql, "nhiều dầu") ||
		strings.Contains(ql, "da dầu") ||
		strings.Contains(ql, "oily") ||
		strings.Contains(ql, "dầu nhiều")
}

func tipLooksPauseStrongProduct(tip string) bool {
	tl := strings.ToLower(tip)
	return strings.Contains(tl, "tạm nghỉ") ||
		strings.Contains(tl, "tạm tránh") ||
		strings.Contains(tl, "pause") ||
		strings.Contains(tl, "sản phẩm mạnh") ||
		strings.Contains(tl, "sản phẩm trị mụn mạnh") ||
		strings.Contains(tl, "strong active") ||
		strings.Contains(tl, "strong treatment")
}

func tipLooksOilShineCause(cause string) bool {
	cl := strings.ToLower(cause)
	return (strings.Contains(cl, "bóng") || strings.Contains(cl, "shine") || strings.Contains(cl, "oil")) &&
		(strings.Contains(cl, "dầu") || strings.Contains(cl, "bít") || strings.Contains(cl, "clog"))
}

// AlignAdminSkinAnalysisWithQuestion softens close-up laterality and rewrites
// public causes/tips so they don't contradict the user's stated context.
// Returns true when any field changed.
func AlignAdminSkinAnalysisWithQuestion(a *dto.AdminSkinReviewAnalysis, question, locale string) bool {
	if a == nil {
		return false
	}
	q := strings.TrimSpace(question)
	changed := false

	soften := adminSkinLooksCloseUpCheek(a)
	if soften {
		if s := SoftenCheekLateralityProse(a.Overview); s != a.Overview {
			a.Overview = s
			changed = true
		}
		if s := SoftenCheekLateralityProse(a.AdditionalObservations); s != a.AdditionalObservations {
			a.AdditionalObservations = s
			changed = true
		}
		if s := SoftenCheekLateralityProse(a.PhotoNotes); s != a.PhotoNotes {
			a.PhotoNotes = s
			changed = true
		}
		if s := SoftenCheekLateralityProse(a.SkinTypeNote); s != a.SkinTypeNote {
			a.SkinTypeNote = s
			changed = true
		}
		for i := range a.AttentionAreas {
			if s := SoftenCheekLateralityProse(a.AttentionAreas[i].Note); s != a.AttentionAreas[i].Note {
				a.AttentionAreas[i].Note = s
				changed = true
			}
		}
	}

	if q == "" {
		return changed
	}

	if questionImpliesSpotProduct(q) {
		filtered := make([]string, 0, len(a.SoothingTips))
		for _, tip := range a.SoothingTips {
			if tipLooksPauseStrongProduct(tip) {
				changed = true
				continue
			}
			filtered = append(filtered, tip)
		}
		a.SoothingTips = filtered

		causes := make([]string, 0, len(a.PossibleCauses))
		for _, c := range a.PossibleCauses {
			if tipLooksOilShineCause(c) && (strings.Contains(strings.ToLower(q), "bóng") || strings.Contains(strings.ToLower(q), "shine")) {
				changed = true
				continue
			}
			causes = append(causes, c)
		}
		a.PossibleCauses = causes

		replacement := "Chỗ bóng đúng kiểu lớp kem/chấm mụn đang bôi — không phải da đổ dầu cả vùng."
		if locale == "en" {
			replacement = "The shine looks like spot-cream film — not oil all over the face."
		}
		if !containsFold(a.PossibleCauses, "lớp kem") && !containsFold(a.PossibleCauses, "spot cream") &&
			(strings.Contains(strings.ToLower(q), "bóng") || strings.Contains(strings.ToLower(q), "shine")) {
			a.PossibleCauses = append([]string{replacement}, a.PossibleCauses...)
			if len(a.PossibleCauses) > 2 {
				a.PossibleCauses = a.PossibleCauses[:2]
			}
			changed = true
		}

		keepTip := "Cứ chấm đúng nốt, đừng nặn ổ đang sưng."
		if locale == "en" {
			keepTip = "Keep spot treatment on the bump — don't pick inflamed spots."
		}
		if !containsFold(a.SoothingTips, "chấm") && !containsFold(a.SoothingTips, "spot") {
			a.SoothingTips = append([]string{keepTip}, a.SoothingTips...)
			changed = true
		}
	}

	if questionAsksWrongStep(q) {
		filtered := make([]string, 0, len(a.SoothingTips))
		for _, tip := range a.SoothingTips {
			if tipLooksPauseStrongProduct(tip) {
				changed = true
				continue
			}
			filtered = append(filtered, tip)
		}
		a.SoothingTips = filtered
		askTip := "Kể đang dùng gì / có hay nặn không — từ ảnh thôi chưa chốt sai bước nào."
		if locale == "en" {
			askTip = "Tell what you're using / if you pick — a photo alone can't name the wrong step."
		}
		if !containsFold(a.SoothingTips, "đang dùng") && !containsFold(a.SoothingTips, "what you're using") {
			a.SoothingTips = append([]string{askTip}, a.SoothingTips...)
			changed = true
		}
	}

	if questionClaimsOily(q) && strings.TrimSpace(a.SkinTypeNote) != "" {
		note := a.SkinTypeNote
		if !strings.Contains(strings.ToLower(note), "dầu") && !strings.Contains(strings.ToLower(note), "oil") {
			if locale == "en" {
				a.SkinTypeNote = note + " You said your skin runs oily — that matches the local clogged-bump pattern on the photo."
			} else {
				a.SkinTypeNote = note + " Mày nói da nhiều dầu — khớp kiểu bít tắc cục bộ trên ảnh."
			}
			changed = true
		}
	}

	// Keep tips 2–3, causes 1–2.
	if len(a.SoothingTips) > 3 {
		a.SoothingTips = a.SoothingTips[:3]
		changed = true
	}
	if len(a.PossibleCauses) > 2 {
		a.PossibleCauses = a.PossibleCauses[:2]
		changed = true
	}
	if len(a.SoothingTips) == 0 {
		if locale == "en" {
			a.SoothingTips = []string{"Don't pick inflamed spots.", "Cleanse gently."}
		} else {
			a.SoothingTips = []string{"Đừng nặn ổ đang sưng đỏ.", "Rửa mặt dịu nhẹ."}
		}
		changed = true
	}
	if len(a.PossibleCauses) == 0 {
		if locale == "en" {
			a.PossibleCauses = []string{"Local clogging and irritation on the visible area."}
		} else {
			a.PossibleCauses = []string{"Do dầu bít tắc và kích ứng tại chỗ."}
		}
		changed = true
	}

	return changed
}

func containsFold(items []string, needle string) bool {
	n := strings.ToLower(needle)
	for _, it := range items {
		if strings.Contains(strings.ToLower(it), n) {
			return true
		}
	}
	return false
}
