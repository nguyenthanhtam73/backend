package dto

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/mediaurl"
	"github.com/google/uuid"
)

func TestResponses_UseSignedUploadURLs(t *testing.T) {
	signer := mediaurl.New("response-test-key", "", time.Hour)
	mediaurl.SetDefault(signer)
	t.Cleanup(func() { mediaurl.SetDefault(nil) })

	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	key := userID.String() + "/onboarding/a.jpg"
	raw, _ := json.Marshal([]string{key})
	snap, _ := json.Marshal(map[string]any{
		"goal":       "calm",
		"photo_urls": []string{"/uploads/" + key},
	})
	profile := SkinProfileFromDomain(&domain.SkinProfile{
		UserID:             userID,
		PhotoURLs:          raw,
		OnboardingSnapshot: snap,
	})
	if len(profile.PhotoURLs) != 1 || !signedUpload(profile.PhotoURLs[0], key) {
		t.Fatalf("photo_urls=%v", profile.PhotoURLs)
	}
	var decoded map[string]any
	if err := json.Unmarshal(profile.OnboardingSnapshot, &decoded); err != nil {
		t.Fatal(err)
	}
	photos, _ := decoded["photo_urls"].([]any)
	if len(photos) != 1 || !signedUpload(photos[0].(string), key) {
		t.Fatalf("snapshot photo_urls=%v", decoded["photo_urls"])
	}
	// Persisted form stays unsigned even while a signer is installed.
	stored := BuildPublicUploadURLs(raw)
	if len(stored) != 1 || stored[0] != "/uploads/"+key || strings.Contains(stored[0], "sig=") {
		t.Fatalf("stored=%v", stored)
	}

	check := domain.SkinCheck{
		ID:        uuid.New(),
		UserID:    userID,
		ImageURLs: raw,
		CheckDate: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
	}
	timeline := NewProgressTimelineResponse([]domain.SkinCheck{check}, 30, "/uploads")
	if len(timeline.Entries) != 1 || !signedUpload(timeline.Entries[0].ImageURLs[0], key) {
		t.Fatalf("progress=%v", timeline.Entries[0].ImageURLs)
	}
	internal := NewProgressTimelineResponse([]domain.SkinCheck{check}, 30, "")
	if strings.Contains(internal.Entries[0].ImageURLs[0], "sig=") {
		t.Fatalf("summary path was signed: %s", internal.Entries[0].ImageURLs[0])
	}

	user := UserFromDomain(&domain.User{
		ID:        userID,
		Email:     "a@dadiary.test",
		Username:  "a",
		AvatarURL: "/uploads/" + key,
	})
	if !signedUpload(user.AvatarURL, key) {
		t.Fatalf("avatar=%s", user.AvatarURL)
	}
	ext := UserFromDomain(&domain.User{
		ID:        userID,
		Email:     "a@dadiary.test",
		Username:  "a",
		AvatarURL: "https://lh3.googleusercontent.com/a/photo",
	})
	if ext.AvatarURL != "https://lh3.googleusercontent.com/a/photo" {
		t.Fatalf("external avatar=%s", ext.AvatarURL)
	}

	shareKey := "2026/10/03/admin-skin-review-public/slug__" + userID.String() + "/x.jpg"
	got := ClientUploadURLs(mustJSON([]string{shareKey}))
	if len(got) != 1 || !signedUpload(got[0], shareKey) {
		t.Fatalf("share=%v", got)
	}
	exp, sig := queryPair(t, got[0])
	if !signer.Valid(shareKey, exp, sig, time.Now()) {
		t.Fatal("public share url did not verify")
	}
}

func signedUpload(raw, key string) bool {
	return strings.HasPrefix(raw, "/uploads/"+key+"?") &&
		strings.Contains(raw, "exp=") &&
		strings.Contains(raw, "sig=")
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func queryPair(t *testing.T, raw string) (exp, sig string) {
	t.Helper()
	q := raw[strings.Index(raw, "?")+1:]
	for _, part := range strings.Split(q, "&") {
		k, v, _ := strings.Cut(part, "=")
		switch k {
		case "exp":
			exp = v
		case "sig":
			sig = v
		}
	}
	if exp == "" || sig == "" {
		t.Fatalf("query in %s", raw)
	}
	return exp, sig
}
