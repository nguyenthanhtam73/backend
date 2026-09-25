package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
)

// reportedComboProfile is the production test user: combination skin, concerns
// mụn + thâm/sạm, goal giảm mụn, experience mới bắt đầu, and no check-ins.
func reportedComboProfile() *domain.SkinProfile {
	concerns, _ := json.Marshal([]string{"Mụn", "Thâm / sạm"})
	snap, _ := json.Marshal(map[string]any{
		"goal":          "Giảm mụn",
		"skin_type":     "Da hỗn hợp",
		"skill_level":   "beginner",
		"body_concerns": []string{"Mụn", "Thâm / sạm"},
	})
	return &domain.SkinProfile{
		SkinType:           "Da hỗn hợp",
		SkillLevel:         domain.SkillLevelBeginner,
		Concerns:           concerns,
		OnboardingSnapshot: snap,
	}
}

func reportedComboProfileIDs() *domain.SkinProfile {
	concerns, _ := json.Marshal([]string{"clear_acne", "combination", "acne", "hyperpigmentation"})
	snap, _ := json.Marshal(map[string]any{
		"goal":          "clear_acne",
		"skin_type":     "combination",
		"skill_level":   "beginner",
		"body_concerns": []string{"acne", "hyperpigmentation"},
	})
	return &domain.SkinProfile{
		SkinType:           "combination",
		SkillLevel:         domain.SkillLevelBeginner,
		Concerns:           concerns,
		OnboardingSnapshot: snap,
	}
}

func TestBuildWardrobePrompt_ProfileWithNoCheckIns(t *testing.T) {
	for _, profile := range []*domain.SkinProfile{reportedComboProfile(), reportedComboProfileIDs()} {
		text, known := buildWardrobeProductInsightUser(WardrobeProductInsightRequest{
			Name:    "The Body Shop Coconut Body Butter",
			Brand:   "The Body Shop",
			Profile: profile,
		})
		if !known {
			t.Fatal("a saved skin type must count as known even with zero check-ins")
		}
		for _, s := range []string{
			"da hỗn hợp",
			"mụn",
			"thâm",
			"Giảm mụn",
			"Mới bắt đầu",
			"skin type is known",
			"RECENT_CHECK_INS: none",
			"missing check-in is not missing skin information",
		} {
			if !strings.Contains(text, s) {
				t.Fatalf("prompt missing %q\n%s", s, text)
			}
		}
		for _, absent := range []string{
			"No saved skin profile",
			"clear_acne",
			"combination",
			"hyperpigmentation",
			"beginner",
			"- sensitivity:",
		} {
			if strings.Contains(text, absent) {
				t.Fatalf("prompt should not contain %q\n%s", absent, text)
			}
		}
	}
}

func TestBuildWardrobePrompt_SensitivityWhenPresent(t *testing.T) {
	snap, _ := json.Marshal(map[string]any{
		"goal":      "barrier",
		"skin_type": "sensitive",
		"skin_analysis": map[string]any{
			"barrier_signal": "possibly_compromised",
		},
	})
	text, known := buildWardrobeProductInsightUser(WardrobeProductInsightRequest{
		Name: "Kem dưỡng",
		Profile: &domain.SkinProfile{
			SkinType:           "sensitive",
			SkillLevel:         domain.SkillLevelBeginner,
			OnboardingSnapshot: snap,
		},
	})
	if !known {
		t.Fatal("expected known skin")
	}
	if !strings.Contains(text, "da nhạy cảm") || !strings.Contains(text, "- sensitivity: Da nhạy cảm") {
		t.Fatalf("sensitivity missing\n%s", text)
	}
	if !strings.Contains(text, "Mới bắt đầu") {
		t.Fatalf("experience missing\n%s", text)
	}
}

func TestWardrobeInsightValidation_ContradictoryBodyButter(t *testing.T) {
	req := WardrobeProductInsightRequest{
		Name:    "The Body Shop Coconut Body Butter",
		Brand:   "The Body Shop",
		Profile: reportedComboProfile(),
	}
	facts := assembleWardrobeInsightFacts(req)
	if !facts.SkinTypeKnown || !facts.AcneProne || facts.FaceLimit != acneFaceBodyProduct || facts.Irritated {
		t.Fatalf("facts: %+v", facts)
	}
	card := dto.WardrobeProductInsight{
		WhatItDoes: "Bơ dưỡng thể dừa, dưỡng ẩm cho da.",
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitMaybe, Reason: "Không có thông tin về kích ứng gần đây."},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyNo, Why: "Chưa có thông tin đầy đủ về da."},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
	problems := validateWardrobeProductInsight(card, facts)
	if len(problems) == 0 {
		t.Fatal("expected the contradictory card to fail")
	}
	fallback := wardrobeInsightFallback(facts)
	if again := validateWardrobeProductInsight(fallback, facts); len(again) != 0 {
		t.Fatalf("fallback still invalid: %v\n%+v", again, fallback)
	}
	if fallback.Fit.Verdict != dto.WardrobeFitNo || fallback.Buy.Advice != dto.WardrobeBuyNo {
		t.Fatalf("body butter should be not a fit: %+v", fallback)
	}
	blob := strings.ToLower(fallback.Fit.Reason + " " + fallback.Buy.Why + " " + fallback.WhatItDoes)
	for _, s := range []string{"cơ thể", "không nên bôi", "mặt", "hỗn hợp", "mụn"} {
		if !strings.Contains(blob, s) {
			t.Fatalf("fallback missing %q: %s", s, blob)
		}
	}
	for _, bad := range []string{"nặng", "bí", "bít"} {
		if strings.Contains(blob, bad) {
			t.Fatalf("body product should not be called bad (%q): %s", bad, blob)
		}
	}
	assertNoShoppingOrUnknown(t, fallback)
}

