package mascot

import (
	"sort"
	"strings"
	"time"
)

const (
	levelLight  = "light"
	levelMedium = "medium"
	levelHeavy  = "heavy"

	moodHappy   = "happy"
	moodSparkle = "sparkle"
	moodHug     = "hug_return"

	// A higher gauge is a healthier read, so a low score is a heavier mark.
	gaugeHeavyBelow  = 0.40
	gaugeMediumBelow = 0.67
	improveDelta     = 0.03
)

// Step is one checklist row. A finished session is every morning step or every
// evening step completed. One tick does not finish a session.
type Step struct {
	ID        string
	Completed bool
}

// RoutineDay is one civil day of the existing AM/PM checklist.
type RoutineDay struct {
	Date    time.Time
	Morning []Step
	Evening []Step
}

// ZoneCue is one analysis note. Severity is the model's mild/moderate/pronounced.
type ZoneCue struct {
	Text     string
	Severity string
}

// Gauges are the check-in soft scores. Nil means that score was absent.
type Gauges struct {
	Overall   *float64
	Hydration *float64
	Clarity   *float64
	Barrier   *float64
}

// CheckView is the latest (or previous) check-in, without stored mood.
type CheckView struct {
	At         time.Time
	Conditions []string
	Symptoms   []string
	Zones      []ZoneCue
	Gauges     Gauges
}

// Input is everything Derive needs. StreakFromRow is set when a streak row
// exists, including when the effective streak is 0. Nil falls back to routine days.
type Input struct {
	Today         time.Time
	Routines      []RoutineDay
	Latest        *CheckView
	Previous      *CheckView
	StreakFromRow *int
}

// Mark is one concern. Level is light, medium, or heavy.
type Mark struct {
	ID    string
	Level string
}

// RoutineCounts is today's checklist progress.
type RoutineCounts struct {
	AMDone  int
	AMTotal int
	PMDone  int
	PMTotal int
}

// Result is the derived mascot state. Loadout is not part of it.
type Result struct {
	Mood         string
	LineVI       string
	LineEN       string
	Marks        []Mark
	MarksPrev    []Mark
	LastCheckIn  *time.Time
	RoutineToday RoutineCounts
	LastStepDone string
	StreakDays   int
	MissedDays   int
	MessyLevel   int
}

// Derive picks mood, lines, marks, and missed days.
// A finished session today (all AM or all PM) resets missed days and messy level.
// Coming back from a miss is hug_return; the line says happy comes next.
// Otherwise a miss uses the stage table. Skin marks always come from the latest
// check-in. Glow points are not an input and must not add or remove a mark.
func Derive(in Input) Result {
	today := civil(in.Today)
	marks := marksFrom(in.Latest)
	prev := marksFrom(in.Previous)
	counts, lastStep := routineToday(in.Routines, today)

	missed, sessionToday := missedDays(in.Routines, today)
	before := 0
	if sessionToday {
		before = missedBefore(in.Routines, today)
		missed = 0
	}

	res := Result{
		Marks:        marks,
		MarksPrev:    prev,
		RoutineToday: counts,
		LastStepDone: lastStep,
		MissedDays:   missed,
		MessyLevel:   MessyLevel(missed),
	}
	if in.Latest != nil && !in.Latest.At.IsZero() {
		at := in.Latest.At.UTC()
		res.LastCheckIn = &at
	}
	switch {
	case sessionToday && before >= 1:
		res.Mood = moodHug
		res.LineVI, res.LineEN = "Ôm cái đã, rồi Bé Da vui.", "A hug first, then Bé Da is happy."
	case missed >= 1:
		st, _ := stageFor(missed)
		res.Mood = st.Mood
		res.LineVI = st.LineVI
		res.LineEN = st.LineEN
	default:
		res.Mood, res.LineVI, res.LineEN = skinMood(in.Latest, in.Previous, marks, prev)
	}
	if in.StreakFromRow != nil {
		res.StreakDays = *in.StreakFromRow
	} else {
		res.StreakDays = routineStreak(in.Routines, today)
	}
	return res
}

func skinMood(latest, prev *CheckView, marks, prevMarks []Mark) (string, string, string) {
	if improved(latest, prev, marks, prevMarks) {
		vi, en := sparkleLines(marks)
		return moodSparkle, vi, en
	}
	if id := dominantConcern(marks); id != "" {
		vi, en := concernLines(marks)
		return id, vi, en
	}
	vi, en := happyLines(marks)
	return moodHappy, vi, en
}

