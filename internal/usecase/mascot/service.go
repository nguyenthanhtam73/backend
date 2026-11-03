package mascot

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
)

// Service reads the mascot and derives mood from check-ins and the routine checklist.
type Service struct {
	mascots  *repository.MascotRepository
	checks   *repository.GormSkinCheckRepository
	routines *repository.GormRoutineEntryRepository
	streaks  *repository.GormStreakRepository
}

// NewService constructs the mascot service.
func NewService(
	mascots *repository.MascotRepository,
	checks *repository.GormSkinCheckRepository,
	routines *repository.GormRoutineEntryRepository,
	streaks *repository.GormStreakRepository,
) *Service {
	return &Service{mascots: mascots, checks: checks, routines: routines, streaks: streaks}
}

// Get returns the mascot, creating the free default on first read.
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (dto.MascotResponse, error) {
	var zero dto.MascotResponse
	if s == nil || s.mascots == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	if userID == uuid.Nil {
		return zero, domain.Unauthorized("unauthorized", "missing user")
	}
	row, err := s.ensureRow(ctx, userID, nil)
	if err != nil {
		return zero, err
	}
	return s.view(ctx, userID, row)
}

func (s *Service) activeGrants(ctx context.Context, userID uuid.UUID) (map[string]string, error) {
	if err := s.mascots.EnsureFreeGrants(ctx, userID, time.Now().UTC()); err != nil {
		return nil, err
	}
	return s.mascots.ActiveItemSources(ctx, userID, time.Now().UTC())
}

func grantIDs(sources map[string]string) map[string]struct{} {
	out := make(map[string]struct{}, len(sources))
	for id := range sources {
		out[id] = struct{}{}
	}
	return out
}

// AckGlowSeen sets glow_level_seen to the current level so the celebration shows once.
func (s *Service) AckGlowSeen(ctx context.Context, userID uuid.UUID) (dto.MascotResponse, error) {
	var zero dto.MascotResponse
	if s == nil || s.mascots == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	if userID == uuid.Nil {
		return zero, domain.Unauthorized("unauthorized", "missing user")
	}
	row, err := s.ensureRow(ctx, userID, nil)
	if err != nil {
		return zero, err
	}
	glow, err := s.syncGlow(ctx, userID, row)
	if err != nil {
		return zero, err
	}
	if err := s.mascots.SaveGlowSeen(ctx, userID, glow.Level); err != nil {
		return zero, err
	}
	return s.Get(ctx, userID)
}

// UpdateLoadout saves a lookout of free (or owned) items and returns the fresh view.
func (s *Service) UpdateLoadout(ctx context.Context, userID uuid.UUID, loadout domain.MascotLoadout) (dto.MascotResponse, error) {
	var zero dto.MascotResponse
	if s == nil || s.mascots == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	if userID == uuid.Nil {
		return zero, domain.Unauthorized("unauthorized", "missing user")
	}
	loadout = normalizeLoadout(loadout)
	if err := s.mascots.EnsureCatalog(ctx); err != nil {
		return zero, err
	}
	items, err := s.mascots.ListItems(ctx)
	if err != nil {
		return zero, err
	}
	catalog := map[string]domain.MascotItem{}
	for _, item := range items {
		catalog[item.ID] = item
	}
	sources, err := s.activeGrants(ctx, userID)
	if err != nil {
		return zero, err
	}
	if err := ValidateLoadout(loadout, catalog, grantIDs(sources)); err != nil {
		return zero, err
	}
	level, err := s.glowLevelFor(ctx, userID)
	if err != nil {
		return zero, err
	}
	if err := ValidateGlowWear(loadout, level, sources); err != nil {
		return zero, err
	}
	raw, err := json.Marshal(loadout)
	if err != nil {
		return zero, domain.BadRequest("invalid_loadout", "could not store loadout")
	}
	if _, err := s.ensureRow(ctx, userID, raw); err != nil {
		return zero, err
	}
	if err := s.mascots.SaveLoadout(ctx, userID, raw); err != nil {
		return zero, err
	}
	return s.Get(ctx, userID)
}

