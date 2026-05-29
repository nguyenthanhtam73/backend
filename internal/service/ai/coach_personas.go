package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

// CoachPersona is a synthetic user fixture for coach QA — profile, today's
// check-in, and a pre-built USER_MEMORY block (no DB required).
type CoachPersona struct {
	ID                 string
	SkillLevel         string
	Profile            *domain.SkinProfile
	TodayCheck         *domain.SkinCheck
	Memory             string
	WantInOutput       []string // substrings expected in output
	WantWithMemoryOnly []string // should appear when memory wired, not required without
	AvoidInOutput      []string
}

// CoachPersonas returns the standard QA personas for daily feedback + routine suggest.
func CoachPersonas() []CoachPersona {
	return []CoachPersona{
		personaBeginnerOily(),
		personaIntermediateCombo(),
		personaFrequentNotHelpful(),
		personaStrongAdherence(),
	}
}

// TodayContext builds SKIN_PROFILE + TODAY_CHECK_IN markdown for coach calls.
func (p CoachPersona) TodayContext() string {
	return BuildDailyCheckInCoachContext(p.TodayCheck, p.Profile)
}

// TodayContextWithoutHistory is the same today block but with an empty memory sentinel.
func (p CoachPersona) TodayContextWithoutHistory() string {
	return p.TodayContext() + "\n\n" + emptyUserMemorySentinel()
}

// FullContextWithMemory joins today context + USER_MEMORY for GenerateDailyFeedback.
func (p CoachPersona) FullContextWithMemory() string {
	return p.TodayContext() + "\n\n" + strings.TrimSpace(p.Memory)
}

func emptyUserMemorySentinel() string {
	return "USER_MEMORY (lịch sử da — dùng để cá nhân hoá, paraphrase ấm áp, không quote nguyên văn):\n(no saved memory yet — this is a fresh user; rely on TODAY context only.)\n"
}

func personaBeginnerOily() CoachPersona {
	uid := uuid.New()
	profile := &domain.SkinProfile{
		ID:         uuid.New(),
		UserID:     uid,
		SkinType:   "oily",
		SkillLevel: domain.SkillLevelBeginner,
		Concerns:   mustJSONPersona([]string{"large_pores", "dehydration"}),
		Notes:      "vùng T hay bóng dầu, má hơi khô",
	}
	check := mkTodayCheck(uid, []string{"oily", "large_pores"}, []string{"shiny_tzone"},
		"Da dầu vùng trán và mũi, hơi khô ở má. Ngồi máy lạnh cả ngày.", "beginner")

	memory := assembleMemory(
		profileSection(profile),
		recentChecksSection([]recentCheckLine{
			{date: "2026-05-27", tags: "oily, large_pores", note: "T-zone bóng buổi chiều"},
			{date: "2026-05-26", tags: "oily, dehydration", note: "má căng nhẹ"},
			{date: "2026-05-25", tags: "oily", note: "quên kem chống nắng buổi sáng"},
		}),
		routineSection(28, 88),
	)

	return CoachPersona{
		ID:         "beginner_oily",
		SkillLevel: "beginner",
		Profile:    profile,
		TodayCheck: check,
		Memory:     memory,
		WantInOutput: []string{
			"dầu", "vùng T", "máy lạnh", "kem chống nắng",
		},
		WantWithMemoryOnly: []string{
			"mấy lần gần đây", "vài hôm", "tick", "bước", "routine",
		},
		AvoidInOutput: []string{"BHA", "retinol", "AHA"},
	}
}

