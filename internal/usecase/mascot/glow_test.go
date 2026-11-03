package mascot

import (
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
)

func TestGlowLevelThresholds(t *testing.T) {
	prev := -1
	for i, at := range GlowThresholds {
		if at <= prev {
			t.Fatalf("GlowThresholds must be strictly ascending, [%d]=%d", i, at)
		}
		prev = at
	}

	assertGlowBand(t, -1, 1)
	assertGlowBand(t, 0, 1)
	if len(GlowThresholds) == 0 {
		assertGlowBand(t, 100, 1)
		return
	}
	if GlowThresholds[0] > 0 {
		assertGlowBand(t, GlowThresholds[0]-1, 1)
	}
	for i, at := range GlowThresholds {
		level := i + 2
		if at > 0 {
			assertGlowBand(t, at-1, level-1)
		}
		assertGlowBand(t, at, level)
		if DaysForGlowLevel(level) != at {
			t.Fatalf("days for level %d = %d, threshold %d", level, DaysForGlowLevel(level), at)
		}
	}
	top := len(GlowThresholds) + 1
	last := GlowThresholds[len(GlowThresholds)-1]
	assertGlowBand(t, last+50, top)
	if DaysForGlowLevel(1) != 0 {
		t.Fatal("level 1 starts at 0 days")
	}
	if DaysForGlowLevel(top+3) != last {
		t.Fatal("levels past the list stay on the last threshold")
	}
}

func TestGlowLevelFollowsListLength(t *testing.T) {
	saved := append([]int(nil), GlowThresholds...)
	t.Cleanup(func() { GlowThresholds = saved })

	GlowThresholds = []int{2, 5}
	assertGlowBand(t, 1, 1)
	assertGlowBand(t, 2, 2)
	assertGlowBand(t, 4, 2)
	assertGlowBand(t, 5, 3)
	assertGlowBand(t, 9, 3)

	GlowThresholds = []int{10, 20, 30, 40, 50}
	assertGlowBand(t, 9, 1)
	assertGlowBand(t, 10, 2)
	assertGlowBand(t, 49, 5)
	assertGlowBand(t, 50, 6)
	if NextLevelAt(6) != nil {
		t.Fatal("six levels from five thresholds has no next")
	}

	GlowThresholds = nil
	assertGlowBand(t, 100, 1)
	if NextLevelAt(1) != nil {
		t.Fatal("empty list is a single level")
	}
}

func assertGlowBand(t *testing.T, days, level int) {
	t.Helper()
	if got := GlowLevel(days); got != level {
		t.Fatalf("days %d level %d want %d", days, got, level)
	}
	got := NextLevelAt(level)
	idx := level - 1
	if idx < 0 || idx >= len(GlowThresholds) {
		if got != nil {
			t.Fatalf("days %d level %d next %d, want none", days, level, *got)
		}
		return
	}
	if got == nil || *got != GlowThresholds[idx] {
		t.Fatalf("days %d level %d next %#v want %d", days, level, got, GlowThresholds[idx])
	}
}

func TestEffectiveGlowNeverDecreases(t *testing.T) {
	if got := EffectiveGlow(0, 0); got != 1 {
		t.Fatalf("zero stored %d", got)
	}
	top := len(GlowThresholds) + 1
	if got := EffectiveGlow(top, 0); got != top {
		t.Fatalf("max level stays %d", got)
	}
	if top == 1 {
		return
	}
	if got := EffectiveGlow(top-1, 0); got != top-1 {
		t.Fatalf("stored floor %d", got)
	}
	topDays := GlowThresholds[len(GlowThresholds)-1]
	if got := EffectiveGlow(1, topDays); got != top {
		t.Fatalf("computed wins %d", got)
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