func (s *Service) ensureRow(ctx context.Context, userID uuid.UUID, loadout json.RawMessage) (*domain.UserMascot, error) {
	if err := s.mascots.EnsureCatalog(ctx); err != nil {
		return nil, err
	}
	row, err := s.mascots.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row != nil {
		return row, nil
	}
	if len(loadout) == 0 {
		loadout, err = json.Marshal(domain.DefaultMascotLoadout())
		if err != nil {
			return nil, err
		}
	}
	row = &domain.UserMascot{UserID: userID, Loadout: loadout, GlowLevel: 1, GlowLevelSeen: 1}
	if err := s.mascots.Create(ctx, row); err != nil {
		if repository.IsUniqueConflict(err) {
			return s.mascots.Get(ctx, userID)
		}
		return nil, err
	}
	return row, nil
}

func (s *Service) view(ctx context.Context, userID uuid.UUID, row *domain.UserMascot) (dto.MascotResponse, error) {
	var zero dto.MascotResponse
	today := streaktime.Today()
	in := Input{Today: today}

	if s.checks != nil {
		rows, err := s.checks.ListForOwner(ctx, userID, time.Time{}, 2)
		if err != nil {
			return zero, err
		}
		if len(rows) > 0 {
			in.Latest = checkView(&rows[0])
		}
		if len(rows) > 1 {
			in.Previous = checkView(&rows[1])
		}
	}
	if s.routines != nil {
		since := today.AddDate(0, 0, -370)
		rows, err := s.routines.ListForUserSince(ctx, userID, since, 365)
		if err != nil {
			return zero, err
		}
		in.Routines = routineDays(rows, today)
	}
	if s.streaks != nil {
		streak, err := s.streaks.GetByUserID(ctx, userID)
		if err != nil {
			return zero, err
		}
		if streak != nil {
			n := dto.EvaluateStreakView(streak, today).EffectiveStreak
			in.StreakFromRow = &n
		}
	}

	derived := Derive(in)
	loadout := decodeLoadout(row)
	sources, err := s.activeGrants(ctx, userID)
	if err != nil {
		return zero, err
	}
	glow, err := s.syncGlow(ctx, userID, row)
	if err != nil {
		return zero, err
	}
	loadout = visibleLoadout(loadout, sources, glow.Level)
	// derived.Marks are the latest check-in only. Glow fields are attached
	// after that and do not filter skin_marks.
	out := toResponse(loadout, derived)
	out.GlowLevel = glow.Level
	out.GlowPoints = glow.Points
	out.NextLevelPoints = glow.Next
	out.GlowProgress = glow.Progress
	out.NearLevelUp = glow.Near
	out.LevelUp = glow.LevelUp
	return out, nil
}

type glowView struct {
	Level    int
	Points   int
	Next     *int
	Progress float64
	Near     bool
	LevelUp  bool
}

// syncGlow returns the level to show and raises the stored high water when
// points have reached a higher band. It never writes a lower level.
func (s *Service) syncGlow(ctx context.Context, userID uuid.UUID, row *domain.UserMascot) (glowView, error) {
	var zero glowView
	points, err := s.glowPoints(ctx, userID)
	if err != nil {
		return zero, err
	}
	stored := 1
	seen := 1
	if row != nil {
		if row.GlowLevel > stored {
			stored = row.GlowLevel
		}
		if row.GlowLevelSeen > seen {
			seen = row.GlowLevelSeen
		}
	}
	level := EffectiveGlow(stored, points)
	if row != nil && level > row.GlowLevel {
		if err := s.mascots.SaveGlowLevel(ctx, userID, level); err != nil {
			return zero, err
		}
		row.GlowLevel = level
	}
	progress, near := GlowProgress(points, level)
	return glowView{
		Level:    level,
		Points:   points,
		Next:     NextLevelPoints(level),
		Progress: progress,
		Near:     near,
		LevelUp:  level > seen,
	}, nil
}