func personaIntermediateCombo() CoachPersona {
	uid := uuid.New()
	profile := &domain.SkinProfile{
		ID:         uuid.New(),
		UserID:     uid,
		SkinType:   "combo",
		SkillLevel: domain.SkillLevelIntermediate,
		Concerns:   mustJSONPersona([]string{"dehydration", "large_pores"}),
		Notes:      "T dầu, má khô bên trong",
	}
	check := mkTodayCheck(uid, []string{"combo", "dehydration"}, []string{"tight_cheeks", "shiny_tzone"},
		"Má căng khô, trán hơi dầu. Ngủ muộn 2 đêm.", "intermediate")

	memory := assembleMemory(
		profileSection(profile),
		recentChecksSection([]recentCheckLine{
			{date: "2026-05-28", tags: "dehydration, combo", note: "má căng + T bóng"},
			{date: "2026-05-27", tags: "dehydration", note: "ngủ ít, da thiếu nước"},
			{date: "2026-05-26", tags: "combo", signals: "tight_cheeks", note: "má khô hơn T"},
		}),
		routineSection(55, 70),
	)

	return CoachPersona{
		ID:         "intermediate_combo",
		SkillLevel: "intermediate",
		Profile:    profile,
		TodayCheck: check,
		Memory:     memory,
		WantInOutput: []string{
			"khô", "má", "dầu", "ngủ",
		},
		WantWithMemoryOnly: []string{
			"mấy lần gần đây", "gần đây", "xu hướng", "vài hôm",
		},
	}
}

func personaFrequentNotHelpful() CoachPersona {
	uid := uuid.New()
	profile := &domain.SkinProfile{
		ID:         uuid.New(),
		UserID:     uid,
		SkinType:   "sensitive",
		SkillLevel: domain.SkillLevelBeginner,
		Concerns:   mustJSONPersona([]string{"redness", "weak_barrier"}),
		Notes:      "da dễ đỏ, từng dùng BHA bị rát",
	}
	check := mkTodayCheck(uid, []string{"sensitive", "redness"}, []string{"stinging", "weak_barrier"},
		"Má đỏ và châm chích nhẹ. Không dám thử sản phẩm mới.", "beginner")

	feedbackRows := []domain.AIUserFeedback{
		mkPersonaFeedback("not_helpful", "BHA quá mạnh, em da nhạy", 1),
		mkPersonaFeedback("not_helpful", "Gợi ý quá chung chung", 3),
		mkPersonaFeedback("helpful", "", 5),
		mkPersonaFeedback("not_helpful", "Quá nhiều bước", 7),
		mkPersonaFeedback("not_helpful", "Không hợp da nhạy", 10),
	}
	feedbackBody := strings.TrimSpace(BuildPriorFeedbackContext(feedbackRows))

	memory := assembleMemory(
		profileSection(profile),
		recentChecksSection([]recentCheckLine{
			{date: "2026-05-28", tags: "redness, sensitive", note: "má đỏ sau kem mới"},
			{date: "2026-05-27", tags: "weak_barrier", signals: "stinging", note: "châm chích tối qua"},
		}),
		fmt.Sprintf("## Feedback summary\n%s", buildFeedbackSummaryLine(feedbackRows, 1, 4)),
		"## Past AI feedback votes\n"+feedbackBody,
		routineSection(15, 40),
	)

	return CoachPersona{
		ID:         "frequent_not_helpful",
		SkillLevel: "beginner",
		Profile:    profile,
		TodayCheck: check,
		Memory:     memory,
		WantInOutput: []string{
			"đỏ", "châm chích", "dịu", "nhạy",
		},
		WantWithMemoryOnly: []string{
			"nhẹ", "đơn giản", "2 bước", "rút",
		},
		AvoidInOutput: []string{"BHA", "AHA", "retinol", "tẩy da chết hoá học"},
	}
}

