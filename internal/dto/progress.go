package dto

import (
	"encoding/json"
	"math"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// ProgressTimelineResponse is returned by GET /api/v1/progress.
//
// Shape choices:
//   - `entries` is ordered newest-first so the timeline renders top-down without
//     re-sorting on the client.
//   - Every entry carries a flat snapshot of soft scores so the frontend can chart
//     them with a single pass — no extra round-trips per row.
//   - `summary` is included inline for convenience; the same payload powers the
//     "this month vs last month" hero card.
type ProgressTimelineResponse struct {
	RangeDays int                 `json:"range_days"` // 0 = "all"
	From      string              `json:"from,omitempty"`
	To        string              `json:"to"`
	Total     int                 `json:"total"`
	Entries   []ProgressEntry     `json:"entries"`
	Summary   ProgressSummaryData `json:"summary"`
}

// ProgressEntry is one row on the timeline: photo thumbnail + tags + score gauges.
// It is intentionally lean compared to the full skin-check DTO — the timeline does
// NOT render the full coach feedback (user clicks through to view it via the
// single-check GET endpoint).
type ProgressEntry struct {
	ID        string                `json:"id"`
	CheckDate string                `json:"check_date"`
	CreatedAt string                `json:"created_at"`
	Title     string                `json:"title,omitempty"`
	UserNote  string                `json:"user_note,omitempty"`
	Tags      []string              `json:"tags,omitempty"`     // conditions tags chosen by user
	Symptoms  []string              `json:"symptoms,omitempty"` // signal/symptom tags
	ImageURLs []string              `json:"image_urls"`         // already prefixed with `/uploads/`
	Status    string                `json:"status"`             // pending|processing|completed|failed
	Gauges    *SkinCoachScoreGauges `json:"gauges,omitempty"`
	Snippet   string                `json:"snippet,omitempty"` // 1-line coach summary for the card preview
}

// ProgressSummaryData is the aggregate view: monthly buckets + an at-a-glance
// "this month vs previous month" comparison so the hero card has motivational copy.
type ProgressSummaryData struct {
	Buckets       []MonthlyBucket    `json:"buckets,omitempty"`        // newest month first
	CurrentMonth  *MonthlyBucket     `json:"current_month,omitempty"`  // alias for buckets[0] (nil if empty)
	PreviousMonth *MonthlyBucket     `json:"previous_month,omitempty"` // alias for buckets[1] (nil if not enough data)
	Comparison    *MonthlyComparison `json:"comparison,omitempty"`     // null when previous month is unavailable
	TotalChecks   int                `json:"total_checks"`             // in the queried range
	StreakDays    int                `json:"streak_days"`              // consecutive days with at least one check-in counted backwards from today
	TopTags       []TagCount         `json:"top_tags,omitempty"`       // top 5 condition tags in the range
	// FeedbackTargetID — fresh per-render UUID the frontend uses when posting
	// thumbs-up/down votes against this summary card (target_type="progress_summary").
	FeedbackTargetID string `json:"feedback_target_id,omitempty"`
}

// MonthlyBucket aggregates one calendar month's worth of soft gauges and tag counts.
// Mean values are nil when no completed analysis in that month carried that gauge.
type MonthlyBucket struct {
	Month        string   `json:"month"` // "2026-05"
	ChecksCount  int      `json:"checks_count"`
	OverallAvg   *float64 `json:"overall_avg,omitempty"`
	HydrationAvg *float64 `json:"hydration_avg,omitempty"`
	ClarityAvg   *float64 `json:"clarity_avg,omitempty"`
	BarrierAvg   *float64 `json:"barrier_avg,omitempty"`
}

// MonthlyComparison highlights how the current month moved vs the previous month
// across each gauge. Values are signed deltas (e.g. +0.12 means "12 points higher").
// Trend = "up" | "flat" | "down" based on |delta| > 0.03 threshold (≈ 3 points on a 0–1 scale).
type MonthlyComparison struct {
	OverallDelta   *float64 `json:"overall_delta,omitempty"`
	HydrationDelta *float64 `json:"hydration_delta,omitempty"`
	ClarityDelta   *float64 `json:"clarity_delta,omitempty"`
	BarrierDelta   *float64 `json:"barrier_delta,omitempty"`
	Trend          string   `json:"trend"`               // overall trend label: "up" | "flat" | "down"
	HeadlinePct    *int     `json:"headline_pct,omitempty"` // signed percent change on Overall, suitable for "+18%"
}

// TagCount supports a small leaderboard like "most common conditions this month".
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// NewProgressTimelineResponse builds the timeline response from raw domain rows.
//
// uploadPublicPrefix is the public URL prefix that gets prepended to every
// stored relative image path (e.g. "/uploads"). Pass an empty string if the
// caller wants raw relatives back.
func NewProgressTimelineResponse(rows []domain.SkinCheck, rangeDays int, uploadPublicPrefix string) ProgressTimelineResponse {
	out := ProgressTimelineResponse{
		RangeDays: rangeDays,
		To:        time.Now().UTC().Format("2006-01-02"),
		Entries:   make([]ProgressEntry, 0, len(rows)),
		Total:     len(rows),
	}
	if rangeDays > 0 {
		out.From = time.Now().UTC().AddDate(0, 0, -rangeDays).Format("2006-01-02")
	}
	for i := range rows {
		out.Entries = append(out.Entries, newProgressEntry(&rows[i], uploadPublicPrefix))
	}
	out.Summary = computeProgressSummary(rows)
	return out
}

func newProgressEntry(c *domain.SkinCheck, uploadPublicPrefix string) ProgressEntry {
	if c == nil {
		return ProgressEntry{}
	}
	tags, _ := DecodeStringSlice(c.Conditions)
	syms, _ := DecodeStringSlice(c.Symptoms)
	rels, _ := DecodeStringSlice(c.ImageURLs)
	urls := make([]string, 0, len(rels))
	for _, rel := range rels {
		clean := strings.TrimLeft(strings.ReplaceAll(rel, "\\", "/"), "/")
		if clean == "" {
			continue
		}
		if uploadPublicPrefix == "" {
			urls = append(urls, "/"+clean)
		} else {
			urls = append(urls, "/"+path.Join(strings.Trim(uploadPublicPrefix, "/"), clean))
		}
	}
	entry := ProgressEntry{
		ID:        c.ID.String(),
		CheckDate: c.CheckDate.UTC().Format("2006-01-02"),
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
		Title:     strings.TrimSpace(c.Title),
		UserNote:  strings.TrimSpace(c.UserNote),
		Tags:      tags,
		Symptoms:  syms,
		ImageURLs: urls,
		Status:    "pending",
	}
	if c.Analysis != nil {
		entry.Status = string(c.Analysis.Status)
		if c.Analysis.Status == domain.AnalysisStatusCompleted {
			coach := buildCoachDetailFromDomain(c.Analysis)
			if coach != nil {
				entry.Gauges = coach.SkinScoreGauges
				entry.Snippet = pickTimelineSnippet(coach)
			}
		}
	}
	return entry
}

// pickTimelineSnippet prefers summary_notes (always 1 short warm line) and falls
// back to the situation_summary trimmed to a single sentence so timeline cards
// stay scannable.
func pickTimelineSnippet(coach *SkinCoachDetail) string {
	if coach == nil {
		return ""
	}
	if s := strings.TrimSpace(coach.SummaryNotes); s != "" {
		return truncateLine(s, 160)
	}
	if s := strings.TrimSpace(coach.SituationSummary); s != "" {
		return truncateLine(firstSentence(s), 160)
	}
	return ""
}

func firstSentence(s string) string {
	for i, r := range s {
		if r == '.' || r == '!' || r == '?' {
			return strings.TrimSpace(s[:i+1])
		}
	}
	return s
}

func truncateLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return strings.TrimSpace(string(runes[:max])) + "…"
}

