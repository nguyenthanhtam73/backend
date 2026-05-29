// Package ai — user_memory.go assembles the "personalised memory" block we
// inject into every AI Coach call (daily check-in feedback + routine suggest).
//
// Why a dedicated helper:
//   - The existing context builders (BuildSkinProfileContext,
//     BuildRecentCheckInsContext, BuildPriorFeedbackContext) each handle one
//     slice of context and were called ad-hoc from a few call sites. As we add
//     more memory channels (routine completion, prior AI feedback), the call
//     site code kept growing and started drifting out of sync between the
//     skin-check pipeline and the routine-suggest pipeline.
//   - This file centralises the "what does the AI know about THIS user?" logic
//     in one place. Both call sites now ask for a single string and trust the
//     helper to gather + truncate + format it.
//
// Token budget:
//   - We deliberately keep this block compact (~1.5–2.5k tokens worst case).
//     SkinChecks are summarised to date + tags + symptoms + a short AI line;
//     feedback is capped to 12 rows; routine completion is one line.
//   - When a repo is missing or the lookup fails, the corresponding section is
//     simply skipped — the coach still gets *something* useful instead of
//     erroring out.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
)

// UserMemoryDeps bundles the repositories needed to build the memory block.
//
// Every field is optional — a nil repo means "skip that section". This lets
// us wire the helper into call sites that don't (yet) have all repos handy
// without forcing a refactor cascade.
//
// Cache is also optional — when set, BuildUserMemoryContext returns a cached
// copy on hit and Put()s a fresh build on miss. Pass nil to disable caching
// (handy for tests and one-off admin scripts).
type UserMemoryDeps struct {
	Profiles *repository.GormSkinProfileRepository
	Checks   *repository.GormSkinCheckRepository
	Feedback *repository.GormAIFeedbackRepository
	Routines *repository.GormRoutineEntryRepository
	Cache    *MemoryCache
}

// UserMemoryOptions tweaks how much we pull. Zero values pick sensible
// defaults so most call sites can pass UserMemoryOptions{} and get a
// reasonable block.
type UserMemoryOptions struct {
	// RecentCheckLimit is the number of SkinChecks (with AI analysis) to
	// summarise. Defaults to 6 (within the 5–8 sweet spot).
	RecentCheckLimit int
	// FeedbackLimit is the number of thumbs-up/down rows to surface.
	// Defaults to 12.
	FeedbackLimit int
	// RoutineWindowDays is how many days of routines we sample for the
	// completion-rate line. Defaults to 14.
	RoutineWindowDays int
	// DigestThreshold is the total-checks threshold above which we emit a
	// "## Older history (monthly digest)" section in addition to the
	// recent verbatim rows. Defaults to 50.
	DigestThreshold int
	// DigestMonths caps the number of month buckets in the digest section.
	// Defaults to 6 (about half a year of history).
	DigestMonths int
	// ExcludeCheckID is optional — set to the current SkinCheck's ID when
	// we're building memory for a check that's already saved so the AI
	// doesn't see "today" twice.
	ExcludeCheckID uuid.UUID
	// SkipCache forces a fresh rebuild even when a cached entry exists.
	// Use this from the /me/memory debug endpoint when the caller wants
	// to see ground truth, or when bust-on-write hasn't been wired yet.
	SkipCache bool
}