// glowLevelFor is the level PUT validation uses. It matches syncGlow without
// requiring the mascot row to exist yet.
func (s *Service) glowLevelFor(ctx context.Context, userID uuid.UUID) (int, error) {
	points, err := s.glowPoints(ctx, userID)
	if err != nil {
		return 0, err
	}
	stored := 1
	row, err := s.mascots.Get(ctx, userID)
	if err != nil {
		return 0, err
	}
	if row != nil && row.GlowLevel > stored {
		stored = row.GlowLevel
	}
	return EffectiveGlow(stored, points), nil
}

func (s *Service) glowPoints(ctx context.Context, userID uuid.UUID) (int, error) {
	days, err := s.glowDays(ctx, userID)
	if err != nil {
		return 0, err
	}
	return computeGlowPoints(days), nil
}

// glowDays builds one row per civil day that has a check-in or a finished routine.
// Several check-ins on one day become a single HasCheckIn flag.
func (s *Service) glowDays(ctx context.Context, userID uuid.UUID) ([]glowDay, error) {
	byDay := map[string]*glowDay{}
	touch := func(day time.Time) *glowDay {
		civilDay := streaktime.DateOf(day)
		key := civilDay.Format("2006-01-02")
		row, ok := byDay[key]
		if !ok {
			row = &glowDay{Date: civilDay}
			byDay[key] = row
		}
		return row
	}
	if s.checks != nil {
		dates, err := s.checks.ListDistinctCheckDates(ctx, userID)
		if err != nil {
			return nil, err
		}
		for _, day := range dates {
			if day.IsZero() {
				continue
			}
			touch(day).HasCheckIn = true
		}
	}
	if s.routines != nil {
		rows, err := s.routines.ListAllForUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		for i := range rows {
			row := &rows[i]
			if row.RoutineDate.IsZero() || !routineFullyDone(row.Morning, row.Evening) {
				continue
			}
			touch(row.RoutineDate).RoutineComplete = true
		}
	}
	out := make([]glowDay, 0, len(byDay))
	for _, day := range byDay {
		out = append(out, *day)
	}
	return out, nil
}

// visibleLoadout drops equipped items whose grant is missing, expired, or
// still locked behind the glow level. Required slots fall back to the free
// default when that grant is still active. The free headband and star clip
// stay in the stored default and appear here once the level unlocks them.
// A shop grant shows at any level.
func visibleLoadout(loadout domain.MascotLoadout, sources map[string]string, level int) domain.MascotLoadout {
	owned := grantIDs(sources)
	loadout.Body = keepOrDefault(loadout.Body, domain.MascotItemBodyChibi, owned)
	loadout.Hair = keepOrDefault(loadout.Hair, domain.MascotItemHairSleepBuns, owned)
	loadout.Outfit = keepOrDefault(loadout.Outfit, domain.MascotItemOutfitCozyPajamas, owned)
	raw := loadout.Accessories
	if len(raw) == 0 && loadout.Accessory != "" {
		raw = []string{loadout.Accessory}
	}
	kept := make([]string, 0, len(raw))
	for _, id := range raw {
		if accessoryShown(id, sources, level) {
			kept = append(kept, id)
		}
	}
	// Every equipped accessory expired. Put the free pair back when still
	// owned and unlocked. An explicit empty list stays empty so the user
	// can take them off. A locked free accessory is not put back.
	if len(kept) == 0 && len(raw) > 0 {
		for _, id := range []string{domain.MascotItemAccessoryPinkHeadband, domain.MascotItemAccessoryStarClip} {
			if accessoryShown(id, sources, level) {
				kept = append(kept, id)
			}
		}
	}
	loadout.Accessories = kept
	if len(kept) > 0 {
		loadout.Accessory = kept[0]
	} else {
		loadout.Accessory = ""
	}
	rooms := make([]string, 0, len(loadout.RoomItems))
	for _, id := range loadout.RoomItems {
		if _, ok := owned[id]; ok {
			rooms = append(rooms, id)
		}
	}
	loadout.RoomItems = rooms
	return loadout
}

