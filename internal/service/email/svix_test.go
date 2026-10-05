package email

import (
	"testing"
	"time"
)

func TestVerifySvix_GoldenVector(t *testing.T) {
	// Independent HMAC-SHA256 of msg_2.1700000000.<body> with 24 bytes of 0x11.
	const (
		secret = "whsec_ERERERERERERERERERERERERERERERER"
		id     = "msg_2"
		ts     = "1700000000"
		sig    = "v1,GiYVOEltBEIK+QoCbvd16cX5FxA8aaCOHSUM6q8UoRo="
	)
	body := []byte(`{"type":"email.opened","data":{"email_id":"abc"}}`)
	now := time.Unix(1700000000, 0)
	if err := VerifySvix(secret, body, id, ts, sig, now); err != nil {
		t.Fatal(err)
	}
	// webhook secret without the whsec_ prefix is accepted too.
	if err := VerifySvix("ERERERERERERERERERERERERERERERER", body, id, ts, sig, now); err != nil {
		t.Fatal(err)
	}
	if err := VerifySvix(secret, body, id, ts, "v1,AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", now); err == nil {
		t.Fatal("bad signature accepted")
	}
	if err := VerifySvix(secret, append(body, ' '), id, ts, sig, now); err == nil {
		t.Fatal("tampered body accepted")
	}
	if err := VerifySvix(secret, body, id, ts, sig, now.Add(10*time.Minute)); err == nil {
		t.Fatal("stale timestamp accepted")
	}
	if err := VerifySvix("", body, id, ts, sig, now); err != ErrWebhookNotConfigured {
		t.Fatalf("empty secret err=%v", err)
	}
}