// computeProgressSummary buckets check-ins into months and computes the
// motivational comparison card. We work directly off the domain SkinScores JSON
// blob so we do not duplicate gauge-extraction logic.
func computeProgressSummary(rows []domain.SkinCheck) ProgressSummaryData {
	type acc struct {
		count                                       int
		overSum, hydrSum, clarSum, barrSum          float64
		overCount, hydrCount, clarCount, barrCount  int
	}
	buckets := make(map[string]*acc)
	tagCounts := make(map[string]int)
	checkDates := make(map[string]struct{})
	for i := range rows {
		c := &rows[i]
		key := c.CheckDate.UTC().Format("2006-01")
		b, ok := buckets[key]
		if !ok {
			b = &acc{}
			buckets[key] = b
		}
		b.count++
		// Tag frequency for the leaderboard.
		if tags, err := DecodeStringSlice(c.Conditions); err == nil {
			for _, tg := range tags {
				tg = strings.TrimSpace(tg)
				if tg != "" {
					tagCounts[tg]++
				}
			}
		}
		checkDates[c.CheckDate.UTC().Format("2006-01-02")] = struct{}{}
		// Soft gauges live in skin_scores JSON; tolerate missing rows / shapes.
		if c.Analysis != nil && c.Analysis.Status == domain.AnalysisStatusCompleted && len(c.Analysis.SkinScores) > 0 {
			var scores map[string]any
			if err := json.Unmarshal(c.Analysis.SkinScores, &scores); err == nil {
				if v, ok := numFromAny(scores["overall"]); ok && v != nil {
					b.overSum += *v
					b.overCount++
				}
				if v, ok := numFromAny(scores["hydration"]); ok && v != nil {
					b.hydrSum += *v
					b.hydrCount++
				}
				if v, ok := numFromAny(scores["clarity"]); ok && v != nil {
					b.clarSum += *v
					b.clarCount++
				}
				if v, ok := numFromAny(scores["barrier"]); ok && v != nil {
					b.barrSum += *v
					b.barrCount++
				}
			}
		}
	}

	// Newest month first.
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))

	out := ProgressSummaryData{
		TotalChecks:      len(rows),
		StreakDays:       computeStreak(checkDates),
		TopTags:          topTags(tagCounts, 5),
		FeedbackTargetID: uuid.New().String(),
	}
	for _, k := range keys {
		b := buckets[k]
		bucket := MonthlyBucket{
			Month:       k,
			ChecksCount: b.count,
		}
		if b.overCount > 0 {
			v := round2(b.overSum / float64(b.overCount))
			bucket.OverallAvg = &v
		}
		if b.hydrCount > 0 {
			v := round2(b.hydrSum / float64(b.hydrCount))
			bucket.HydrationAvg = &v
		}
		if b.clarCount > 0 {
			v := round2(b.clarSum / float64(b.clarCount))
			bucket.ClarityAvg = &v
		}
		if b.barrCount > 0 {
			v := round2(b.barrSum / float64(b.barrCount))
			bucket.BarrierAvg = &v
		}
		out.Buckets = append(out.Buckets, bucket)
	}
	if len(out.Buckets) > 0 {
		out.CurrentMonth = &out.Buckets[0]
	}
	if len(out.Buckets) > 1 {
		out.PreviousMonth = &out.Buckets[1]
		out.Comparison = compareMonths(out.Buckets[0], out.Buckets[1])
	}
	return out
}

