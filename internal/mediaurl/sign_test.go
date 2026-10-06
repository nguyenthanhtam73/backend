package mediaurl

import (
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNew_BothKeysEmptyFailsClosed(t *testing.T) {
	for _, pair := range [][2]string{{"", ""}, {"  ", "\t"}, {"", "   "}} {
		s := New(pair[0], pair[1], time.Hour)
		if s != nil {
			t.Fatalf("New(%q, %q) returned a signer", pair[0], pair[1])
		}
		key := "11111111-1111-1111-1111-111111111111/a.jpg"
		if got := s.SignedPath(key); got != "" {
			t.Fatalf("nil signer issued %q", got)
		}
		if got := s.SignedPathAt(key, time.Now()); got != "" {
			t.Fatalf("nil signer issued %q", got)
		}
		if s.Valid(key, "9999999999", "abcd", time.Now()) {
			t.Fatal("nil signer accepted a signature")
		}
		if s.TTL() != DefaultTTL {
			t.Fatalf("nil TTL=%s", s.TTL())
		}
	}
}

func TestNew_DerivesFromJWTSecret(t *testing.T) {
	derived := New("", "jwt-secret", 0)
	if derived == nil {
		t.Fatal("expected signer derived from jwt secret")
	}
	if derived.TTL() != DefaultTTL {
		t.Fatalf("ttl=%s", derived.TTL())
	}
	raw := New("jwt-secret", "", time.Hour)
	key := "11111111-1111-1111-1111-111111111111/a.jpg"
	signed := derived.SignedPath(key)
	exp, sig := splitQuery(t, signed)
	if raw.Valid(key, exp, sig, time.Now()) {
		t.Fatal("derived key must not equal the raw jwt secret")
	}
	if !derived.Valid(key, exp, sig, time.Now()) {
		t.Fatal("derived signer rejected its own url")
	}
}

func TestNew_ClampsTTL(t *testing.T) {
	s := New("k", "", 1000*time.Hour)
	if s.TTL() != MaxTTL {
		t.Fatalf("ttl=%s", s.TTL())
	}
}

func TestValid_ExpiredAndTampered(t *testing.T) {
	s := New("k", "", time.Hour)
	key := "11111111-1111-1111-1111-111111111111/a.jpg"
	fresh := s.SignedPath(key)
	exp, sig := splitQuery(t, fresh)
	if !s.Valid(key, exp, sig, time.Now()) {
		t.Fatal("fresh signature rejected")
	}
	expired := s.SignedPathAt(key, time.Now().Add(-2*time.Hour))
	exp, sig = splitQuery(t, expired)
	if s.Valid(key, exp, sig, time.Now()) {
		t.Fatal("expired signature accepted")
	}
	exp, sig = splitQuery(t, fresh)
	tampered := sig[:len(sig)-1] + flipHex(sig[len(sig)-1])
	if s.Valid(key, exp, tampered, time.Now()) {
		t.Fatal("tampered signature accepted")
	}
}

func TestMAC_OtherUserScopeRejected(t *testing.T) {
	s := New("k", "", time.Hour)
	userA := "11111111-1111-1111-1111-111111111111"
	userB := "22222222-2222-2222-2222-222222222222"
	keyB := userB + "/b.jpg"
	exp := time.Now().Add(time.Minute).Unix()
	forged := hex.EncodeToString(s.mac(keyB, userA, exp))
	if s.Valid(keyB, strconv.FormatInt(exp, 10), forged, time.Now()) {
		t.Fatal("signature scoped to another user was accepted")
	}
	good := hex.EncodeToString(s.mac(keyB, ScopeForKey(keyB), exp))
	if !s.Valid(keyB, strconv.FormatInt(exp, 10), good, time.Now()) {
		t.Fatal("owner scope rejected")
	}
}

func TestScopeForKey_PublicShareIsNotOwner(t *testing.T) {
	s := New("k", "", time.Hour)
	admin := "11111111-1111-1111-1111-111111111111"
	key := "2026/10/03/admin-skin-review-public/slug__" + admin + "/x.jpg"
	if ScopeForKey(key) != "public" {
		t.Fatalf("scope=%q", ScopeForKey(key))
	}
	exp := time.Now().Add(time.Minute).Unix()
	ownerSig := hex.EncodeToString(s.mac(key, admin, exp))
	if s.Valid(key, strconv.FormatInt(exp, 10), ownerSig, time.Now()) {
		t.Fatal("admin user scope must not authorize a public share object")
	}
	if !s.Valid(key, strconv.FormatInt(exp, 10), hex.EncodeToString(s.mac(key, "public", exp)), time.Now()) {
		t.Fatal("public scope rejected")
	}
}

func TestKeyFromWildcard_RejectsTraversal(t *testing.T) {
	for _, raw := range []string{
		"../secret",
		"..%2Fsecret",
		"%2e%2e/secret",
		"foo/../../etc/passwd",
		"%2e%2e%2fetc%2fpasswd",
		"brand/../../secret",
	} {
		if _, ok := KeyFromWildcard(raw); ok {
			t.Fatalf("accepted %q", raw)
		}
	}
	key, ok := KeyFromWildcard("brand/logo.png")
	if !ok || key != "brand/logo.png" {
		t.Fatalf("brand key=%q ok=%v", key, ok)
	}
	// A leading slash is folded into a relative object key. It must not stay
	// absolute — filepath.Join would otherwise discard the upload root.
	key, ok = KeyFromWildcard("/etc/passwd")
	if !ok || key != "etc/passwd" {
		t.Fatalf("absolute capture key=%q ok=%v", key, ok)
	}
}

func TestSignClientURL_LeavesExternalAvatar(t *testing.T) {
	SetDefault(New("k", "", time.Hour))
	t.Cleanup(func() { SetDefault(nil) })
	const external = "https://lh3.googleusercontent.com/a/photo"
	if got := SignClientURL(external); got != external {
		t.Fatalf("external avatar rewritten: %s", got)
	}
	signed := SignClientURL("/uploads/11111111-1111-1111-1111-111111111111/a.jpg")
	if !strings.Contains(signed, "sig=") || !strings.HasPrefix(signed, "/uploads/") {
		t.Fatalf("signed=%s", signed)
	}
	if got := SignClientURL("brand/logo.png"); strings.Contains(got, "sig=") {
		t.Fatalf("brand asset signed: %s", got)
	}
}

func splitQuery(t *testing.T, signed string) (exp, sig string) {
	t.Helper()
	q := signed[strings.Index(signed, "?")+1:]
	for _, part := range strings.Split(q, "&") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			t.Fatalf("query %q", signed)
		}
		switch k {
		case "exp":
			exp = v
		case "sig":
			sig = v
		}
	}
	if exp == "" || sig == "" {
		t.Fatalf("missing exp/sig in %s", signed)
	}
	return exp, sig
}

func flipHex(b byte) string {
	if b == '0' {
		return "1"
	}
	return "0"
}
