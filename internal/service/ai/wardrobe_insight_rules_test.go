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
	correction := wardrobeInsightCorrection(problems)
	if !strings.Contains(correction, "có thể chưa hợp") || strings.Contains(correction, "should not be applied") {
		t.Fatalf("retry instruction should ask for a hedge:\n%s", correction)
	}
	fallback := wardrobeInsightFallback(facts)
	if again := validateWardrobeProductInsight(fallback, facts); len(again) != 0 {
		t.Fatalf("fallback still invalid: %v\n%+v", again, fallback)
	}
	if fallback.Fit.Verdict != dto.WardrobeFitNo || fallback.Buy.Advice != dto.WardrobeBuyNo {
		t.Fatalf("body butter should be not a fit: %+v", fallback)
	}
	blob := strings.ToLower(fallback.Fit.Reason + " " + fallback.Buy.Why + " " + fallback.WhatItDoes)
	for _, s := range []string{"cơ thể", "có thể chưa hợp", "mặt", "hỗn hợp", "mụn", "bạn cân nhắc"} {
		if !strings.Contains(blob, s) {
			t.Fatalf("fallback missing %q: %s", s, blob)
		}
	}
	if strings.Contains(blob, "không nên bôi") {
		t.Fatalf("fallback still commands the user: %s", blob)
	}
	for _, s := range []string{"bít", "lỗ chân lông", "có thể"} {
		if !strings.Contains(blob, s) {
			t.Fatalf("body fallback missing the plain why %q: %s", s, blob)
		}
	}
	if fallback.WhatItDoes != exampleBodyWhat || fallback.Fit.Reason != exampleBodyReason || fallback.Buy.Why != exampleBodyWhy {
		t.Fatalf("body fallback copy:\nwhat %q\nreason %q\nwhy %q", fallback.WhatItDoes, fallback.Fit.Reason, fallback.Buy.Why)
	}
	if len(fallback.Actives) != 0 {
		t.Fatalf("body product with no obvious single ingredient should not invent actives: %+v", fallback.Actives)
	}
	for _, bad := range []string{"nặng", "occlusive", "the body shop", "khiến da dễ nổi mụn"} {
		if strings.Contains(blob, bad) {
			t.Fatalf("body product should not be called bad (%q): %s", bad, blob)
		}
	}
	if hasVietnameseWord(blob, "bí") {
		t.Fatalf("standalone bí still calls the product bad: %s", blob)
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
	if oilCard.WhatItDoes != exampleCoconutWhat || oilCard.Fit.Reason != exampleCoconutReason || oilCard.Buy.Why != exampleCoconutWhy {
		t.Fatalf("coconut fallback copy:\nwhat %q\nreason %q\nwhy %q", oilCard.WhatItDoes, oilCard.Fit.Reason, oilCard.Buy.Why)
	}
	if len(oilCard.Actives) != 1 || oilCard.Actives[0].Name != coconutActiveName || oilCard.Actives[0].Gloss != exampleCoconutGloss {
		t.Fatalf("coconut active: %+v", oilCard.Actives)
	}
	if strings.Contains(oilCard.Fit.Reason, "nặng") || strings.Contains(strings.ToLower(oilCard.Buy.Why), "khiến da dễ nổi mụn") {
		t.Fatalf("coconut oil wording: reason %s why %s", oilCard.Fit.Reason, oilCard.Buy.Why)
	}
	oilBlob := strings.ToLower(oilCard.Fit.Reason + " " + oilCard.Buy.Why + " " + oilCard.WhatItDoes)
	if !strings.Contains(oilBlob, "có thể chưa hợp") || !strings.Contains(oilBlob, "bạn cân nhắc") || strings.Contains(oilBlob, "không nên bôi") {
		t.Fatalf("coconut oil card should hedge, got %s", oilBlob)
	}
	for _, a := range oilCard.Actives {
		oilBlob += " " + strings.ToLower(a.Gloss)
	}
	if strings.Contains(oilBlob, "nặng") || hasVietnameseWord(oilBlob, "bí") {
		t.Fatalf("coconut card calls the oil bad: %s", oilBlob)
	}
	hint, _ := buildWardrobeProductInsightUser(WardrobeProductInsightRequest{Name: "Dầu dừa nguyên chất", Profile: profile})
	if !strings.Contains(hint, exampleCoconutReason) || !strings.Contains(hint, "bít lỗ chân lông") || !strings.Contains(hint, "mặt") || strings.Contains(hint, "should not be applied") {
		t.Fatalf("coconut oil hint should include a passing sentence\n%s", hint)
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
	if fallback.WhatItDoes != exampleCleanserWhat {
		t.Fatalf("foaming cleanser what: %q", fallback.WhatItDoes)
	}
	useLine := strings.ToLower(fallback.Fit.Reason + " " + fallback.Buy.Why)
	if !strings.Contains(useLine, "mục tiêu giảm mụn") || strings.Contains(useLine, "làm sạch mụn") {
		t.Fatalf("goal line: %s", useLine)
	}
	if strings.Contains(strings.ToLower(fallback.WhatItDoes+" "+useLine), "la roche-posay") {
		t.Fatalf("fallback copied the brand: %+v", fallback)
	}
	readsAsEffect := dto.WardrobeProductInsight{
		WhatItDoes: "Sữa rửa mặt tạo bọt giúp làm sạch da dầu và dễ nổi mụn.",
		Fit:        good.Fit,
		Buy:        good.Buy,
		Disclaimer: dto.WardrobeInsightDisclaimer,
	}
	if problems := validateWardrobeProductInsight(readsAsEffect, facts); len(problems) == 0 {
		t.Fatal("skin-type words must not read as what the product does")
	}
	rewrittenGoal := good
	rewrittenGoal.Buy.Why = "Nên dùng tiếp vì hợp với da hỗn hợp và mục tiêu làm sạch mụn."
	if problems := validateWardrobeProductInsight(rewrittenGoal, facts); len(problems) == 0 {
		t.Fatal("goal must stay mục tiêu giảm mụn")
	}
	branded := good
	branded.WhatItDoes = "Sữa rửa mặt La Roche-Posay thường dùng để làm sạch dầu thừa."
	if problems := validateWardrobeProductInsight(branded, facts); len(problems) == 0 {
		t.Fatal("brand name must not pass")
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
		WhatItDoes: exampleCleanserWhat,
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitMaybe, Reason: "Da hỗn hợp có dấu hiệu rát gần đây, bạn cân nhắc tạm dừng."},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyNo, Why: examplePauseWhy},
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
	fallback := wardrobeInsightFallback(facts)
	if fallback.Fit.Verdict != dto.WardrobeFitNo || fallback.Buy.Advice != dto.WardrobeBuyNo {
		t.Fatalf("irritation fallback: %+v", fallback)
	}
	blob := strings.ToLower(fallback.Fit.Reason + " " + fallback.Buy.Why)
	if !strings.Contains(blob, "có thể chưa hợp") || !strings.Contains(blob, "bạn cân nhắc") {
		t.Fatalf("irritation fallback should hedge: %s", blob)
	}
	if strings.Contains(blob, "nên tạm dừng") || strings.Contains(blob, "không nên") {
		t.Fatalf("irritation fallback still commands: %s", blob)
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
	specific.WhatItDoes = "Kem dưỡng thường dùng để dưỡng ẩm cho da."
	specific.Fit.Reason = "Kem này khá đặc, có thể dễ bít lỗ chân lông với da hỗn hợp, mục tiêu giảm mụn."
	specific.Buy.Why = "Bạn cân nhắc tạm dừng, vì có thể chưa hợp với da hỗn hợp."
	if problems := validateWardrobeProductInsight(specific, facts); len(problems) != 0 {
		t.Fatalf("specific not-a-fit should pass: %v", problems)
	}
}

func TestGoalOnlyProfileSentence(t *testing.T) {
	const (
		coconutReason = "Dầu dừa khá đặc, dễ bít lỗ chân lông, nên với mục tiêu giảm mụn thì có thể chưa hợp với da mặt của bạn."
		bodyReason    = "Kem dưỡng cho cơ thể khá đặc, dễ bít lỗ chân lông, nên với mục tiêu giảm mụn thì có thể chưa hợp với da mặt của bạn."
		otherReason   = "Chưa rõ loại da. Với mục tiêu giảm mụn, bạn cân nhắc theo dõi thêm."
		otherWhy      = "Có thể chưa chắc lúc này, vì chưa rõ loại da."
		awkward       = "da mặt dễ nổi mụn, mục tiêu"
	)
	for _, goal := range []string{"Giảm mụn", "clear_acne"} {
		profile := goalOnlyProfile(goal)
		oil := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{Name: "Dầu dừa nguyên chất", Profile: profile})
		if oil.SkinType != "" || oil.SkinTypeKnown || !oil.SkinKnown || !oil.AcneProne || oil.Goal != "Giảm mụn" {
			t.Fatalf("goal-only facts for %q: %+v", goal, oil)
		}
		oilCard := wardrobeInsightFallback(oil)
		if oilCard.Fit.Verdict != dto.WardrobeFitNo || oilCard.Buy.Advice != dto.WardrobeBuyNo {
			t.Fatalf("goal-only coconut verdict: %+v", oilCard)
		}
		if oilCard.WhatItDoes != exampleCoconutWhat || oilCard.Buy.Why != exampleCoconutWhy || oilCard.Fit.Reason != coconutReason {
			t.Fatalf("goal-only coconut copy:\nwhat %q\nreason %q\nwhy %q", oilCard.WhatItDoes, oilCard.Fit.Reason, oilCard.Buy.Why)
		}
		if len(oilCard.Actives) != 1 || oilCard.Actives[0].Name != coconutActiveName || oilCard.Actives[0].Gloss != exampleCoconutGloss {
			t.Fatalf("approved coconut gloss drifted: %+v", oilCard.Actives)
		}
		if strings.Contains(oilCard.Fit.Reason, awkward) || strings.Contains(oilCard.Fit.Reason, "mục tiêu giảm mụn của bạn") {
			t.Fatalf("awkward goal-only reason: %s", oilCard.Fit.Reason)
		}
		if problems := validateWardrobeProductInsight(oilCard, oil); len(problems) != 0 {
			t.Fatalf("goal-only coconut invalid: %v", problems)
		}
		hint, _ := buildWardrobeProductInsightUser(WardrobeProductInsightRequest{Name: "Dầu dừa nguyên chất", Profile: profile})
		if !strings.Contains(hint, coconutReason) || !strings.Contains(hint, exampleCoconutGloss) || strings.Contains(hint, awkward) {
			t.Fatalf("goal-only hint:\n%s", hint)
		}

		body := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{Name: "Kem dưỡng thể", Profile: profile})
		bodyCard := wardrobeInsightFallback(body)
		if bodyCard.Fit.Reason != bodyReason || bodyCard.Fit.Verdict != dto.WardrobeFitNo || bodyCard.Buy.Advice != dto.WardrobeBuyNo {
			t.Fatalf("goal-only body: %+v", bodyCard)
		}
		if problems := validateWardrobeProductInsight(bodyCard, body); len(problems) != 0 {
			t.Fatalf("goal-only body invalid: %v", problems)
		}

		wash := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{Name: "Sữa rửa mặt tạo bọt", Profile: profile})
		washCard := wardrobeInsightFallback(wash)
		if washCard.Fit.Verdict != dto.WardrobeFitMaybe || washCard.Buy.Advice != dto.WardrobeBuyNo {
			t.Fatalf("goal-only cleanser verdict: %+v", washCard)
		}
		if washCard.WhatItDoes != exampleCleanserWhat || washCard.Fit.Reason != otherReason || washCard.Buy.Why != otherWhy {
			t.Fatalf("goal-only cleanser copy:\nwhat %q\nreason %q\nwhy %q", washCard.WhatItDoes, washCard.Fit.Reason, washCard.Buy.Why)
		}
		if strings.Contains(washCard.Fit.Reason, awkward) || strings.Contains(washCard.Fit.Reason+washCard.Buy.Why, "trị mụn") {
			t.Fatalf("goal-only cleanser wording: %+v", washCard)
		}
		if problems := validateWardrobeProductInsight(washCard, wash); len(problems) != 0 {
			t.Fatalf("goal-only cleanser invalid: %v", problems)
		}
	}

	combo := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{Name: "Dầu dừa nguyên chất", Profile: reportedComboProfile()})
	comboCard := wardrobeInsightFallback(combo)
	if comboCard.Fit.Reason != exampleCoconutReason || comboCard.WhatItDoes != exampleCoconutWhat || comboCard.Actives[0].Gloss != exampleCoconutGloss {
		t.Fatalf("combo coconut wording changed: %+v", comboCard)
	}

	empty := wardrobeInsightFallback(assembleWardrobeInsightFacts(WardrobeProductInsightRequest{Name: "Kem"}))
	if empty.Fit.Reason != "Chưa có loại da hoặc check-in gần đây để so." || empty.Buy.Why != "Chưa đủ thông tin da để biết có nên dùng tiếp." {
		t.Fatalf("empty profile copy changed: %+v", empty)
	}
	if empty.Fit.Verdict != dto.WardrobeFitMaybe || empty.Buy.Advice != dto.WardrobeBuyNo {
		t.Fatalf("empty profile verdict: %+v", empty)
	}
}

