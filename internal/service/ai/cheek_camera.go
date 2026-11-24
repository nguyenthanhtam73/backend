package ai

import (
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

// ResolveCamera reads photo_meta.camera.
// "rear" / "back" / "environment" is the rear lens and is not mirrored.
// Anything else, including a missing field, is "front": a check-in face photo
// is a mirrored selfie unless the client says the rear camera was used.
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

// MapFrameCheek turns the left or right edge of the saved image into the
// user's own cheek. The model is asked to name the edge it sees.
// A front-camera selfie is mirrored, so the left edge is the user's right
// cheek and the label is swapped. A rear-camera photo is not mirrored, so
// the label stays.
func MapFrameCheek(frameSide, camera string) string {
	side := strings.ToLower(strings.TrimSpace(frameSide))
	if side != "left" && side != "right" {
		return side
	}
	if strings.ToLower(strings.TrimSpace(camera)) == "rear" {
		return side
	}
	if side == "left" {
		return "right"
	}
	return "left"
}

// FlipFrameCheeks swaps má trái/phải and left_cheek/right_cheek.
// Call it once for a front-camera photo. Do not call it for rear.
func FlipFrameCheeks(s string) string {
	if s == "" {
		return s
	}
	const (
		lt = "\x00LT\x00"
		rt = "\x00RT\x00"
		lk = "\x00LK\x00"
		rk = "\x00RK\x00"
	)
	replacer := strings.NewReplacer(
		"má trái", lt, "Má trái", lt, "MÁ TRÁI", lt,
		"má phải", rt, "Má phải", rt, "MÁ PHẢI", rt,
		"left_cheek", lk,
		"right_cheek", rk,
	)
	s = replacer.Replace(s)
	s = strings.NewReplacer(
		lt, "má phải",
		rt, "má trái",
		lk, "right_cheek",
		rk, "left_cheek",
	).Replace(s)
	return s
}