func improved(latest, prev *CheckView, marks, prevMarks []Mark) bool {
	if latest == nil || prev == nil {
		return false
	}
	if rank(levelOf(marks, "acne")) > rank(levelOf(prevMarks, "acne")) {
		return false
	}
	if levelOf(prevMarks, "acne") == "" && levelOf(marks, "acne") != "" {
		return false
	}
	if latest.Gauges.Overall != nil && prev.Gauges.Overall != nil {
		delta := *latest.Gauges.Overall - *prev.Gauges.Overall
		if delta > improveDelta {
			return true
		}
		if delta < -improveDelta {
			return false
		}
	}
	return marksLighter(marks, prevMarks)
}

func marksLighter(cur, prev []Mark) bool {
	if len(prev) == 0 {
		return false
	}
	old := make(map[string]string, len(prev))
	for _, m := range prev {
		old[m.ID] = m.Level
	}
	better := false
	seen := map[string]struct{}{}
	for _, m := range cur {
		seen[m.ID] = struct{}{}
		was, ok := old[m.ID]
		if !ok || rank(m.Level) > rank(was) {
			return false
		}
		if rank(m.Level) < rank(was) {
			better = true
		}
	}
	if len(seen) < len(old) {
		better = true
	}
	return better
}

var moodConcerns = []string{"acne", "red", "dark_spot", "oily", "dry"}

func dominantConcern(marks []Mark) string {
	bestID := ""
	bestRank := 0
	order := map[string]int{}
	for i, id := range moodConcerns {
		order[id] = i
	}
	for _, m := range marks {
		r := rank(m.Level)
		if r == 0 {
			continue
		}
		pos, ok := order[m.ID]
		if !ok {
			continue
		}
		if bestID == "" || r > bestRank || (r == bestRank && pos < order[bestID]) {
			bestID = m.ID
			bestRank = r
		}
	}
	return bestID
}

func happyLines(marks []Mark) (string, string) {
	a, b := topPhrases(marks)
	if a.vi == "" {
		return "Hôm nay da ổn.", "Skin looks okay today."
	}
	if b.vi == "" {
		return "Hôm nay ổn, vẫn thấy " + a.vi + ".", "Today looks okay, still seeing " + a.en + "."
	}
	return "Hôm nay ổn, vẫn thấy " + a.vi + " và " + b.vi + ".",
		"Today looks okay, still seeing " + a.en + " and " + b.en + "."
}

func sparkleLines(marks []Mark) (string, string) {
	a, b := topPhrases(marks)
	if a.vi == "" {
		return "Da khá hơn lần trước.", "Skin looks a bit better than last time."
	}
	if b.vi == "" {
		return "Da khá hơn lần trước, vẫn thấy " + a.vi + ".",
			"Skin looks a bit better than last time, still seeing " + a.en + "."
	}
	return "Da khá hơn lần trước, vẫn thấy " + a.vi + " và " + b.vi + ".",
		"Skin looks a bit better than last time, still seeing " + a.en + " and " + b.en + "."
}

func concernLines(marks []Mark) (string, string) {
	a, b := topPhrases(marks)
	if a.vi == "" {
		return "Hôm nay da ổn.", "Skin looks okay today."
	}
	if b.vi == "" {
		return "Hôm nay thấy " + a.vi + ".", "Today shows " + a.en + "."
	}
	return "Hôm nay thấy " + a.vi + " và " + b.vi + ".",
		"Today shows " + a.en + " and " + b.en + "."
}

type phrase struct{ vi, en string }

func topPhrases(marks []Mark) (phrase, phrase) {
	ranked := append([]Mark(nil), marks...)
	sort.SliceStable(ranked, func(i, j int) bool {
		ri, rj := rank(ranked[i].Level), rank(ranked[j].Level)
		if ri != rj {
			return ri > rj
		}
		return concernPos(ranked[i].ID) < concernPos(ranked[j].ID)
	})
	var out []phrase
	for _, m := range ranked {
		p, ok := phrases(m.ID)
		if !ok {
			continue
		}
		out = append(out, p)
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return phrase{}, phrase{}
	}
	if len(out) == 1 {
		return out[0], phrase{}
	}
	return out[0], out[1]
}