func accessoryShown(id string, sources map[string]string, level int) bool {
	source, ok := sources[id]
	if !ok {
		return false
	}
	return AccessoryWearable(id, level, source)
}

func keepOrDefault(id, fallback string, owned map[string]struct{}) string {
	if _, ok := owned[id]; ok {
		return id
	}
	if _, ok := owned[fallback]; ok {
		return fallback
	}
	return id
}

func toResponse(loadout domain.MascotLoadout, derived Result) dto.MascotResponse {
	if loadout.RoomItems == nil {
		loadout.RoomItems = []string{}
	}
	if loadout.Accessories == nil {
		loadout.Accessories = []string{}
	}
	out := dto.MascotResponse{
		Loadout:       loadout,
		Mood:          derived.Mood,
		LineVI:        derived.LineVI,
		LineEN:        derived.LineEN,
		SkinMarks:     toMarks(derived.Marks),
		SkinMarksPrev: toMarks(derived.MarksPrev),
		RoutineToday: dto.MascotRoutineToday{
			AMDone:  derived.RoutineToday.AMDone,
			AMTotal: derived.RoutineToday.AMTotal,
			PMDone:  derived.RoutineToday.PMDone,
			PMTotal: derived.RoutineToday.PMTotal,
		},
		StreakDays: derived.StreakDays,
		MissedDays: derived.MissedDays,
		MessyLevel: derived.MessyLevel,
	}
	if derived.LastCheckIn != nil {
		s := derived.LastCheckIn.UTC().Format(time.RFC3339)
		out.LastCheckInAt = &s
	}
	if derived.LastStepDone != "" {
		step := derived.LastStepDone
		out.LastStepDone = &step
	}
	return out
}

func toMarks(in []Mark) []dto.MascotSkinMark {
	out := make([]dto.MascotSkinMark, 0, len(in))
	for _, m := range in {
		out = append(out, dto.MascotSkinMark{ID: m.ID, Level: m.Level})
	}
	return out
}

func decodeLoadout(row *domain.UserMascot) domain.MascotLoadout {
	fallback := domain.DefaultMascotLoadout()
	if row == nil || len(row.Loadout) == 0 {
		return fallback
	}
	var loadout domain.MascotLoadout
	if err := json.Unmarshal(row.Loadout, &loadout); err != nil {
		return fallback
	}
	return normalizeLoadout(loadout)
}

func normalizeLoadout(in domain.MascotLoadout) domain.MascotLoadout {
	in.Body = strings.TrimSpace(in.Body)
	in.Hair = strings.TrimSpace(in.Hair)
	in.Outfit = strings.TrimSpace(in.Outfit)
	in.Accessory = strings.TrimSpace(in.Accessory)
	if in.RoomItems == nil {
		in.RoomItems = []string{}
	}
	items := make([]string, 0, len(in.RoomItems))
	for _, id := range in.RoomItems {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		items = append(items, id)
	}
	in.RoomItems = items
	acc := make([]string, 0, len(in.Accessories))
	seen := map[string]struct{}{}
	for _, id := range in.Accessories {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		acc = append(acc, id)
	}
	if len(acc) == 0 && in.Accessory != "" {
		acc = []string{in.Accessory}
	}
	in.Accessories = acc
	if len(acc) > 0 {
		in.Accessory = acc[0]
	} else {
		in.Accessory = ""
	}
	return in
}