func TestAcneFaceUseLimit_BodyLabelsAndCoconutOilOnly(t *testing.T) {
	profile := reportedComboProfile()
	bodyNames := []string{
		"Nivea Body Lotion",
		"Shea Body Butter",
		"Everyday Body Cream",
		"Kem dưỡng thể",
		"Kem body ban đêm",
	}
	for _, name := range bodyNames {
		facts := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{Name: name, Profile: profile})
		if facts.FaceLimit != acneFaceBodyProduct {
			t.Fatalf("%q should be a body product, got %d", name, facts.FaceLimit)
		}
	}
	oil := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{Name: "Dầu dừa nguyên chất", Profile: profile})
	if oil.FaceLimit != acneFaceCoconutOil {
		t.Fatalf("coconut oil limit: %d", oil.FaceLimit)
	}
	oilCard := wardrobeInsightFallback(oil)
	if oilCard.Fit.Verdict != dto.WardrobeFitNo || oilCard.Buy.Advice != dto.WardrobeBuyNo {
		t.Fatalf("coconut oil card: %+v", oilCard)
	}
	if problems := validateWardrobeProductInsight(oilCard, oil); len(problems) != 0 {
		t.Fatalf("coconut oil fallback invalid: %v\n%+v", problems, oilCard)
	}
	if !strings.Contains(oilCard.Fit.Reason, "dầu dừa") || strings.Contains(oilCard.Fit.Reason, "nặng") {
		t.Fatalf("coconut oil wording: %s", oilCard.Fit.Reason)
	}
	oilEN := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{Name: "Organic Coconut Oil", Profile: profile})
	if oilEN.FaceLimit != acneFaceCoconutOil {
		t.Fatalf("coconut oil EN limit: %d", oilEN.FaceLimit)
	}
	// "Coconut" plus "butter" is not a rule. Only a body label or the words "coconut oil" count.
	plain := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{Name: "Coconut Butter", Profile: profile})
	if plain.FaceLimit != acneFaceOK {
		t.Fatalf("coconut butter without a body label or coconut oil must not be forced off the face, got %d", plain.FaceLimit)
	}
}

func TestPetrolatumIsNotForcedOffAcneProneFace(t *testing.T) {
	profile := reportedComboProfile()
	for _, name := range []string{
		"Vaseline Healing Jelly",
		"White Petrolatum ointment",
		"Mineral Oil",
		"Paraffinum Liquidum",
	} {
		facts := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{Name: name, Profile: profile})
		if facts.FaceLimit != acneFaceOK {
			t.Fatalf("%q must not be forced to no, got limit %d", name, facts.FaceLimit)
		}
		card := dto.WardrobeProductInsight{
			WhatItDoes: "Kem dưỡng khóa ẩm.",
			Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitYes, Reason: "Hợp da hỗn hợp đang muốn giảm mụn."},
			Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyYes, Why: "Nên dùng tiếp vì hợp da hỗn hợp."},
			Disclaimer: dto.WardrobeInsightDisclaimer,
		}
		if problems := validateWardrobeProductInsight(card, facts); len(problems) != 0 {
			t.Fatalf("%q was forced off the face: %v", name, problems)
		}
		fallback := wardrobeInsightFallback(facts)
		if fallback.Fit.Verdict == dto.WardrobeFitNo {
			t.Fatalf("%q fallback must not be a forced no: %+v", name, fallback)
		}
	}
}

