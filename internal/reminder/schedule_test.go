package reminder

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/streaktime"
)

func TestParseClockAndSavedSchedule(t *testing.T) {
	enabled := true
	disabled := false
	hhmm := "08:05"
	if !SavedSchedule(&enabled, &hhmm) {
		t.Fatal("enabled + HH:MM is a saved schedule")
	}
	if SavedSchedule(nil, &hhmm) || SavedSchedule(&disabled, &hhmm) || SavedSchedule(&enabled, nil) {
		t.Fatal("NULL, off, and true-without-time are not a saved schedule")
	}
	blank := "  "
	if SavedSchedule(&enabled, &blank) {
		t.Fatal("blank time is not a saved schedule")
	}
	if _, _, ok := ParseClock("8:05"); ok {
		t.Fatal("unpadded hour must be rejected")
	}
}

func TestZoneDefaultsToVietnam(t *testing.T) {
	if Zone(nil).String() != streaktime.Location.String() {
		t.Fatalf("nil zone %s", Zone(nil))
	}
	blank := "  "
	local := "Local"
	bad := "Not/AZone"
	if Zone(&blank).String() != streaktime.Location.String() ||
		Zone(&local).String() != streaktime.Location.String() ||
		Zone(&bad).String() != streaktime.Location.String() {
		t.Fatal("blank, Local, and unknown names must use Asia/Ho_Chi_Minh")
	}
	ny := "America/New_York"
	if Zone(&ny).String() != "America/New_York" {
		t.Fatalf("zone %s", Zone(&ny))
	}
}

func TestOpenWindowGraceAndMidnight(t *testing.T) {
	loc := streaktime.Location
	// 23:40 is inside 23:30 + 2h, still the same local date.
	late := time.Date(2026, 6, 15, 23, 40, 0, 0, loc)
	if _, open := OpenWindow(late, loc, "23:30", CaptureGrace); !open {
		t.Fatal("23:40 should be inside a 23:30 window")
	}
	// 00:10 the next civil day is after local midnight, even though 23:30+2h is 01:30.
	next := time.Date(2026, 6, 16, 0, 10, 0, 0, loc)
	if _, open := OpenWindow(next, loc, "23:30", CaptureGrace); open {
		t.Fatal("grace must not cross local midnight")
	}
	before := time.Date(2026, 6, 15, 8, 59, 0, 0, loc)
	if _, open := OpenWindow(before, loc, "09:00", CaptureGrace); open {
		t.Fatal("before the saved minute must wait")
	}
	exact := time.Date(2026, 6, 15, 11, 0, 0, 0, loc)
	if _, open := OpenWindow(exact, loc, "09:00", CaptureGrace); !open {
		t.Fatal("the 2h grace instant is still inside the window")
	}
	past := time.Date(2026, 6, 15, 11, 1, 0, 0, loc)
	if _, open := OpenWindow(past, loc, "09:00", CaptureGrace); open {
		t.Fatal("past 2h grace must be closed")
	}
}

func TestOpenWindowNewYorkDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// 15:30 UTC is 10:30 EST in January (inside 09:00–11:00) and 11:30 EDT in
	// July (past the 2h grace). A fixed offset gets one of these wrong.
	january := time.Date(2026, 1, 15, 15, 30, 0, 0, time.UTC)
	date, open := OpenWindow(january, ny, "09:00", CaptureGrace)
	if !open || date != "2026-01-15" {
		t.Fatalf("january window date=%s open=%v", date, open)
	}
	july := time.Date(2026, 7, 15, 15, 30, 0, 0, time.UTC)
	if _, open := OpenWindow(july, ny, "09:00", CaptureGrace); open {
		t.Fatal("july 15:30 UTC is 11:30 EDT, past a 09:00 grace")
	}

	// Fall back: 01:30 happens twice. Both wall times sit in one grace window
	// that starts at the earlier instance.
	first := time.Date(2026, 11, 1, 5, 35, 0, 0, time.UTC)  // 01:35 EDT
	second := time.Date(2026, 11, 1, 6, 35, 0, 0, time.UTC) // 01:35 EST
	if _, open := OpenWindow(first, ny, "01:30", CaptureGrace); !open {
		t.Fatal("first 01:35 on the fall-back day should be open")
	}
	if _, open := OpenWindow(second, ny, "01:30", CaptureGrace); !open {
		t.Fatal("second 01:35 on the fall-back day should still be inside grace")
	}

	// Spring forward: 02:30 does not exist. Do not slide the send to another hour.
	jumped := time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC) // 03:00 EDT
	if _, open := OpenWindow(jumped, ny, "02:30", CaptureGrace); open {
		t.Fatal("02:30 does not exist on the spring-forward day")
	}
	after := time.Date(2026, 3, 8, 7, 35, 0, 0, time.UTC) // 03:35 EDT
	if _, open := OpenWindow(after, ny, "03:30", CaptureGrace); !open {
		t.Fatal("03:30 exists after the spring-forward gap and should send")
	}
}

func TestCaptureTickFitsTenMinutes(t *testing.T) {
	if CaptureTick <= 0 || CaptureTick > 10*time.Minute {
		t.Fatalf("tick %s must land inside about 10 minutes", CaptureTick)
	}
}