func phrases(id string) (phrase, bool) {
	switch id {
	case "acne":
		return phrase{"nốt mới", "a few spots"}, true
	case "oily":
		return phrase{"da hơi bóng", "a bit of shine"}, true
	case "dry":
		return phrase{"da hơi khô", "a little dryness"}, true
	case "red":
		return phrase{"da hơi đỏ", "a little redness"}, true
	case "dark_spot":
		return phrase{"vết thâm nhẹ", "a faint mark"}, true
	case "dull":
		return phrase{"da hơi xỉn", "a bit of dullness"}, true
	case "pores":
		return phrase{"lỗ chân lông rõ", "more visible pores"}, true
	default:
		return phrase{}, false
	}
}

var concernOrder = []string{"acne", "red", "dark_spot", "oily", "dry", "dull", "pores"}

func concernPos(id string) int {
	for i, known := range concernOrder {
		if known == id {
			return i
		}
	}
	return len(concernOrder)
}

func marksFrom(c *CheckView) []Mark {
	if c == nil {
		return []Mark{}
	}
	ids := concernIDs(c.Conditions, c.Symptoms)
	out := make([]Mark, 0, len(ids))
	for _, id := range ids {
		out = append(out, Mark{ID: id, Level: levelFor(id, c)})
	}
	return out
}

func concernIDs(conditions, symptoms []string) []string {
	seen := map[string]struct{}{}
	var known []string
	var extra []string
	add := func(tag string) {
		id, ok := canonicalConcern(tag)
		if !ok {
			return
		}
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		if concernPos(id) < len(concernOrder) {
			known = append(known, id)
			return
		}
		extra = append(extra, id)
	}
	for _, tag := range conditions {
		add(tag)
	}
	for _, tag := range symptoms {
		add(tag)
	}
	sort.SliceStable(known, func(i, j int) bool { return concernPos(known[i]) < concernPos(known[j]) })
	sort.Strings(extra)
	return append(known, extra...)
}

func canonicalConcern(tag string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(tag)) {
	case "", "normal", "combo":
		return "", false
	case "breakout", "new_breakouts":
		return "acne", true
	case "oily":
		return "oily", true
	case "dry", "dehydrated":
		return "dry", true
	case "redness", "inflammation", "sensitive":
		return "red", true
	case "hyperpigmentation", "pih", "post_acne_marks":
		return "dark_spot", true
	case "dull":
		return "dull", true
	case "clogged_pores", "large_pores":
		return "pores", true
	default:
		id := strings.ToLower(strings.TrimSpace(tag))
		if id == "" {
			return "", false
		}
		return id, true
	}
}

func levelFor(id string, c *CheckView) string {
	best := ""
	for _, z := range c.Zones {
		if !zoneMatches(id, z.Text) {
			continue
		}
		if sev := mapSeverity(z.Severity); sev != "" {
			best = maxLevel(best, sev)
		}
	}
	if best != "" {
		return best
	}
	if g := gaugeFor(id, c.Gauges); g != nil {
		return levelFromGauge(*g)
	}
	return levelLight
}

func zoneMatches(concern, text string) bool {
	low := strings.ToLower(text)
	switch concern {
	case "acne":
		return hasAny(low, "mụn", "mun", "acne", "breakout", "whitehead", "blackhead", "nốt")
	case "oily":
		return hasAny(low, "bóng", "dầu", "oily", "shiny", "shine")
	case "dry":
		// "không" contains "khô", so strip that word before looking for dryness.
		cleaned := strings.ReplaceAll(low, "không", " ")
		cleaned = strings.ReplaceAll(cleaned, "khong", " ")
		return hasAny(cleaned, "khô", "dry", "flaky", "dehydr")
	case "red":
		return hasAny(low, "đỏ", "red", "kích", "kich")
	case "dark_spot":
		return hasAny(low, "thâm", "tham", "nám", "pigment", "dark spot", "pih")
	case "dull":
		return hasAny(low, "xỉn", "dull")
	case "pores":
		return hasAny(low, "lỗ chân lông", "lo chan long", "pore")
	default:
		return strings.Contains(low, strings.ToLower(concern))
	}
}

