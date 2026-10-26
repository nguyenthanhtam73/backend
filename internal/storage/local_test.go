package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalRead_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(filepath.Dir(dir), "secret-upload-traversal.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(secret) })

	st, err := newLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(context.Background(), "11111111-1111-1111-1111-111111111111/a.jpg", []byte("ok"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	got, err := st.Read(context.Background(), "11111111-1111-1111-1111-111111111111/a.jpg")
	if err != nil || string(got) != "ok" {
		t.Fatalf("read=%q err=%v", got, err)
	}
	for _, key := range []string{"../secret-upload-traversal.txt", "..", "foo/../../secret-upload-traversal.txt"} {
		if _, err := st.Read(context.Background(), key); err == nil {
			t.Fatalf("read escaped for %q", key)
		}
	}
	if _, err := os.ReadFile(secret); err != nil {
		t.Fatal(err)
	}
}

func TestSafeObjectKey(t *testing.T) {
	if !SafeObjectKey("11111111-1111-1111-1111-111111111111/") {
		t.Fatal("legacy prefix should stay deletable")
	}
	if SafeObjectKey("../etc/passwd") || SafeObjectKey("%2e%2e/secret") || SafeObjectKey(`a\b`) {
		t.Fatal("unsafe key accepted")
	}
	if !IsPublicAssetKey("brand/logo.png") || IsPublicAssetKey("11111111-1111-1111-1111-111111111111/a.jpg") {
		t.Fatal("public asset classification")
	}
}
