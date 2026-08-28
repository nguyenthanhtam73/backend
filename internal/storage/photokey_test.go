package storage

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPhotoKey_DateKindUsername(t *testing.T) {
	uid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	at := time.Date(2026, 8, 29, 22, 15, 0, 0, time.FixedZone("VN", 7*3600))
	key := PhotoKey(uid, "Tam_Nguyen", KindCheckIn, ".JPG", at)

	wantPrefix := "2026/08/29/check-in/tam-nguyen__550e8400-e29b-41d4-a716-446655440000/"
	if !strings.HasPrefix(key, wantPrefix) {
		t.Fatalf("key=%q want prefix %q", key, wantPrefix)
	}
	if !strings.HasSuffix(key, ".jpg") {
		t.Fatalf("key=%q want .jpg suffix", key)
	}
	rest := strings.TrimPrefix(key, wantPrefix)
	rest = strings.TrimSuffix(rest, ".jpg")
	if _, err := uuid.Parse(rest); err != nil {
		t.Fatalf("filename uuid %q: %v", rest, err)
	}
}

func TestSlugUsername_FallbackAndSanitize(t *testing.T) {
	uid := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if got := SlugUsername("心", uid); got != "aaaaaaaa" {
		t.Fatalf("unicode-only slug=%q", got)
	}
	if got := SlugUsername("  Hello World!! ", uid); got != "hello-world" {
		t.Fatalf("spaces slug=%q", got)
	}
	if got := SlugUsername("", uuid.Nil); got != "user" {
		t.Fatalf("empty slug=%q", got)
	}
}

func TestPhotoKey_RejectsPathInjectionInKind(t *testing.T) {
	uid := uuid.New()
	key := PhotoKey(uid, "tam", "../etc", ".png", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	if strings.Contains(key, "..") {
		t.Fatalf("kind leaked: %q", key)
	}
	if !strings.Contains(key, "/check-in/") {
		t.Fatalf("unsafe kind should fall back to check-in: %q", key)
	}
}
