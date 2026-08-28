package storage

import (
	"strings"
	"time"
	"unicode"

	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

// Object kinds used as the folder after YYYY/MM/DD in Cloudflare R2.
const (
	KindCheckIn               = "check-in"
	KindOnboarding            = "onboarding"
	KindAdminSkinReview       = "admin-skin-review"
	KindAdminSkinReviewPublic = "admin-skin-review-public"
)

// PhotoKey builds a storage key that is browsable in the R2 dashboard:
//
//	{YYYY}/{MM}/{DD}/{kind}/{username}__{userID}/{uuid}{ext}
//
// Date is the Vietnam civil day (same calendar as check-ins). Username is
// sanitized so the folder name is readable; userID stays in the path so
// rename/collision cannot mix two accounts. Legacy keys
// ("{userID}/{uuid}.jpg") are unchanged — only new uploads use this shape.
func PhotoKey(userID uuid.UUID, username, kind, ext string, at time.Time) string {
	if at.IsZero() {
		at = time.Now()
	}
	day := streaktime.DateOf(at)
	kind = sanitizeKind(kind)
	slug := SlugUsername(username, userID)
	id := uuid.New().String()
	ext = sanitizeExt(ext)
	owner := slug + "__" + userID.String()
	return strings.Join([]string{
		day.Format("2006"),
		day.Format("01"),
		day.Format("02"),
		kind,
		owner,
		id + ext,
	}, "/")
}

// SlugUsername folds a username into a path-safe token. Empty / non-ascii-only
// names fall back to the first 8 hex chars of userID so the folder still
// identifies someone.
func SlugUsername(username string, userID uuid.UUID) string {
	s := strings.ToLower(strings.TrimSpace(username))
	var b strings.Builder
	b.Grow(len(s))
	prevSep := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevSep = false
		case r == '_' || r == '-' || unicode.IsSpace(r):
			if !prevSep {
				b.WriteByte('-')
				prevSep = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 32 {
		out = strings.Trim(out[:32], "-")
	}
	if out == "" {
		if userID == uuid.Nil {
			return "user"
		}
		hex := strings.ReplaceAll(userID.String(), "-", "")
		if len(hex) >= 8 {
			return hex[:8]
		}
		return "user"
	}
	return out
}

func sanitizeKind(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	k = strings.ReplaceAll(k, "\\", "/")
	k = strings.Trim(k, "/")
	if k == "" || strings.Contains(k, "..") {
		return KindCheckIn
	}
	var b strings.Builder
	prevSep := true
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevSep = false
		case r == '-':
			if !prevSep {
				b.WriteByte('-')
				prevSep = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return KindCheckIn
	}
	return out
}

func sanitizeExt(ext string) string {
	e := strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(e, ".") {
		e = "." + e
	}
	switch e {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		if e == ".jpeg" {
			return ".jpg"
		}
		return e
	default:
		return ".jpg"
	}
}