func goalOnlyProfile(goal string) *domain.SkinProfile {
	snap, _ := json.Marshal(map[string]any{"goal": goal})
	return &domain.SkinProfile{OnboardingSnapshot: snap}
}

// TestActiveNameAndGlossStaySeparate locks the cabinet payload: name and gloss
// are separate fields. The live line "Dầu dừa. dưỡng ẩm..." is not produced here.
func TestActiveNameAndGlossStaySeparate(t *testing.T) {
	facts := assembleWardrobeInsightFacts(WardrobeProductInsightRequest{
		Name:    "Dầu dừa nguyên chất",
		Profile: reportedComboProfile(),
	})
	card := wardrobeInsightFallback(facts)
	if len(card.Actives) != 1 {
		t.Fatalf("actives: %+v", card.Actives)
	}
	active := card.Actives[0]
	if active.Name != "Dầu dừa" || strings.HasSuffix(active.Name, ".") || strings.HasPrefix(active.Gloss, ".") {
		t.Fatalf("name/gloss punctuation: %+v", active)
	}
	if active.Gloss != exampleCoconutGloss {
		t.Fatalf("gloss %q", active.Gloss)
	}
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	glitch := active.Name + ". " + active.Gloss
	if strings.Contains(string(raw), glitch) || strings.Contains(string(raw), active.Name+".") {
		t.Fatalf("payload joined name and gloss: %s", raw)
	}
	var payload struct {
		Actives []struct {
			Name  string `json:"name"`
			Gloss string `json:"gloss"`
		} `json:"actives"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Actives) != 1 || payload.Actives[0].Name != active.Name || payload.Actives[0].Gloss != active.Gloss {
		t.Fatalf("round trip: %+v", payload.Actives)
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