// MemoryDebug is the structured trace returned alongside the memory text.
//
// Why we expose it:
//   - The /me/memory endpoint surfaces it so engineers and product owners
//     can verify which sections are being pulled into the AI prompt without
//     having to re-derive that from raw SQL.
//   - The analysis pipeline and routine.Service log it at slog.Debug after
//     every build so prompt-loop diagnostics are one log line away.
//
// All counters are best-effort: a query failure in one section is reflected
// as a zero count for that section (not a build error), so debug never lies
// about what the model actually saw — empty section = zero counter.
type MemoryDebug struct {
	// SectionsPresent enumerates the markdown sections that ended up in
	// the output. Useful to spot "this user has no feedback yet" at a
	// glance.
	SectionsPresent []string `json:"sections_present"`
	// CharCount is len([]rune(text)); a rough proxy for token usage
	// (Vietnamese ≈ 5-6 chars/token, English ≈ 4).
	CharCount int `json:"char_count"`

	// Per-section counters — zero when section is absent.
	ProfilePresent  bool   `json:"profile_present"`
	RecentChecks    int    `json:"recent_checks"`
	HasMonthlyDigest bool  `json:"has_monthly_digest"`
	TotalChecks     int64  `json:"total_checks"`
	FeedbackRows    int    `json:"feedback_rows"`
	HelpfulVotes    int    `json:"helpful_votes"`
	NotHelpfulVotes int    `json:"not_helpful_votes"`
	AdherenceTier   string `json:"adherence_tier"` // "" when no routine data

	// CacheHit reports whether the build was short-circuited by the cache.
	// When true, all other counters represent the cached values inferred
	// from a re-parse of the text (best-effort).
	CacheHit bool `json:"cache_hit"`
	// CacheEligible is true when the call would have served / populated
	// the cache (no ExcludeCheckID, no SkipCache). Useful to spot
	// "every call disables cache" bugs.
	CacheEligible bool `json:"cache_eligible"`
}

func (o UserMemoryOptions) normalize() UserMemoryOptions {
	if o.RecentCheckLimit <= 0 {
		o.RecentCheckLimit = 6
	}
	if o.RecentCheckLimit > 8 {
		o.RecentCheckLimit = 8
	}
	if o.FeedbackLimit <= 0 {
		o.FeedbackLimit = 6
	}
	if o.FeedbackLimit > 8 {
		o.FeedbackLimit = 8
	}
	if o.RoutineWindowDays <= 0 {
		o.RoutineWindowDays = 14
	}
	if o.DigestThreshold <= 0 {
		o.DigestThreshold = 50
	}
	if o.DigestMonths <= 0 {
		o.DigestMonths = 6
	}
	return o
}

// BuildUserMemoryContext returns a human-readable markdown-style block that
// summarises everything we know about the user, ready to drop straight into a
// prompt (either system message or the user message body — both work).
//
// This is a thin wrapper around BuildUserMemoryWithDebug — use that variant
// directly when you also want the structured debug counters (the /me/memory
// endpoint and the AI pipelines both do).
//
// Empty string is NEVER returned: callers can concatenate the output without
// guarding against "".
func BuildUserMemoryContext(
	ctx context.Context,
	userID uuid.UUID,
	deps UserMemoryDeps,
	opts UserMemoryOptions,
) string {
	text, _ := BuildUserMemoryWithDebug(ctx, userID, deps, opts)
	return text
}

