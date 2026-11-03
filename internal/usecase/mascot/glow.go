package mascot

import (
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
)

// Glow bands from distinct check-in days (Asia/Ho_Chi_Minh).
// 1: 0–2, 2: 3–6, 3: 7–13, 4: 14–20, 5: 21+.
const (
	glowLevel2Days = 3
	glowLevel3Days = 7
	glowLevel4Days = 14
	glowLevel5Days = 21

	glowUnlockHeadband = 3
	glowUnlockStarClip = 4
)

// GlowLevel maps a distinct check-in day count to a level from 1 to 5.
func GlowLevel(days int) int {
	switch {
	case days >= glowLevel5Days:
		return 5
	case days >= glowLevel4Days:
		return 4
	case days >= glowLevel3Days:
		return 3
	case days >= glowLevel2Days:
		return 2
	default:
		return 1
	}
}

// NextLevelAt is the distinct-day total where the next level starts.
// Nil at level 5. It is that milestone (3, 7, 14, 21), not the days still remaining.
func NextLevelAt(level int) *int {
	var n int
	switch level {
	case 1:
		n = glowLevel2Days
	case 2:
		n = glowLevel3Days
	case 3:
		n = glowLevel4Days
	case 4:
		n = glowLevel5Days
	default:
		return nil
	}
	return &n
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