func TestWardrobeInsightValidation_FoamingGelStaysConsistent(t *testing.T) {
	req := WardrobeProductInsightRequest{
		Name:    "La Roche-Posay Effaclar Purifying Foaming Gel",
		Brand:   "La Roche-Posay",
		Profile: reportedComboProfile(),
	}
	facts := assembleWardrobeInsightFacts(req)
	if facts.FaceLimit != acneFaceOK {
		t.Fatal("foaming gel must not be treated as a body product")
	}
	good := dto.WardrobeProductInsight{
		WhatItDoes: "Sữa rửa mặt tạo bọt, làm sạch dầu thừa.",
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitYes, Reason: "Hợp da hỗn hợp đang muốn giảm mụn, chưa thấy da rát."},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyYes, Why: "Nên dùng tiếp vì hợp da hỗn hợp và mục tiêu giảm mụn."},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
	if problems := validateWardrobeProductInsight(good, facts); len(problems) != 0 {
		t.Fatalf("good card: %v", problems)
	}
	bad := dto.WardrobeProductInsight{
		WhatItDoes: "Sữa rửa mặt tạo bọt.",
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitMaybe, Reason: "Không có thông tin về kích ứng gần đây."},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyNo, Why: "Chưa đủ thông tin về da."},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
	if problems := validateWardrobeProductInsight(bad, facts); len(problems) == 0 {
		t.Fatal("expected contradiction to fail")
	}
	fallback := wardrobeInsightFallback(facts)
	if fallback.Fit.Verdict != dto.WardrobeFitMaybe || fallback.Buy.Advice != dto.WardrobeBuyYes {
		t.Fatalf("cleanser fallback should stay a possible keep-using card: %+v", fallback)
	}
	if again := validateWardrobeProductInsight(fallback, facts); len(again) != 0 {
		t.Fatalf("fallback invalid: %v\n%+v", again, fallback)
	}
	if !strings.Contains(strings.ToLower(fallback.Fit.Reason), "hỗn hợp") || !strings.Contains(strings.ToLower(fallback.Fit.Reason), "mụn") {
		t.Fatalf("fallback should name the profile: %+v", fallback)
	}
	assertNoShoppingOrUnknown(t, fallback)
}

func TestWardrobeInsightValidation_IrritationAllowsPause(t *testing.T) {
	req := WardrobeProductInsightRequest{
		Name:    "La Roche-Posay Effaclar Purifying Foaming Gel",
		Profile: reportedComboProfile(),
		Recent: []domain.SkinCheck{{
			UserNote: "má đang rát sau khi rửa mặt",
		}},
	}
	facts := assembleWardrobeInsightFacts(req)
	if !facts.Irritated {
		t.Fatal("expected irritation")
	}
	paused := dto.WardrobeProductInsight{
		WhatItDoes: "Sữa rửa mặt tạo bọt.",
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitMaybe, Reason: "Da hỗn hợp đang rát sau lần rửa gần đây."},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyNo, Why: "Chưa nên dùng tiếp vì da đang rát."},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
	if problems := validateWardrobeProductInsight(paused, facts); len(problems) != 0 {
		t.Fatalf("irritation pause should be allowed: %v", problems)
	}
	vague := paused
	vague.Fit.Reason = "Có thể hợp."
	vague.Buy.Why = "Chưa nên dùng tiếp."
	if problems := validateWardrobeProductInsight(vague, facts); len(problems) == 0 {
		t.Fatal("pause without an irritation sentence should fail")
	}
}

func TestWardrobeInsightValidation_NotAFitNeedsProfileAnchor(t *testing.T) {
	facts := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{
		Name:    "Kem dưỡng",
		Profile: reportedComboProfile(),
	})
	vague := dto.WardrobeProductInsight{
		WhatItDoes: "Kem dưỡng.",
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitNo, Reason: "Không hợp lắm."},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyNo, Why: "Chưa nên dùng tiếp."},
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
	if problems := validateWardrobeProductInsight(vague, facts); len(problems) == 0 {
		t.Fatal("not-a-fit without the person's skin should fail")
	}
	specific := vague
	specific.Fit.Reason = "Kem này nặng và bí với da hỗn hợp đang muốn giảm mụn."
	specific.Buy.Why = "Chưa nên dùng tiếp vì dễ làm da hỗn hợp bị bít mụn."
	if problems := validateWardrobeProductInsight(specific, facts); len(problems) != 0 {
		t.Fatalf("specific not-a-fit should pass: %v", problems)
	}
}

func TestWardrobeInsightFallback_UnknownSkinStaysMaybe(t *testing.T) {
	facts := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{Name: "Kem"})
	if facts.SkinKnown || facts.SkinTypeKnown {
		t.Fatal("empty profile")
	}
	card := wardrobeInsightFallback(facts)
	if card.Fit.Verdict != dto.WardrobeFitMaybe || card.Buy.Advice != dto.WardrobeBuyNo {
		t.Fatalf("unknown card: %+v", card)
	}
	if card.Buy.Advice != "chưa nên" && card.Buy.Advice != "nên mua" {
		t.Fatalf("advice token drifted: %q", card.Buy.Advice)
	}
}

func assertNoShoppingOrUnknown(t *testing.T, card dto.WardrobeProductInsight) {
	t.Helper()
	blob := strings.ToLower(card.WhatItDoes + " " + card.Fit.Reason + " " + card.Buy.Why)
	for _, s := range []string{"chưa có thông tin", "không có thông tin", "chưa đủ thông tin", "không đủ thông tin"} {
		if strings.Contains(blob, s) {
			t.Fatalf("fallback claims missing skin info (%q): %s", s, blob)
		}
	}
	if strings.Contains(blob, "mua") {
		t.Fatalf("fallback says mua: %s", blob)
	}
	if card.Buy.Advice != dto.WardrobeBuyYes && card.Buy.Advice != dto.WardrobeBuyNo {
		t.Fatalf("advice token: %q", card.Buy.Advice)
	}
}
