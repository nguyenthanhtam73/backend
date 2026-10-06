package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// localStorage persists objects on the local filesystem, rooted at an absolute dir.
// Behavior mirrors the pre-storage disk logic so the local dev experience is unchanged.
type localStorage struct {
	root string
}

func newLocal(dir string) (*localStorage, error) {
	if dir == "" {
		dir = "./data/uploads"
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("storage(local): resolve dir: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("storage(local): create dir: %w", err)
	}
	return &localStorage{root: abs}, nil
}

// abs resolves key under the upload root. Keys that escape the root — including
// ".." segments that filepath.Join would clean — are rejected.
func (l *localStorage) abs(key string) (string, error) {
	if !SafeObjectKey(key) {
		return "", fmt.Errorf("storage(local): unsafe key")
	}
	rel := filepath.FromSlash(CleanKey(key))
	if rel == "" || rel == "." {
		return "", fmt.Errorf("storage(local): empty key")
	}
	root := filepath.Clean(l.root)
	full := filepath.Clean(filepath.Join(root, rel))
	r, err := filepath.Rel(root, full)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("storage(local): path escapes upload root")
	}
	return full, nil
}

func (l *localStorage) Save(_ context.Context, key string, data []byte, _ string) error {
	p, err := l.abs(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("storage(local): mkdir: %w", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return fmt.Errorf("storage(local): write: %w", err)
	}
	return nil
}

func (l *localStorage) Read(_ context.Context, key string) ([]byte, error) {
	p, err := l.abs(key)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

func (l *localStorage) DeletePrefix(_ context.Context, prefix string) error {
	p, err := l.abs(prefix)
	if err != nil {
		return err
	}
	// RemoveAll tolerates missing paths and removes the whole subtree.
	return os.RemoveAll(p)
}

func (l *localStorage) Driver() string   { return "local" }
func (l *localStorage) LocalDir() string { return l.root }
