package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestCleanKey_RejectsUnsafeAndKeepsUploadsPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "   ", want: ""},
		{in: "..", want: ""},
		{in: "../secret", want: ""},
		{in: "foo/../../etc/passwd", want: ""},
		{in: "/etc/passwd", want: ""},
		{in: `C:\Windows\system32`, want: ""},
		{in: `\\server\share\a.jpg`, want: ""},
		{in: "/uploads/../etc/passwd", want: ""},
		{in: "/uploads/", want: ""},
		{in: "/uploads/2026/10/a.jpg", want: "2026/10/a.jpg"},
		{in: "/uploads/2026/10/a.jpg?exp=1999999999&sig=abc", want: "2026/10/a.jpg"},
		{in: "2026/10/a.jpg#fragment", want: "2026/10/a.jpg"},
		{in: "uploads/2026/10/a.jpg", want: "2026/10/a.jpg"},
		{in: `2026\10\a.jpg`, want: "2026/10/a.jpg"},
		{in: "2026/10/a.jpg", want: "2026/10/a.jpg"},
	}
	for _, tc := range cases {
		if got := CleanKey(tc.in); got != tc.want {
			t.Errorf("CleanKey(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestLocalDeletePrefix_RejectsEscape(t *testing.T) {
	root := t.TempDir()
	store, err := newLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := uuid.NewString()
	inside := "2026/10/03/check-in/ann__" + id + "/a.jpg"
	legacy := id + "/legacy.jpg"
	other := "2026/10/03/check-in/bee__" + uuid.NewString() + "/b.jpg"
	if err := store.Save(ctx, inside, []byte("face"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, legacy, []byte("old"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, other, []byte("keep"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(filepath.Dir(root), "outside-"+id+".txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })

	for _, bad := range []string{"", "..", "../" + filepath.Base(outside), "/etc/passwd", `C:\temp\a.jpg`, "/uploads/../../etc/passwd"} {
		if err := store.DeletePrefix(ctx, bad); err == nil {
			t.Fatalf("DeletePrefix(%q) succeeded", bad)
		} else if !errors.Is(err, errUnsafeKey) {
			t.Fatalf("DeletePrefix(%q)=%v", bad, err)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("file outside uploads root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(other))); err != nil {
		t.Fatalf("other user's file: %v", err)
	}

	if err := store.DeletePrefix(ctx, inside); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(ctx, inside); !os.IsNotExist(err) {
		t.Fatalf("deleted key still readable: %v", err)
	}
	if _, err := store.Read(ctx, other); err != nil {
		t.Fatalf("other key: %v", err)
	}
	if err := store.DeletePrefix(ctx, id+"/"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(ctx, legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy key still readable: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("uploads root removed: %v", err)
	}
}

func TestR2DeletePrefix_RequiresUserScopedPrefix(t *testing.T) {
	r := &r2Storage{}
	ctx := context.Background()
	id := uuid.NewString()
	bad := []string{
		"",
		"a",
		"2026",
		"2026/10/03/check-in",
		"..",
		"/etc/passwd",
		"folder/not-a-user",
		"2026/10/03/check-in/someone-without-a-user-id/a.jpg",
	}
	for _, prefix := range bad {
		err := r.DeletePrefix(ctx, prefix)
		if !errors.Is(err, errUnsafeKey) {
			t.Fatalf("DeletePrefix(%q)=%v", prefix, err)
		}
	}

	ok := []string{
		id,
		id + "/",
		id + "/onboarding/a.jpg",
		"2026/10/03/check-in/ann__" + id,
		"2026/10/03/check-in/ann__" + id + "/a.jpg",
		"/uploads/2026/10/03/check-in/ann__" + id + "/a.jpg",
	}
	for _, prefix := range ok {
		err := r.DeletePrefix(ctx, prefix)
		if err == nil || errors.Is(err, errUnsafeKey) {
			t.Fatalf("DeletePrefix(%q)=%v, want a client error after the prefix check", prefix, err)
		}
	}
}
