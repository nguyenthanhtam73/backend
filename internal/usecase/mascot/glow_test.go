package mascot

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
)

func TestGlowLevelThresholds(t *testing.T) {
	cases := []struct {
		days  int
		level int
		next  *int
	}{
		{days: -1, level: 1, next: intPtr(3)},
		{days: 0, level: 1, next: intPtr(3)},
		{days: 2, level: 1, next: intPtr(3)},
		{days: 3, level: 2, next: intPtr(7)},
		{days: 6, level: 2, next: intPtr(7)},
		{days: 7, level: 3, next: intPtr(14)},
		{days: 13, level: 3, next: intPtr(14)},
		{days: 14, level: 4, next: intPtr(21)},
		{days: 20, level: 4, next: intPtr(21)},
		{days: 21, level: 5, next: nil},
		{days: 40, level: 5, next: nil},
	}
	for _, tc := range cases {
		if got := GlowLevel(tc.days); got != tc.level {
			t.Fatalf("days %d level %d want %d", tc.days, got, tc.level)
		}
		got := NextLevelAt(tc.level)
		if tc.next == nil {
			if got != nil {
				t.Fatalf("days %d next %d", tc.days, *got)
			}
			continue
		}
		if got == nil || *got != *tc.next {
			t.Fatalf("days %d next %#v want %d", tc.days, got, *tc.next)
		}
	}
}

func TestEffectiveGlowNeverDecreases(t *testing.T) {
	if got := EffectiveGlow(4, 0); got != 4 {
		t.Fatalf("stored floor %d", got)
	}
	if got := EffectiveGlow(1, 21); got != 5 {
		t.Fatalf("computed wins %d", got)
	}
	if got := EffectiveGlow(0, 2); got != 1 {
		t.Fatalf("zero stored %d", got)
	}
	if got := EffectiveGlow(5, 7); got != 5 {
		t.Fatalf("max level stays %d", got)
	}
}

func TestDistinctCheckInDaysVietnam(t *testing.T) {
	// 02 Nov 17:30 UTC and 03 Nov 10:00 UTC are both 03 Nov in Asia/Ho_Chi_Minh.
	// 02 Nov 16:00 UTC is still 02 Nov there (23:00).
	sameEvening := time.Date(2026, 11, 2, 17, 30, 0, 0, time.UTC)
	sameMorning := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	previous := time.Date(2026, 11, 2, 16, 0, 0, 0, time.UTC)
	if got := distinctCheckInDays([]time.Time{sameEvening, sameMorning, previous, {}}); got != 2 {
		t.Fatalf("days %d", got)
	}
	day := streaktime.DateOf(sameEvening)
	if got := distinctCheckInDays([]time.Time{day, day}); got != 1 {
		t.Fatalf("same civil day %d", got)
	}
	if distinctCheckInDays(nil) != 0 {
		t.Fatal("empty")
	}
}

func TestAccessoryWearableUnlocks(t *testing.T) {
	head := domain.MascotItemAccessoryPinkHeadband
	star := domain.MascotItemAccessoryStarClip
	if AccessoryWearable(head, 2, domain.MascotSourceFree) {
		t.Fatal("headband free at 2")
	}
	if !AccessoryWearable(head, 3, domain.MascotSourceFree) {
		t.Fatal("headband free at 3")
	}
	if AccessoryWearable(star, 3, domain.MascotSourceFree) {
		t.Fatal("star free at 3")
	}
	if !AccessoryWearable(star, 4, domain.MascotSourceRoomReward) {
		t.Fatal("star at 4")
	}
	if !AccessoryWearable(head, 1, domain.MascotSourceShop) || !AccessoryWearable(star, 1, domain.MascotSourceShop) {
		t.Fatal("shop bypass")
	}
	if AccessoryWearable(star, 1, domain.MascotSourceRoomGraduation) {
		t.Fatal("graduation does not bypass")
	}
	if !AccessoryWearable(domain.MascotItemRoomPlant, 1, domain.MascotSourceFree) {
		t.Fatal("room has no glow gate")
	}

	sources := map[string]string{
		head: domain.MascotSourceFree,
		star: domain.MascotSourceFree,
	}
	loadout := domain.DefaultMascotLoadout()
	if err := ValidateGlowWear(loadout, 2, sources); err == nil {
		t.Fatal("expected lock")
	}
	ae, ok := domain.AsAppError(ValidateGlowWear(loadout, 2, sources))
	if !ok || ae.Code != "item_locked" {
		t.Fatalf("code %v", ValidateGlowWear(loadout, 2, sources))
	}
	onlyHead := loadout
	onlyHead.Accessory = head
	onlyHead.Accessories = []string{head}
	if err := ValidateGlowWear(onlyHead, 3, sources); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGlowWear(loadout, 3, sources); err == nil {
		t.Fatal("star still locked at 3")
	}
	if err := ValidateGlowWear(loadout, 4, sources); err != nil {
		t.Fatal(err)
	}
	shop := map[string]string{head: domain.MascotSourceShop, star: domain.MascotSourceShop}
	if err := ValidateGlowWear(loadout, 1, shop); err != nil {
		t.Fatal(err)
	}

	shown := visibleLoadout(domain.DefaultMascotLoadout(), sources, 1)
	if shown.Accessory != "" || len(shown.Accessories) != 0 {
		t.Fatalf("level 1 shows %#v", shown.Accessories)
	}
	shown = visibleLoadout(domain.DefaultMascotLoadout(), sources, 3)
	if len(shown.Accessories) != 1 || shown.Accessories[0] != head || shown.Accessory != head {
		t.Fatalf("level 3 shows %#v", shown)
	}
	shown = visibleLoadout(domain.DefaultMascotLoadout(), sources, 4)
	if len(shown.Accessories) != 2 || shown.Accessories[1] != star {
		t.Fatalf("level 4 shows %#v", shown.Accessories)
	}
	shown = visibleLoadout(domain.DefaultMascotLoadout(), shop, 1)
	if len(shown.Accessories) != 2 {
		t.Fatalf("shop shows %#v", shown.Accessories)
	}
}

func intPtr(n int) *int { return &n }
