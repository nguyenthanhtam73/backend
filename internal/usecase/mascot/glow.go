package mascot

import (
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
)

// GlowThresholds is the distinct check-in day count where each level after 1 starts.
// Level 1 is 0 days, then 2 at 3, 3 at 10, 4 at 25, 5 at 50, 6 at 90.
// The list is ascending. The highest level is len(GlowThresholds)+1.
// Keep this as the only copy of the numbers. GlowLevel, NextLevelAt, and the tests read it.
var GlowThresholds = []int{3, 10, 25, 50, 90}

const (
	glowUnlockHeadband = 3
	glowUnlockStarClip = 4
)

// GlowLevel maps a distinct check-in day count onto GlowThresholds.
// One level per entry, plus level 1 below the first entry.
func GlowLevel(days int) int {
	level := 1
	for _, at := range GlowThresholds {
		if days < at {
			break
		}
		level++
	}
	return level
}

// NextLevelAt is the GlowThresholds entry where the next level starts.
// Nil at the highest level (and for a level below 1). It is that day total,
// not the days still remaining.
func NextLevelAt(level int) *int {
	idx := level - 1
	if idx < 0 || idx >= len(GlowThresholds) {
		return nil
	}
	n := GlowThresholds[idx]
	return &n
}

// DaysForGlowLevel is the smallest distinct-day count that reaches level.
// Level 1 is 0. A level past the list returns the last threshold.
func DaysForGlowLevel(level int) int {
	if level <= 1 || len(GlowThresholds) == 0 {
		return 0
	}
	idx := level - 2
	if idx >= len(GlowThresholds) {
		return GlowThresholds[len(GlowThresholds)-1]
	}
	if GlowThresholds[idx] < 0 {
		return 0
	}
	return GlowThresholds[idx]
}

// EffectiveGlow is max(stored high water, the level implied by days).
// A stored value below 1 is treated as the starting level.
func EffectiveGlow(stored, days int) int {
	if stored < 1 {
		stored = 1
	}
	computed := GlowLevel(days)
	if stored > computed {
		return stored
	}
	return computed
}

// distinctCheckInDays counts calendar days in Asia/Ho_Chi_Minh.
// check_date is stored as that civil day (streaktime.DateOf). Several
// timestamps on one Vietnam day count once. Zero times are ignored.
func distinctCheckInDays(dates []time.Time) int {
	seen := make(map[string]struct{}, len(dates))
	for _, d := range dates {
		if d.IsZero() {
			continue
		}
		day := streaktime.DateOf(d)
		seen[day.Format("2006-01-02")] = struct{}{}
	}
	return len(seen)
}

// AccessoryWearable reports whether this grant may be equipped at level.
// The free pink headband unlocks at 3 and the free star clip at 4.
// source=shop wears either at any level. Any other source, including free,
// waits for the level. Items with no glow rule are always wearable.
func AccessoryWearable(itemID string, level int, source string) bool {
	need := glowUnlockLevel(itemID)
	if need == 0 {
		return true
	}
	if source == domain.MascotSourceShop {
		return true
	}
	return level >= need
}

func glowUnlockLevel(itemID string) int {
	switch itemID {
	case domain.MascotItemAccessoryPinkHeadband:
		return glowUnlockHeadband
	case domain.MascotItemAccessoryStarClip:
		return glowUnlockStarClip
	default:
		return 0
	}
}

// ValidateGlowWear rejects equipped accessories the glow level has not unlocked.
// Ownership is checked separately. A shop grant skips the level.
func ValidateGlowWear(loadout domain.MascotLoadout, level int, sources map[string]string) error {
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(loadout.Accessories)+1)
	if loadout.Accessory != "" {
		ids = append(ids, loadout.Accessory)
	}
	ids = append(ids, loadout.Accessories...)
	for _, id := range ids {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		if AccessoryWearable(id, level, sources[id]) {
			continue
		}
		return domain.BadRequest("item_locked", "that accessory unlocks at a higher glow level")
	}
	return nil
}
