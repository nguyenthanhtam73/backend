package dto

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// Photo-context enums match the Android zone chips and the admin touch form
// (frontend components/admin/skin-context-fields.tsx).
const (
	PhotoKindFullFace = "full_face"
	PhotoKindCloseup  = "closeup"

	maxSkinContextExtraRunes = 300
)

// PhotoContextError is a 400 from POST /skin-checks. Code is invalid_photo_meta
// or invalid_skin_context.
type PhotoContextError struct {
	Code string
	Msg  string
}

func (e *PhotoContextError) Error() string {
	if e == nil {
		return ""
	}
	return e.Msg
}

// PhotoMetaImage is one uploaded photo's framing. Index is the 0-based position
// in images[] so a dropped entry does not shift the photos after it.
type PhotoMetaImage struct {
	Index int    `json:"index"`
	Kind  string `json:"kind"`
	Zone  string `json:"zone,omitempty"`
}

// SkinContextInput is what the user reports about touch, timing, and pain.
// Same ids as the admin skin-context form.
type SkinContextInput struct {
	Firmness string `json:"firmness,omitempty"`
	Duration string `json:"duration,omitempty"`
	Pain     string `json:"pain,omitempty"`
	Extra    string `json:"extra,omitempty"`
}

// StoredPhotoContext is the skin_checks.photo_context JSON document.
type StoredPhotoContext struct {
	Images      []PhotoMetaImage  `json:"images,omitempty"`
	SkinContext *SkinContextInput `json:"skin_context,omitempty"`
}

var (
	photoKinds = map[string]struct{}{
		PhotoKindFullFace: {},
		PhotoKindCloseup:  {},
	}
	photoZones = map[string]struct{}{
		"forehead": {}, "nose": {}, "left_cheek": {}, "right_cheek": {},
		"chin": {}, "around_mouth": {}, "jawline": {}, "under_eyes": {},
		"neck": {}, "other": {},
	}
	firmnessIDs = map[string]struct{}{
		"firm": {}, "soft": {}, "stalked": {}, "unknown": {},
	}
	durationIDs = map[string]struct{}{
		"days": {}, "weeks": {}, "months": {}, "comes_and_goes": {}, "unknown": {},
	}
	painIDs = map[string]struct{}{
		"none": {}, "itchy": {}, "sore": {}, "unknown": {},
	}
)

// ParsePhotoContext validates optional multipart photo_meta and skin_context.
// Empty inputs return a nil document. Invalid JSON is an error. Unknown enum
// ids are dropped. A close-up with no zone is stored as zone "other".
// photo_meta longer than the uploaded image count is invalid_photo_meta.
func ParsePhotoContext(photoMetaRaw, skinContextRaw string, imageCount int) (json.RawMessage, error) {
	images, err := parsePhotoMeta(photoMetaRaw, imageCount)
	if err != nil {
		return nil, err
	}
	skin, err := parseSkinContext(skinContextRaw)
	if err != nil {
		return nil, err
	}
	if len(images) == 0 && skin == nil {
		return nil, nil
	}
	doc := StoredPhotoContext{Images: images, SkinContext: skin}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func parsePhotoMeta(raw string, imageCount int) ([]PhotoMetaImage, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}
	if !json.Valid([]byte(raw)) {
		return nil, &PhotoContextError{Code: "invalid_photo_meta", Msg: `field "photo_meta" must be a JSON array`}
	}
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, &PhotoContextError{Code: "invalid_photo_meta", Msg: `field "photo_meta" must be a JSON array`}
	}
	if len(items) > imageCount {
		return nil, &PhotoContextError{Code: "invalid_photo_meta", Msg: "photo_meta has more entries than uploaded images"}
	}
	out := make([]PhotoMetaImage, 0, len(items))
	for i, item := range items {
		if len(item) == 0 || string(item) == "null" {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			continue
		}
		kind, ok := jsonStringField(obj["kind"])
		if !ok {
			continue
		}
		kind = strings.ToLower(strings.TrimSpace(kind))
		if _, known := photoKinds[kind]; !known {
			continue
		}
		zone := ""
		if z, ok := jsonStringField(obj["zone"]); ok {
			zone = strings.ToLower(strings.TrimSpace(z))
			if zone != "" {
				if _, known := photoZones[zone]; !known {
					zone = ""
				}
			}
		}
		if kind == PhotoKindCloseup && zone == "" {
			zone = "other"
		}
		img := PhotoMetaImage{Index: i, Kind: kind}
		if zone != "" {
			img.Zone = zone
		}
		out = append(out, img)
	}
	return out, nil
}

func parseSkinContext(raw string) (*SkinContextInput, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return nil, nil
	}
	if !json.Valid([]byte(raw)) {
		return nil, &PhotoContextError{Code: "invalid_skin_context", Msg: `field "skin_context" must be a JSON object`}
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, &PhotoContextError{Code: "invalid_skin_context", Msg: `field "skin_context" must be a JSON object`}
	}
	out := &SkinContextInput{}
	if v, ok := jsonStringField(obj["firmness"]); ok {
		v = strings.ToLower(strings.TrimSpace(v))
		if _, known := firmnessIDs[v]; known {
			out.Firmness = v
		}
	}
	if v, ok := jsonStringField(obj["duration"]); ok {
		v = strings.ToLower(strings.TrimSpace(v))
		if _, known := durationIDs[v]; known {
			out.Duration = v
		}
	}
	if v, ok := jsonStringField(obj["pain"]); ok {
		v = strings.ToLower(strings.TrimSpace(v))
		if _, known := painIDs[v]; known {
			out.Pain = v
		}
	}
	if v, ok := jsonStringField(obj["extra"]); ok {
		v = strings.TrimSpace(v)
		if utf8.RuneCountInString(v) > maxSkinContextExtraRunes {
			return nil, &PhotoContextError{Code: "invalid_skin_context", Msg: "skin_context.extra must be at most 300 characters"}
		}
		out.Extra = v
	}
	if out.Firmness == "" && out.Duration == "" && out.Pain == "" && out.Extra == "" {
		return nil, nil
	}
	return out, nil
}

func jsonStringField(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// DecodePhotoContext reads a stored photo_context document. Malformed JSON
// yields an empty document so a bad column cannot break a read.
func DecodePhotoContext(raw json.RawMessage) StoredPhotoContext {
	if len(raw) == 0 || string(raw) == "null" {
		return StoredPhotoContext{}
	}
	var doc StoredPhotoContext
	if err := json.Unmarshal(raw, &doc); err != nil {
		return StoredPhotoContext{}
	}
	return doc
}

// PhotoContextExtra is the free-text touch note, included in moderation.
func PhotoContextExtra(raw json.RawMessage) string {
	return strings.TrimSpace(DecodePhotoContext(raw).SkinContextExtra())
}

// SkinContextExtra returns the free-text note, or empty.
func (d StoredPhotoContext) SkinContextExtra() string {
	if d.SkinContext == nil {
		return ""
	}
	return strings.TrimSpace(d.SkinContext.Extra)
}

// PublicPhotoMetaAndSkin returns the API echo of a stored document.
// Both are nil when that half was not stored.
func PublicPhotoMetaAndSkin(raw json.RawMessage) ([]PhotoMetaImage, *SkinContextInput) {
	doc := DecodePhotoContext(raw)
	var images []PhotoMetaImage
	if len(doc.Images) > 0 {
		images = doc.Images
	}
	return images, doc.SkinContext
}

// IsPhotoContextError reports a photo_meta / skin_context validation error.
func IsPhotoContextError(err error) (*PhotoContextError, bool) {
	var pe *PhotoContextError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}