// BuildUserMemoryWithDebug is the full builder — returns both the prompt
// text and a structured trace.
//
// The text always begins with a stable header so the model can latch onto it
// reliably. Sections that have no data are omitted entirely (the model gets a
// short "no history yet" line at the very end instead of empty headers).
//
// Cache behaviour:
//   - When deps.Cache is non-nil AND opts.SkipCache is false AND
//     opts.ExcludeCheckID is uuid.Nil, we serve from cache when fresh.
//     ExcludeCheckID disables caching because each "exclude this row"
//     variant would generate a different output — caching them would
//     either be wrong or require keyed-by-exclude entries.
//
// Debug counters reflect the freshly-built data. On a cache hit, counters
// are best-effort re-derived from the cached text via cheap substring
// checks (CacheHit=true, RecentChecks/FeedbackRows may be approximate).
func BuildUserMemoryWithDebug(
	ctx context.Context,
	userID uuid.UUID,
	deps UserMemoryDeps,
	opts UserMemoryOptions,
) (string, MemoryDebug) {
	opts = opts.normalize()

	debug := MemoryDebug{
		CacheEligible: deps.Cache != nil && !opts.SkipCache && opts.ExcludeCheckID == uuid.Nil,
	}

	if debug.CacheEligible {
		if cached, ok := deps.Cache.Get(userID); ok {
			debug.CacheHit = true
			debug.CharCount = len([]rune(cached))
			debug.SectionsPresent = inferSectionsFromText(cached)
			logMemoryBuild(userID, debug)
			return cached, debug
		}
	}

	var sections []string

	if section := buildProfileSection(ctx, userID, deps.Profiles); section != "" {
		sections = append(sections, section)
		debug.SectionsPresent = append(debug.SectionsPresent, "profile")
		debug.ProfilePresent = true
	}
	if section, n := buildRecentChecksSectionDbg(ctx, userID, deps.Checks, opts); section != "" {
		sections = append(sections, section)
		debug.SectionsPresent = append(debug.SectionsPresent, "recent_checks")
		debug.RecentChecks = n
	}
	if section, total := buildMonthlyDigestSectionDbg(ctx, userID, deps.Checks, opts); section != "" {
		sections = append(sections, section)
		debug.SectionsPresent = append(debug.SectionsPresent, "monthly_digest")
		debug.HasMonthlyDigest = true
		debug.TotalChecks = total
	}
	if section, n, helpful, notHelpful := buildFeedbackSectionDbg(ctx, userID, deps.Feedback, opts); section != "" {
		sections = append(sections, section)
		debug.SectionsPresent = append(debug.SectionsPresent, "feedback")
		debug.FeedbackRows = n
		debug.HelpfulVotes = helpful
		debug.NotHelpfulVotes = notHelpful
	}
	if section, tier := buildRoutineCompletionSectionDbg(ctx, userID, deps.Routines, opts); section != "" {
		sections = append(sections, section)
		debug.SectionsPresent = append(debug.SectionsPresent, "routine_adherence")
		debug.AdherenceTier = tier
	}

	var b strings.Builder
	b.WriteString("USER_MEMORY (lịch sử da — dùng để cá nhân hoá, paraphrase ấm áp, không quote nguyên văn):\n")
	if len(sections) == 0 {
		b.WriteString("(no saved memory yet — this is a fresh user; rely on TODAY context only.)\n")
		out := b.String()
		debug.CharCount = len([]rune(out))
		if debug.CacheEligible {
			deps.Cache.Put(userID, out)
		}
		logMemoryBuild(userID, debug)
		return out, debug
	}
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s)
	}
	b.WriteString("\nGUIDANCE: Dựa mạnh vào block này + HÔM NAY. Mâu thuẫn → tin HÔM NAY. 👎 trước đó → đổi góc gợi ý.\n")
	out := b.String()
	debug.CharCount = len([]rune(out))
	if debug.CacheEligible {
		deps.Cache.Put(userID, out)
	}
	logMemoryBuild(userID, debug)
	return out, debug
}

// inferSectionsFromText is the best-effort fallback for cache hits — we
// don't have the raw counters anymore, so we just check for the section
// headers we know the builder emits.
func inferSectionsFromText(text string) []string {
	var sections []string
	if strings.Contains(text, "## Saved SkinProfile") {
		sections = append(sections, "profile")
	}
	if strings.Contains(text, "## Recent SkinChecks") {
		sections = append(sections, "recent_checks")
	}
	if strings.Contains(text, "## Older history (monthly digest") {
		sections = append(sections, "monthly_digest")
	}
	if strings.Contains(text, "## Feedback summary") || strings.Contains(text, "## Past AI feedback votes") {
		sections = append(sections, "feedback")
	}
	if strings.Contains(text, "## Routine adherence") {
		sections = append(sections, "routine_adherence")
	}
	return sections
}

// logMemoryBuild emits one structured debug line summarising what the AI
// will actually see. Use slog so call sites can crank the level via
// `slog.SetDefault(slog.New(...))` in main or tests.
//
// We emit at Debug level so production stays quiet — flip the level once
// the prompt-loop integration is being tuned.
func logMemoryBuild(userID uuid.UUID, d MemoryDebug) {
	slog.Debug(
		"user_memory built",
		"user_id", userID.String(),
		"chars", d.CharCount,
		"sections", strings.Join(d.SectionsPresent, ","),
		"cache_hit", d.CacheHit,
		"cache_eligible", d.CacheEligible,
		"recent_checks", d.RecentChecks,
		"feedback_rows", d.FeedbackRows,
		"helpful", d.HelpfulVotes,
		"not_helpful", d.NotHelpfulVotes,
		"adherence", d.AdherenceTier,
		"monthly_digest", d.HasMonthlyDigest,
	)
}

