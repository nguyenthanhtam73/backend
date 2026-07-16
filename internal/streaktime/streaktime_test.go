package streaktime

import (
	"testing"
	"time"
)

func TestDateOf_VietnamMidnightBoundary(t *testing.T) {
	// 2026-07-15 17:30 UTC == 2026-07-16 00:30 Asia/Ho_Chi_Minh
	justAfterVNMidnight := time.Date(2026, 7, 15, 17, 30, 0, 0, time.UTC)
	got := DateOf(justAfterVNMidnight)
	want := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("DateOf=%v want %v (VN calendar day)", got, want)
	}

	// 2026-07-15 16:59 UTC == 2026-07-15 23:59 VN — still previous VN day
	justBeforeVNMidnight := time.Date(2026, 7, 15, 16, 59, 0, 0, time.UTC)
	got = DateOf(justBeforeVNMidnight)
	want = time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("DateOf=%v want %v", got, want)
	}
}

func TestDateOf_NotRawUTCTruncation(t *testing.T) {
	// Evening VN still same VN day; UTC truncation would already be "next" or "prev".
	eveningVNAsUTC := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC) // 03:00 next day VN
	got := DateOf(eveningVNAsUTC)
	want := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("DateOf=%v want %v", got, want)
	}
}
