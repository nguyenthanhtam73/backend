package alert

import (
	"context"
	"testing"
)

func TestRecorderCapturesAndFindsByInvoice(t *testing.T) {
	t.Parallel()
	rec := NewRecorder(Nop{})
	rec.Send(context.Background(), Event{
		Key:          KeyPaymentSuccess,
		UniqueSuffix: "INV-1",
		Title:        "Payment success",
		Level:        LevelInfo,
		Message:      "User x nâng cấp premium thành công, amount 99000 VND",
		Fields:       map[string]any{"invoice": "INV-1", "reason": KeyPaymentSuccess},
	})
	rec.Send(context.Background(), Event{
		Key:          KeySignatureInvalid,
		Title:        "bad",
		Level:        LevelError,
		Fields:       map[string]any{"reason": KeySignatureInvalid},
	})

	found := rec.Find(KeyPaymentSuccess, "INV-1")
	if len(found) != 1 {
		t.Fatalf("want 1 payment_success for INV-1, got %d", len(found))
	}
	if found[0].UniqueSuffix != "INV-1" {
		t.Fatalf("suffix=%s", found[0].UniqueSuffix)
	}
	if len(rec.Find(KeySignatureInvalid, "")) != 1 {
		t.Fatal("expected signature_invalid event")
	}
}

func TestRecorderClear(t *testing.T) {
	t.Parallel()
	rec := NewRecorder(nil)
	rec.Send(context.Background(), Event{Key: "a", Title: "t", Level: LevelInfo})
	rec.Clear()
	if len(rec.Snapshot()) != 0 {
		t.Fatal("expected empty after Clear")
	}
}