func hasAny(s string, parts ...string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func mapSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mild", "light":
		return levelLight
	case "moderate", "medium":
		return levelMedium
	case "pronounced", "severe", "dense", "heavy":
		return levelHeavy
	default:
		return ""
	}
}

func levelFromGauge(score float64) string {
	switch {
	case score < gaugeHeavyBelow:
		return levelHeavy
	case score < gaugeMediumBelow:
		return levelMedium
	default:
		return levelLight
	}
}

func gaugeFor(id string, g Gauges) *float64 {
	switch id {
	case "dry":
		return g.Hydration
	case "red":
		return g.Barrier
	case "oily":
		return g.Overall
	case "acne", "dark_spot", "dull", "pores":
		return g.Clarity
	default:
		return g.Overall
	}
}

func levelOf(marks []Mark, id string) string {
	for _, m := range marks {
		if m.ID == id {
			return m.Level
		}
	}
	return ""
}

func rank(level string) int {
	switch level {
	case levelHeavy:
		return 3
	case levelMedium:
		return 2
	case levelLight:
		return 1
	default:
		return 0
	}
}

func maxLevel(a, b string) string {
	if rank(b) > rank(a) {
		return b
	}
	return a
}

func routineToday(rows []RoutineDay, today time.Time) (RoutineCounts, string) {
	day, ok := indexRoutines(rows)[dateKey(today)]
	if !ok {
		return RoutineCounts{}, ""
	}
	amDone, amTotal := tally(day.Morning)
	pmDone, pmTotal := tally(day.Evening)
	return RoutineCounts{AMDone: amDone, AMTotal: amTotal, PMDone: pmDone, PMTotal: pmTotal}, lastDone(day)
}

func tally(steps []Step) (done, total int) {
	total = len(steps)
	for _, s := range steps {
		if s.Completed {
			done++
		}
	}
	return done, total
}

func lastDone(day RoutineDay) string {
	last := ""
	for _, s := range day.Morning {
		if s.Completed && s.ID != "" {
			last = s.ID
		}
	}
	for _, s := range day.Evening {
		if s.Completed && s.ID != "" {
			last = s.ID
		}
	}
	return last
}

func missedDays(rows []RoutineDay, today time.Time) (int, bool) {
	today = civil(today)
	by := indexRoutines(rows)
	if sessionDone(by[dateKey(today)]) {
		return 0, true
	}
	earliest, ok := earliestDate(rows)
	if !ok {
		return 0, false
	}
	n := 0
	for d := today.AddDate(0, 0, -1); !d.Before(earliest); d = d.AddDate(0, 0, -1) {
		if sessionDone(by[dateKey(d)]) {
			break
		}
		n++
		if n > 4000 {
			break
		}
	}
	return n, false
}

func missedBefore(rows []RoutineDay, today time.Time) int {
	key := dateKey(today)
	kept := make([]RoutineDay, 0, len(rows))
	for _, row := range rows {
		if dateKey(row.Date) == key {
			continue
		}
		kept = append(kept, row)
	}
	n, _ := missedDays(kept, today)
	return n
}

func routineStreak(rows []RoutineDay, today time.Time) int {
	by := indexRoutines(rows)
	start := civil(today)
	if !sessionDone(by[dateKey(start)]) {
		start = start.AddDate(0, 0, -1)
	}
	n := 0
	for {
		if !sessionDone(by[dateKey(start)]) {
			break
		}
		n++
		start = start.AddDate(0, 0, -1)
		if n > 3660 {
			break
		}
	}
	return n
}

func sessionDone(day RoutineDay) bool {
	return slotDone(day.Morning) || slotDone(day.Evening)
}

func slotDone(steps []Step) bool {
	if len(steps) == 0 {
		return false
	}
	for _, s := range steps {
		if !s.Completed {
			return false
		}
	}
	return true
}

func indexRoutines(rows []RoutineDay) map[string]RoutineDay {
	out := make(map[string]RoutineDay, len(rows))
	for _, row := range rows {
		out[dateKey(row.Date)] = row
	}
	return out
}

func earliestDate(rows []RoutineDay) (time.Time, bool) {
	var earliest time.Time
	found := false
	for _, row := range rows {
		d := civil(row.Date)
		if !found || d.Before(earliest) {
			earliest = d
			found = true
		}
	}
	return earliest, found
}

func civil(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func dateKey(t time.Time) string {
	return civil(t).Format("2006-01-02")
}