func routineDays(rows []domain.RoutineEntry, today time.Time) []RoutineDay {
	out := make([]RoutineDay, 0, len(rows)+1)
	hasToday := false
	var latestBefore *domain.RoutineEntry
	var latestBeforeDate time.Time
	for i := range rows {
		row := &rows[i]
		d := civil(row.RoutineDate)
		out = append(out, RoutineDay{
			Date:    d,
			Morning: decodeSteps(row.Morning),
			Evening: decodeSteps(row.Evening),
		})
		if dateKey(d) == dateKey(today) {
			hasToday = true
			continue
		}
		if d.Before(today) && (latestBefore == nil || d.After(latestBeforeDate)) {
			latestBefore = row
			latestBeforeDate = d
		}
	}
	if !hasToday && latestBefore != nil {
		out = append(out, RoutineDay{
			Date:    civil(today),
			Morning: clearDone(decodeSteps(latestBefore.Morning)),
			Evening: clearDone(decodeSteps(latestBefore.Evening)),
		})
	}
	return out
}

func clearDone(steps []Step) []Step {
	out := make([]Step, len(steps))
	for i, s := range steps {
		s.Completed = false
		out[i] = s
	}
	return out
}

func decodeSteps(raw json.RawMessage) []Step {
	if len(raw) == 0 {
		return nil
	}
	var steps []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Completed bool   `json:"completed"`
	}
	if err := json.Unmarshal(raw, &steps); err != nil {
		var lines []string
		if err := json.Unmarshal(raw, &lines); err != nil {
			return nil
		}
		out := make([]Step, 0, len(lines))
		for i, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			out = append(out, Step{ID: "legacy-" + strings.TrimSpace(itoa(i)), Completed: false})
		}
		return out
	}
	out := make([]Step, 0, len(steps))
	for i, s := range steps {
		title := strings.TrimSpace(s.Title)
		id := strings.TrimSpace(s.ID)
		if title == "" && id == "" {
			continue
		}
		if id == "" {
			id = "step-" + itoa(i)
		}
		out = append(out, Step{ID: id, Completed: s.Completed})
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func checkView(c *domain.SkinCheck) *CheckView {
	if c == nil {
		return nil
	}
	conds, _ := dto.DecodeStringSlice(c.Conditions)
	syms, _ := dto.DecodeStringSlice(c.Symptoms)
	at := c.CreatedAt
	if at.IsZero() {
		at = c.CheckDate
	}
	view := &CheckView{
		At:         at,
		Conditions: conds,
		Symptoms:   syms,
	}
	if c.Analysis != nil && len(c.Analysis.SkinScores) > 0 {
		view.Zones, view.Gauges = parseScores(c.Analysis.SkinScores)
	}
	return view
}

func parseScores(raw json.RawMessage) ([]ZoneCue, Gauges) {
	var scores map[string]any
	if err := json.Unmarshal(raw, &scores); err != nil || scores == nil {
		return nil, Gauges{}
	}
	g := Gauges{
		Overall:   floatPtr(scores["overall"]),
		Hydration: floatPtr(scores["hydration"]),
		Clarity:   floatPtr(scores["clarity"]),
		Barrier:   floatPtr(scores["barrier"]),
	}
	var zones []ZoneCue
	zones = appendCues(zones, scores["zone_notes"], "note")
	zones = appendCues(zones, scores["vision_zone_observations"], "cue")
	return zones, g
}

func appendCues(dst []ZoneCue, raw any, textKey string) []ZoneCue {
	arr, ok := raw.([]any)
	if !ok {
		return dst
	}
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		text, _ := m[textKey].(string)
		sev, _ := m["severity"].(string)
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		dst = append(dst, ZoneCue{Text: text, Severity: sev})
	}
	return dst
}

func floatPtr(v any) *float64 {
	switch n := v.(type) {
	case float64:
		f := n
		return &f
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return nil
		}
		return &f
	default:
		return nil
	}
}
