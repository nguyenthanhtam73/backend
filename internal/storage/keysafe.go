package storage

import (
	"fmt"
	"strings"
)

// SafeObjectKey reports whether key can be used as a storage object name.
// It rejects path traversal, encoded dot-segments, absolute paths, and
// Windows drive prefixes. Empty path segments from a trailing slash are
// allowed so DeletePrefix("userID/") still names that user's folder.
func SafeObjectKey(key string) bool {
	if key == "" || strings.ContainsRune(key, 0) {
		return false
	}
	if strings.Contains(key, "\\") || strings.Contains(key, ":") {
		return false
	}
	if strings.HasPrefix(key, "/") {
		return false
	}
	lower := strings.ToLower(key)
	if strings.Contains(lower, "%2e") || strings.Contains(lower, "%2f") ||
		strings.Contains(lower, "%5c") || strings.Contains(lower, "%00") {
		return false
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// safeClean returns the canonical object key or an error when key is unsafe.
func safeClean(key string) (string, error) {
	if !SafeObjectKey(key) {
		return "", fmt.Errorf("storage: unsafe key")
	}
	k := CleanKey(key)
	if k == "" || !SafeObjectKey(k) {
		return "", fmt.Errorf("storage: unsafe key")
	}
	return k, nil
}

// IsPublicAssetKey reports keys that are not user photos.
// brand/ and static/ stay unsigned so non-user files (logos, icons) keep
// working. User photos never use these prefixes — PhotoKey starts with a
// date, and legacy keys start with a user UUID.
func IsPublicAssetKey(key string) bool {
	k := CleanKey(key)
	if k == "" || !SafeObjectKey(k) {
		return false
	}
	return strings.HasPrefix(k, "brand/") || strings.HasPrefix(k, "static/")
}
