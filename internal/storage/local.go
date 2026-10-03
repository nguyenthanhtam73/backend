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

func (l *localStorage) abs(key string) (string, error) {
	cleaned, err := normalizeKey(key)
	if err != nil {
		return "", err
	}
	full := filepath.Join(l.root, filepath.FromSlash(cleaned))
	rel, err := filepath.Rel(l.root, full)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errUnsafeKey
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
	// RemoveAll tolerates a missing path and removes the whole subtree.
	if err := os.RemoveAll(p); err != nil {
		return fmt.Errorf("storage(local): delete: %w", err)
	}
	return nil
}

func (l *localStorage) Driver() string   { return "local" }
func (l *localStorage) LocalDir() string { return l.root }
