package ai

import "testing"

func TestFriendlySkinTypeVI(t *testing.T) {
	if got := friendlySkinType("combo", "vi"); got != "da hỗn hợp" {
		t.Fatalf("got %q", got)
	}
}

func TestFriendlyUndertoneVI(t *testing.T) {
	if got := friendlyUndertone("warm", "vi"); got != "tone ấm" {
		t.Fatalf("got %q", got)
	}
}

func TestFriendlyConcernPreservesVietnameseLabel(t *testing.T) {
	if got := friendlyConcern("mụn viêm", "vi"); got != "mụn viêm" {
		t.Fatalf("got %q", got)
	}
}

func TestFriendlyConcernMapsID(t *testing.T) {
	if got := friendlyConcern("acne", "vi"); got != "mụn" {
		t.Fatalf("got %q", got)
	}
}