// compareMonths produces the signed delta + "up/flat/down" trend label used by
// the hero card. The 0.03 threshold = ~3 percentage points on a 0–1 gauge; below
// that we say "flat" to avoid celebrating noise.
func compareMonths(cur, prev MonthlyBucket) *MonthlyComparison {
	const flatThreshold = 0.03
	cmp := &MonthlyComparison{Trend: "flat"}
	overall := deltaPtr(cur.OverallAvg, prev.OverallAvg)
	cmp.OverallDelta = overall
	cmp.HydrationDelta = deltaPtr(cur.HydrationAvg, prev.HydrationAvg)
	cmp.ClarityDelta = deltaPtr(cur.ClarityAvg, prev.ClarityAvg)
	cmp.BarrierDelta = deltaPtr(cur.BarrierAvg, prev.BarrierAvg)
	if overall != nil {
		switch {
		case *overall > flatThreshold:
			cmp.Trend = "up"
		case *overall < -flatThreshold:
			cmp.Trend = "down"
		}
		// Express as percentage delta vs previous (rounded to int for headline).
		if prev.OverallAvg != nil && *prev.OverallAvg > 0 {
			pct := int(math.Round((*overall) / *prev.OverallAvg * 100))
			cmp.HeadlinePct = &pct
		}
	}
	return cmp
}

func deltaPtr(cur, prev *float64) *float64 {
	if cur == nil || prev == nil {
		return nil
	}
	v := round2(*cur - *prev)
	return &v
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// computeStreak counts consecutive days ending today that have at least one
// check-in. Today must be present for the streak to start counting; a missed
// day breaks it.
func computeStreak(checkDates map[string]struct{}) int {
	if len(checkDates) == 0 {
		return 0
	}
	today := time.Now().UTC()
	streak := 0
	for i := 0; i < 365; i++ {
		key := today.AddDate(0, 0, -i).Format("2006-01-02")
		if _, ok := checkDates[key]; ok {
			streak++
			continue
		}
		// Allow today itself to be missing (user might check in later) only on day 0.
		if i == 0 {
			continue
		}
		break
	}
	return streak
}

func topTags(counts map[string]int, max int) []TagCount {
	if len(counts) == 0 {
		return nil
	}
	out := make([]TagCount, 0, len(counts))
	for tag, c := range counts {
		out = append(out, TagCount{Tag: tag, Count: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tag < out[j].Tag
	})
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}
