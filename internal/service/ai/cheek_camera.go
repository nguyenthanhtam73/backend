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

// FlipFrameCheeks swaps left/right cheek words and zone keys, preserving case.
// Phrases such as "má bên trái", "phía trái cằm", and "bên trái trán" swap too.
func FlipFrameCheeks(s string) string {
	if s == "" {
		return s
	}
	type pair struct{ from, to string }
	pairs := []pair{
		{"má bên trái", "má bên phải"},
		{"Má bên trái", "Má bên phải"},
		{"má bên phải", "má bên trái"},
		{"Má bên phải", "Má bên trái"},
		{"phía trái", "phía phải"},
		{"Phía trái", "Phía phải"},
		{"phía phải", "phía trái"},
		{"Phía phải", "Phía trái"},
		{"bên trái", "bên phải"},
		{"Bên trái", "Bên phải"},
		{"bên phải", "bên trái"},
		{"Bên phải", "Bên trái"},
		{"má trái", "má phải"},
		{"Má trái", "Má phải"},
		{"má phải", "má trái"},
		{"Má phải", "Má trái"},
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