// buildProfileSection renders the saved SkinProfile (full) — skin type,
// concerns, skill, onboarding snapshot. Reuses the existing
// BuildSkinProfileContext so we get the same wording across call sites.
func buildProfileSection(
	ctx context.Context,
	userID uuid.UUID,
	repo *repository.GormSkinProfileRepository,
) string {
	if repo == nil {
		return ""
	}
	prof, err := repo.GetByUserID(ctx, userID)
	if err != nil || prof == nil {
		return ""
	}
	body := strings.TrimSpace(BuildSkinProfileContext(prof))
	if body == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Saved SkinProfile\n")
	b.WriteString(body)
	b.WriteString("\n")
	return b.String()
}

// buildRecentChecksSectionDbg is the debug-aware sibling of
// buildRecentChecksSection — same output, but also returns the row count
// so MemoryDebug can record it.
func buildRecentChecksSectionDbg(
	ctx context.Context,
	userID uuid.UUID,
	repo *repository.GormSkinCheckRepository,
	opts UserMemoryOptions,
) (string, int) {
	if repo == nil {
		return "", 0
	}
	rows, err := repo.ListRecentWithAnalysis(ctx, userID, opts.ExcludeCheckID, opts.RecentCheckLimit)
	if err != nil || len(rows) == 0 {
		return "", 0
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## Recent SkinChecks (last %d, newest first)\n", len(rows))
	for _, c := range rows {
		dateStr := c.CheckDate.UTC().Format("2006-01-02")
		conds, _ := dto.DecodeStringSlice(c.Conditions)
		syms, _ := dto.DecodeStringSlice(c.Symptoms)
		imgs, _ := dto.DecodeStringSlice(c.ImageURLs)

		fmt.Fprintf(&b, "- %s", dateStr)
		if len(imgs) > 0 {
			fmt.Fprintf(&b, " | photos: %d", len(imgs))
		}
		if len(conds) > 0 {
			fmt.Fprintf(&b, " | tags: %s", strings.Join(conds, ", "))
		}
		if len(syms) > 0 {
			fmt.Fprintf(&b, " | signals: %s", strings.Join(syms, ", "))
		}
		if t := strings.TrimSpace(c.Title); t != "" {
			fmt.Fprintf(&b, " | title: %s", truncateRunes(t, 60))
		}
		if note := strings.TrimSpace(c.UserNote); note != "" {
			fmt.Fprintf(&b, " | note: %s", truncateRunes(note, 80))
		}
		if line := summarizePreviousAIFeedback(c.Analysis); line != "" {
			fmt.Fprintf(&b, "\n    └─ previous AI line: %s", line)
		}
		b.WriteString("\n")
	}
	return b.String(), len(rows)
}

// summarizePreviousAIFeedback condenses an old SkinAnalysis row down to one
// short line: either the model's summary_notes or its situation_analysis
// (whichever exists, truncated). We deliberately don't surface scores —
// they're noisy across days and lead to the model anchoring on numbers.
func summarizePreviousAIFeedback(a *domain.SkinAnalysis) string {
	if a == nil {
		return ""
	}
	if a.Status != domain.AnalysisStatusCompleted {
		return ""
	}
	if s := strings.TrimSpace(a.SummaryNotes); s != "" {
		return truncateRunes(s, 120)
	}
	// situation_analysis lives inside skin_scores JSON (see analysis.Process
	// — it's merged in there as "situation_analysis"). Fall back to it when
	// summary_notes is empty.
	if len(a.SkinScores) > 0 {
		var m map[string]any
		if err := json.Unmarshal(a.SkinScores, &m); err == nil {
			if v, ok := m["situation_analysis"].(string); ok {
				if s := strings.TrimSpace(v); s != "" {
					return truncateRunes(s, 120)
				}
			}
		}
	}
	return ""
}

// buildMonthlyDigestSection emits a compact per-month summary of the user's
// OLDER check-ins (those past the recent verbatim window). This only fires
// when the user has crossed a "lots of history" threshold (DigestThreshold,
// default 50) — for newer users the recent-checks section already covers
// everything.
//
// Token economy: each month becomes a single line ("- 2025-04: 18 checks |
// tags: oily, redness, breakout | signals: stinging, tight"). Six months of
// digest is therefore ~6 lines of text, fitting comfortably alongside the
// other sections.
// buildMonthlyDigestSectionDbg also returns the total-check count so the
// caller can record it in MemoryDebug. Returns ("", 0) when the section
// shouldn't fire (below threshold).
func buildMonthlyDigestSectionDbg(
	ctx context.Context,
	userID uuid.UUID,
	repo *repository.GormSkinCheckRepository,
	opts UserMemoryOptions,
) (string, int64) {
	if repo == nil {
		return "", 0
	}
	total, err := repo.CountForUser(ctx, userID)
	if err != nil || total <= int64(opts.DigestThreshold) {
		return "", total
	}
	digest, err := repo.ListMonthlyDigest(ctx, userID, opts.RecentCheckLimit, opts.DigestMonths)
	if err != nil || len(digest) == 0 {
		return "", total
	}
	var b strings.Builder
	fmt.Fprintf(
		&b,
		"## Older history (monthly digest — %d total check-ins on file, skipping the %d shown above)\n",
		total, opts.RecentCheckLimit,
	)
	for _, row := range digest {
		fmt.Fprintf(&b, "- %s: %d check-ins", row.Month, row.CheckCount)
		if len(row.TopTags) > 0 {
			fmt.Fprintf(&b, " | top tags: %s", strings.Join(row.TopTags, ", "))
		}
		if len(row.TopSymptoms) > 0 {
			fmt.Fprintf(&b, " | top signals: %s", strings.Join(row.TopSymptoms, ", "))
		}
		b.WriteString("\n")
	}
	return b.String(), total
}

// buildFeedbackSectionDbg also returns the row count and the helpful /
// not-helpful split so the caller can see at a glance how the votes are
// distributed without re-parsing the text.
func buildFeedbackSectionDbg(
	ctx context.Context,
	userID uuid.UUID,
	repo *repository.GormAIFeedbackRepository,
	opts UserMemoryOptions,
) (string, int, int, int) {
	if repo == nil {
		return "", 0, 0, 0
	}
	rows, err := repo.ListByUser(ctx, userID, opts.FeedbackLimit)
	if err != nil || len(rows) == 0 {
		return "", 0, 0, 0
	}
	body := strings.TrimSpace(BuildPriorFeedbackContext(rows))
	if body == "" {
		return "", 0, 0, 0
	}
	var helpful, notHelpful int
	for _, r := range rows {
		switch r.Rating {
		case string(domain.AIFeedbackHelpful):
			helpful++
		case string(domain.AIFeedbackNotHelpful):
			notHelpful++
		}
	}
	var b strings.Builder
	b.WriteString("## Feedback summary\n")
	b.WriteString(buildFeedbackSummaryLine(rows, helpful, notHelpful))
	b.WriteString("## Past AI feedback votes\n")
	b.WriteString(body)
	b.WriteString("\n")
	return b.String(), len(rows), helpful, notHelpful
}

// buildFeedbackSummaryLine is a one-line digest so the model grasps vote
// patterns without reading every row first.
func buildFeedbackSummaryLine(rows []domain.AIUserFeedback, helpful, notHelpful int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- 👍 %d helpful / 👎 %d not_helpful (recent %d votes)", helpful, notHelpful, len(rows))
	var reasons []string
	for _, r := range rows {
		if r.Rating != string(domain.AIFeedbackNotHelpful) {
			continue
		}
		if c := strings.TrimSpace(r.Comment); c != "" {
			reasons = append(reasons, truncateRunes(c, 60))
		}
		if len(reasons) >= 2 {
			break
		}
	}
	if len(reasons) > 0 {
		b.WriteString(" | latest 👎: ")
		b.WriteString(strings.Join(reasons, "; "))
	} else if notHelpful > helpful {
		b.WriteString(" | user often marks not_helpful — soften & be specific")
	}
	b.WriteString("\n")
	return b.String()
}

// buildRoutineCompletionSectionDbg also returns the adherence tier label
// (e.g. "strong", "moderate", "low", "none") so the caller can record it in
// MemoryDebug and use it for downstream metrics. Returns ("", "") when no
// routine data is available.
func buildRoutineCompletionSectionDbg(
	ctx context.Context,
	userID uuid.UUID,
	repo *repository.GormRoutineEntryRepository,
	opts UserMemoryOptions,
) (string, string) {
	if repo == nil {
		return "", ""
	}
	since := time.Now().UTC().AddDate(0, 0, -opts.RoutineWindowDays+1)
	since = time.Date(since.Year(), since.Month(), since.Day(), 0, 0, 0, 0, time.UTC)

	rows, err := repo.ListForUserSince(ctx, userID, since, opts.RoutineWindowDays)
	if err != nil || len(rows) == 0 {
		return "", ""
	}

	var (
		daysWithEntry   int
		daysWithAnyTick int
		totalSteps      int
		completedSteps  int
	)
	for _, r := range rows {
		daysWithEntry++
		morning := decodeStepsForCount(r.Morning)
		evening := decodeStepsForCount(r.Evening)
		dayHasTick := false
		for _, s := range morning {
			totalSteps++
			if s.Completed {
				completedSteps++
				dayHasTick = true
			}
		}
		for _, s := range evening {
			totalSteps++
			if s.Completed {
				completedSteps++
				dayHasTick = true
			}
		}
		if dayHasTick {
			daysWithAnyTick++
		}
	}

	var stepRate float64
	if totalSteps > 0 {
		stepRate = float64(completedSteps) / float64(totalSteps)
	}
	var dayRate float64
	if daysWithEntry > 0 {
		dayRate = float64(daysWithAnyTick) / float64(daysWithEntry)
	}

	tier := completionTier(stepRate)
	tierShort := shortTierLabel(tier)

	var b strings.Builder
	b.WriteString("## Routine adherence\n")
	fmt.Fprintf(
		&b,
		"- Last %d days: steps %d/%d (%.0f%%) — %s; days with tick %d/%d (%.0f%%)\n",
		opts.RoutineWindowDays,
		completedSteps, totalSteps, stepRate*100, tier,
		daysWithAnyTick, daysWithEntry, dayRate*100,
	)
	return b.String(), tierShort
}

// shortTierLabel collapses the long human-friendly tier description back to
// a single token for log fields and stats.
func shortTierLabel(longTier string) string {
	switch {
	case strings.HasPrefix(longTier, "strong"):
		return "strong"
	case strings.HasPrefix(longTier, "moderate"):
		return "moderate"
	case strings.HasPrefix(longTier, "low"):
		return "low"
	case strings.HasPrefix(longTier, "no ticks"):
		return "none"
	default:
		return ""
	}
}

// completionTier maps a 0..1 rate to a coach-friendly tier the model can
// react to without needing to read the raw number. Tiers are intentionally
// gentle — even "low" gets framed as "needs less friction" not "user is
// failing".
func completionTier(rate float64) string {
	switch {
	case rate >= 0.75:
		return "strong adherence — praise the consistency, can introduce one small upgrade if appropriate"
	case rate >= 0.4:
		return "moderate adherence — keep suggestions doable, avoid piling new steps"
	case rate > 0:
		return "low adherence — simplify, reduce friction, no guilt-tripping"
	default:
		return "no ticks in window — user may be struggling to start; lean encouraging + ultra-low-friction"
	}
}

// decodeStepsForCount is a local lightweight decoder. We intentionally do
// not import dto.RoutineStep / dto.decodeRoutineSteps here because we only
// need two fields (title presence + completed flag) and the dto helper does
// more work (re-id generation, etc.) than we need.
func decodeStepsForCount(raw json.RawMessage) []routineStepLite {
	if len(raw) == 0 {
		return nil
	}
	var steps []routineStepLite
	if err := json.Unmarshal(raw, &steps); err != nil {
		return nil
	}
	out := make([]routineStepLite, 0, len(steps))
	for _, s := range steps {
		if strings.TrimSpace(s.Title) == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

type routineStepLite struct {
	Title     string `json:"title"`
	Completed bool   `json:"completed"`
}
