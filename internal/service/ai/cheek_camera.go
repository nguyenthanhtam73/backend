package ai

import (
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

// ResolveCamera reads photo_meta.camera. It does not decide whether to swap
// cheeks. Swap unless the image is known to be mirrored; see ShouldSwapCheeks.
func ResolveCamera(doc dto.StoredPhotoContext) string {
	for _, img := range doc.Images {
		switch strings.ToLower(strings.TrimSpace(img.Camera)) {
		case "rear", "back", "environment":
			return "rear"
		case "front", "user", "selfie":
			return "front"
		}
	}
	return "front"
}

// ShouldSwapCheeks reports whether image-left/right must be flipped onto the
// user's face. An unmirrored photo — rear camera, or a front camera that was
// not flipped — shows the user's left cheek on the image's right, so the
// label is swapped. The swap is skipped only when mirrored is true, or when
// every photo is a close-up of a zone the user picked.
func ShouldSwapCheeks(doc dto.StoredPhotoContext) bool {
	if len(doc.Images) == 0 {
		return true
	}
	closeups := 0
	others := 0
	for _, img := range doc.Images {
		if img.Mirrored != nil && *img.Mirrored {
			return false
		}
		if img.Kind == dto.PhotoKindCloseup && img.Zone != "" && img.Zone != "other" {
			closeups++
		} else {
			others++
		}
	}
	return !(closeups > 0 && others == 0)
}

// UserPickedCloseupZone is the zone on a close-up the user chose.
// Empty when the check is not that kind of photo.
func UserPickedCloseupZone(doc dto.StoredPhotoContext) string {
	for _, img := range doc.Images {
		if img.Kind == dto.PhotoKindCloseup && img.Zone != "" && img.Zone != "other" {
			return img.Zone
		}
	}
	return ""
}

// MapFrameCheek maps an image edge to the user's cheek.
// mirrored false swaps (user's left is on the image's right).
// mirrored true keeps the edge (the file is already a mirror).
func MapFrameCheek(frameSide string, mirrored bool) string {
	side := strings.ToLower(strings.TrimSpace(frameSide))
	if side != "left" && side != "right" {
		return side
	}
	if mirrored {
		return side
	}
	if side == "left" {
		return "right"
	}
	return "left"
}

// preferBenSide rewrites "phía phải cằm" to "bên phải cằm" and the left
// equivalent. The side itself is not flipped here.
func preferBenSide(s string) string {
	if s == "" {
		return s
	}
	parts := splitVoiceChunks(s)
	for i := range parts {
		if !parts[i].word || strings.ToLower(parts[i].text) != "phía" {
			continue
		}
		side := nextWordIndex(parts, i)
		if side < 0 || !isSideWord(parts[side].text) {
			continue
		}
		if !sideAttachedToZone(parts, i, side) {
			continue
		}
		parts[i].text = matchVoiceCase(parts[i].text, "bên")
	}
	return joinVoiceChunks(parts)
}

// FlipFrameCheeks swaps left/right on a face zone, preserving case.
// "bên trái/phải" swaps only when a zone is attached, so "nằm nghiêng bên phải"
// stays. "cằm trái" and "trán phải" swap, and so do their opposites.
// "phía" next to a zone is written "bên" as it swaps.
func FlipFrameCheeks(s string) string {
	if s == "" {
		return s
	}
	s = swapZoneKeyText(s)
	parts := splitVoiceChunks(s)
	swapped := map[int]bool{}
	for i := range parts {
		if !parts[i].word || swapped[i] {
			continue
		}
		low := strings.ToLower(parts[i].text)
		if isFaceZoneWord(low) {
			j := nextWordIndex(parts, i)
			if j < 0 || swapped[j] {
				continue
			}
			mid := strings.ToLower(parts[j].text)
			if mid == "bên" || mid == "phía" {
				k := nextWordIndex(parts, j)
				if k < 0 || !isSideWord(parts[k].text) || swapped[k] {
					continue
				}
				parts[k].text = flipSideWord(parts[k].text)
				if mid == "phía" {
					parts[j].text = matchVoiceCase(parts[j].text, "bên")
				}
				swapped[i], swapped[j], swapped[k] = true, true, true
				continue
			}
			if isSideWord(parts[j].text) {
				parts[j].text = flipSideWord(parts[j].text)
				swapped[i], swapped[j] = true, true
			}
			continue
		}
		if low != "bên" && low != "phía" {
			continue
		}
		j := nextWordIndex(parts, i)
		if j < 0 || !isSideWord(parts[j].text) || swapped[j] {
			continue
		}
		if !sideAttachedToZone(parts, i, j) {
			continue
		}
		parts[j].text = flipSideWord(parts[j].text)
		if low == "phía" {
			parts[i].text = matchVoiceCase(parts[i].text, "bên")
		}
		swapped[i], swapped[j] = true, true
	}
	return joinVoiceChunks(parts)
}

func sideAttachedToZone(parts []voiceChunk, sidePhrase, side int) bool {
	next := nextWordIndex(parts, side)
	if next >= 0 && strings.ToLower(parts[next].text) == "của" {
		next = nextWordIndex(parts, next)
	}
	if next >= 0 && isFaceZoneWord(parts[next].text) {
		return true
	}
	prev := prevWordIndex(parts, sidePhrase)
	return prev >= 0 && isFaceZoneWord(parts[prev].text)
}

func isFaceZoneWord(s string) bool {
	switch strings.ToLower(s) {
	case "má", "cằm", "trán", "mũi", "hàm", "mắt", "cổ", "môi":
		return true
	default:
		return false
	}
}

func isSideWord(s string) bool {
	switch strings.ToLower(s) {
	case "trái", "phải":
		return true
	default:
		return false
	}
}

func flipSideWord(s string) string {
	switch strings.ToLower(s) {
	case "trái":
		return matchVoiceCase(s, "phải")
	case "phải":
		return matchVoiceCase(s, "trái")
	default:
		return s
	}
}

func nextWordIndex(parts []voiceChunk, i int) int {
	for j := i + 1; j < len(parts); j++ {
		if parts[j].word {
			return j
		}
	}
	return -1
}

func prevWordIndex(parts []voiceChunk, i int) int {
	for j := i - 1; j >= 0; j-- {
		if parts[j].word {
			return j
		}
	}
	return -1
}

func joinVoiceChunks(parts []voiceChunk) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.text)
	}
	return b.String()
}

func swapZoneKeyText(s string) string {
	type pair struct{ from, to string }
	pairs := []pair{
		{"left_cheek", "right_cheek"},
		{"right_cheek", "left_cheek"},
	}
	const mark = "\x00"
	var b strings.Builder
	for i := 0; i < len(s); {
		matched := false
		for n, p := range pairs {
			if strings.HasPrefix(s[i:], p.from) {
				b.WriteString(mark)
				b.WriteString(strings.Repeat("x", n))
				b.WriteString(mark)
				i += len(p.from)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(s[i])
			i++
		}
	}
	out := b.String()
	for n, p := range pairs {
		out = strings.ReplaceAll(out, mark+strings.Repeat("x", n)+mark, p.to)
	}
	return out
}
