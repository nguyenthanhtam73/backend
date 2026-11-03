package mascot

import (
	"encoding/json"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
)

// GlowConfig is the only place glow points and level bands are defined.
// Thresholds are point totals where levels 2–6 start: 9, 28, 68, 99, 168.
// Level 1 is every total below the first entry. The highest level is
// len(Thresholds)+1, so next_level_points is null there.
// CheckInPoints is awarded once per Asia/Ho_Chi_Minh day with a check-in.
// RoutineBonus is awarded once per day whose stored routine has every step ticked.
// DailyCap is the most one day can add. NearLevelFraction is the progress
// toward the next level that sets near_level_up.
type GlowConfigType struct {
	Thresholds        []int
	CheckInPoints     int
	RoutineBonus      int
	DailyCap          int
	NearLevelFraction float64
}

var GlowConfig = GlowConfigType{
	Thresholds:        []int{9, 28, 68, 99, 168},
	CheckInPoints:     1,
	RoutineBonus:      1,
	DailyCap:          2,
	NearLevelFraction: 0.8,
}

const (
	glowUnlockHeadband = 3
	glowUnlockStarClip = 4
)

// glowDay is one Asia/Ho_Chi_Minh civil day.
// HasCheckIn is true when that day has at least one skin check (photo or skip).
// RoutineComplete is true when that day's stored routine has every step ticked.
// A day with both flags false is a missed day. Decay is not applied yet, but
// those days are part of this input so a later rule can read them.
type glowDay struct {
	Date            time.Time
	HasCheckIn      bool
	RoutineComplete bool
}

// GlowLevel maps a glow-point total onto GlowConfig.Thresholds.
// One level per entry, plus level 1 below the first entry.
func GlowLevel(points int) int {
	level := 1
	for _, at := range GlowConfig.Thresholds {
		if points < at {
			break
		}
		level++
	}
	return level
}

// NextLevelPoints is the GlowConfig.Thresholds entry where the next level starts.
// Nil at the highest level (and for a level below 1). It is that point total,
// not the points still remaining.
func NextLevelPoints(level int) *int {
	idx := level - 1
	if idx < 0 || idx >= len(GlowConfig.Thresholds) {
		return nil
	}
	n := GlowConfig.Thresholds[idx]
	return &n
}

// PointsForGlowLevel is the smallest point total that reaches level.
// Level 1 is 0. A level past the list returns the last threshold.
func PointsForGlowLevel(level int) int {
	if level <= 1 || len(GlowConfig.Thresholds) == 0 {
		return 0
	}
	idx := level - 2
	if idx >= len(GlowConfig.Thresholds) {
		return GlowConfig.Thresholds[len(GlowConfig.Thresholds)-1]
	}
	if GlowConfig.Thresholds[idx] < 0 {
		return 0
	}
	return GlowConfig.Thresholds[idx]
}

// GlowProgress is the fraction of the way from 0 to the next level's point
// total, clamped to 0..1. At the highest level it is 1 and near is false.
// near is true when the fraction is at least NearLevelFraction.
func GlowProgress(points, level int) (progress float64, near bool) {
	next := NextLevelPoints(level)
	if next == nil || *next <= 0 {
		return 1, false
	}
	progress = float64(points) / float64(*next)
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	return progress, progress >= GlowConfig.NearLevelFraction
}

// EffectiveGlow is max(stored high water, the level implied by points).
// A stored value below 1 is treated as the starting level.
// This still never returns a level below the stored one. Decay, when added
// inside computeGlowPoints, may lower the computed level; the stored high
// water is what keeps the level from falling until that product change.
func EffectiveGlow(stored, points int) int {
	if stored < 1 {
		stored = 1
	}
	computed := GlowLevel(points)
	if stored > computed {
		return stored
	}
	return computed
}

// computeGlowPoints is the single scoring function. It sums per-day points
// from stored days. The same days always produce the same total.
//
// Decay is not applied. applyGlowDecay is the plug-in for a later rule:
// from the 3rd missed day, subtract 1 point per day. That may lower the
// computed level. Unlocked items stay owned either way.
func computeGlowPoints(days []glowDay) int {
	merged := map[string]glowDay{}
	for _, day := range days {
		if day.Date.IsZero() {
			continue
		}
		key := streaktime.DateOf(day.Date).Format("2006-01-02")
		prev := merged[key]
		prev.Date = streaktime.DateOf(day.Date)
		prev.HasCheckIn = prev.HasCheckIn || day.HasCheckIn
		prev.RoutineComplete = prev.RoutineComplete || day.RoutineComplete
		merged[key] = prev
	}
	earned := 0
	for _, day := range merged {
		earned += glowPointsForDay(day)
	}
	return applyGlowDecay(earned, days)
}

func glowPointsForDay(day glowDay) int {
	n := 0
	if day.HasCheckIn {
		n += GlowConfig.CheckInPoints
	}
	if day.RoutineComplete {
		n += GlowConfig.RoutineBonus
	}
	if cap := GlowConfig.DailyCap; n > cap {
		n = cap
	}
	if n < 0 {
		return 0
	}
	return n
}

// applyGlowDecay returns points unchanged. days is the civil-day series a
// future missed-day penalty will read. Do not subtract here yet.
func applyGlowDecay(points int, days []glowDay) int {
	_ = days
	return points
}

// routineFullyDone reports whether every stored step is ticked.
// An empty checklist is not complete. Morning and evening both count.
func routineFullyDone(morning, evening json.RawMessage) bool {
	steps := append(decodeSteps(morning), decodeSteps(evening)...)
	if len(steps) == 0 {
		return false
	}
	for _, step := range steps {
		if !step.Completed {
			return false
		}
	}
	return true
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