func personaStrongAdherence() CoachPersona {
	uid := uuid.New()
	profile := &domain.SkinProfile{
		ID:         uuid.New(),
		UserID:     uid,
		SkinType:   "dry",
		SkillLevel: domain.SkillLevelIntermediate,
		Concerns:   mustJSONPersona([]string{"dryness", "dullness"}),
		Notes:      "da khô vùng má, routine đều 2 tuần nay",
	}
	check := mkTodayCheck(uid, []string{"dryness"}, []string{"dull"},
		"Da vẫn khô nhưng đỡ xỉn hơn tuần trước. Tick đủ routine 5 ngày liên tiếp.", "intermediate")

	memory := assembleMemory(
		profileSection(profile),
		recentChecksSection([]recentCheckLine{
			{date: "2026-05-28", tags: "dryness", note: "đỡ xỉn, vẫn khô má"},
			{date: "2026-05-27", tags: "dryness, dullness", note: "tick đủ routine"},
			{date: "2026-05-26", tags: "dryness", note: "kem dưỡng dày hơn giúp đỡ căng"},
		}),
		routineSection(82, 88),
	)

	return CoachPersona{
		ID:         "strong_adherence",
		SkillLevel: "intermediate",
		Profile:    profile,
		TodayCheck: check,
		Memory:     memory,
		WantInOutput: []string{
			"khô", "routine", "kem dưỡng",
		},
		WantWithMemoryOnly: []string{
			"duy trì", "đều", "5 ngày", "tick", "82",
		},
	}
}

// --- memory section builders (mirror user_memory.go format) ------------

type recentCheckLine struct {
	date    string
	tags    string
	signals string
	note    string
}

func assembleMemory(sections ...string) string {
	var b strings.Builder
	b.WriteString("USER_MEMORY (lịch sử da — dùng để cá nhân hoá, paraphrase ấm áp, không quote nguyên văn):\n")
	for i, s := range sections {
		if strings.TrimSpace(s) == "" {
			continue
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s)
	}
	b.WriteString("\nGUIDANCE: Dựa mạnh vào block này + HÔM NAY. Mâu thuẫn → tin HÔM NAY. 👎 trước đó → đổi góc gợi ý.\n")
	return b.String()
}

func profileSection(p *domain.SkinProfile) string {
	body := strings.TrimSpace(BuildSkinProfileContext(p))
	if body == "" {
		return ""
	}
	return "## Saved SkinProfile\n" + body + "\n"
}

func recentChecksSection(lines []recentCheckLine) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Recent SkinChecks (last %d, newest first)\n", len(lines))
	for _, ln := range lines {
		fmt.Fprintf(&b, "- %s", ln.date)
		if ln.tags != "" {
			fmt.Fprintf(&b, " | tags: %s", ln.tags)
		}
		if ln.signals != "" {
			fmt.Fprintf(&b, " | signals: %s", ln.signals)
		}
		if ln.note != "" {
			fmt.Fprintf(&b, " | note: %s", ln.note)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func routineSection(stepPct, dayPct int) string {
	stepRate := float64(stepPct) / 100
	dayRate := float64(dayPct) / 100
	var b strings.Builder
	b.WriteString("## Routine adherence (IMPORTANT — coach MUST mention & adjust routine_hints today)\n")
	fmt.Fprintf(&b, "- COACH_ACTION: %s\n", adherenceCoachAction(stepRate, dayRate))
	fmt.Fprintf(&b, "- Last 14 days: completed %d%% routine steps; days with tick %d%%\n", stepPct, dayPct)
	return b.String()
}

func mkTodayCheck(userID uuid.UUID, conds, syms []string, note, skill string) *domain.SkinCheck {
	climate, _ := json.Marshal(map[string]string{
		"coach_skill_level": skill,
		"ui_locale":         "vi",
	})
	condJSON, _ := json.Marshal(conds)
	symJSON, _ := json.Marshal(syms)
	return &domain.SkinCheck{
		ID:              uuid.New(),
		UserID:          userID,
		Title:           "Check-in hôm nay",
		UserNote:        note,
		Conditions:      condJSON,
		Symptoms:        symJSON,
		ClimateContext:  climate,
		EnvironmentNote: "máy lạnh văn phòng",
		CheckDate:       time.Now().UTC(),
	}
}

func mkPersonaFeedback(rating, comment string, daysAgo int) domain.AIUserFeedback {
	return domain.AIUserFeedback{
		ID:         uuid.New(),
		TargetType: string(domain.AIFeedbackTargetSuggestedRoutine),
		Rating:     rating,
		Comment:    comment,
		CreatedAt:  time.Now().Add(-time.Duration(daysAgo) * 24 * time.Hour),
	}
}

func mustJSONPersona(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
