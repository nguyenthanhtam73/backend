package ai

import (
	"regexp"
	"strings"

	"github.com/dadiary/backend/internal/dto"
)

var (
	reMaPhaiVI     = regexp.MustCompile(`(?i)má\s+phải`)
	reMaTraiVI     = regexp.MustCompile(`(?i)má\s+trái`)
	reRightCheekEN = regexp.MustCompile(`(?i)\bright\s+cheek\b`)
	reLeftCheekEN  = regexp.MustCompile(`(?i)\bleft\s+cheek\b`)
	// Whole-token actives only — avoids "aha" matching inside unrelated words.
	reSpotActiveToken = regexp.MustCompile(`(?i)(^|[^a-z0-9])(aha|bha|pha|retinol|retinoid|retinoids)([^a-z0-9]|$)`)
)

// SoftenCheekLateralityProse replaces guessed left/right cheek labels with a safer phrase.
func SoftenCheekLateralityProse(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	out := reMaPhaiVI.ReplaceAllString(s, "má gần tai")
	out = reMaTraiVI.ReplaceAllString(out, "má gần tai")
	out = reRightCheekEN.ReplaceAllString(out, "cheek near the ear")
	out = reLeftCheekEN.ReplaceAllString(out, "cheek near the ear")
	out = strings.ReplaceAll(out, "má gần tai gần tai", "má gần tai")
	out = strings.ReplaceAll(out, "cheek near the ear near the ear", "cheek near the ear")
	return out
}

func adminSkinLooksCloseUpCheek(a *dto.AdminSkinReviewAnalysis) bool {
	if a == nil {
		return false
	}
	blob := strings.ToLower(strings.TrimSpace(a.PhotoNotes + " " + a.Overview + " " + a.SkinTypeNote))
	needles := []string{
		"close-up", "closeup", "close up",
		"chỉ thấy má", "chỉ xét được má", "thiếu trán", "crop",
		"má gần tai", "chỉ nửa mặt", "chỉ một vùng",
	}
	for _, n := range needles {
		if strings.Contains(blob, n) {
			return true
		}
	}
	// Single visible region = cheeks, others not_visible → treat as cheek close-up.
	visibleCheek := false
	otherVisible := false
	for _, ar := range a.AttentionAreas {
		c := strings.ToLower(strings.TrimSpace(ar.Concern))
		r := strings.ToLower(strings.TrimSpace(ar.Region))
		if c == "" || c == "not_visible" {
			continue
		}
		if r == "cheeks" || r == "cheek" {
			visibleCheek = true
		} else {
			otherVisible = true
		}
	}
	return visibleCheek && !otherVisible
}

func questionImpliesSpotProduct(q string) bool {
	ql := strings.ToLower(q)
	needles := []string{
		"chấm mụn", "bôi chấm", "kem chấm", "spot treatment", "spot cream",
		"đang bôi", "mới bôi", "azelaic", "benzoyl",
	}
	for _, n := range needles {
		if strings.Contains(ql, n) {
			return true
		}
	}
	return reSpotActiveToken.MatchString(q)
}

func questionAsksWrongStep(q string) bool {
	ql := strings.ToLower(q)
	needles := []string{"sai bước", "sai ở bước", "which step", "what step", "doing wrong", "làm sai"}
	for _, n := range needles {
		if strings.Contains(ql, n) {
			return true
		}
	}
	return false
}

func questionClaimsOily(q string) bool {
	ql := strings.ToLower(q)
	return strings.Contains(ql, "nhiều dầu") ||
		strings.Contains(ql, "da dầu") ||
		strings.Contains(ql, "oily") ||
		strings.Contains(ql, "dầu nhiều")
}

func questionAsksLaser(q string) bool {
	ql := strings.ToLower(q)
	// Narrow: treatment intent only — NOT bare "bệnh viện" / "phòng khám".
	if strings.Contains(ql, "laser") || strings.Contains(ql, "lasẻ") {
		return true
	}
	if strings.Contains(ql, "trị thâm") || strings.Contains(ql, "tri tham") {
		return true
	}
	if strings.Contains(ql, "peel") && (strings.Contains(ql, "thâm") || strings.Contains(ql, "chemical") || strings.Contains(ql, "hoa chất")) {
		return true
	}
	return false
}

func questionLooksClinicShopping(q string) bool {
	ql := strings.ToLower(q)
	needles := []string{
		"chỗ nào tốt", "ở đâu tốt", "nên khám ở đâu", "recommend",
		"bao nhiêu buổi", "bao nhiêu giá", "giá sao", "giá bao", "chi phí",
		"bệnh viện nào", "phòng khám nào", "thẩm mỹ viện nào", "clinic nào",
	}
	for _, n := range needles {
		if strings.Contains(ql, n) {
			return true
		}
	}
	// "giá" + (laser|buổi|trị)
	if strings.Contains(ql, "giá") && (questionAsksLaser(q) || strings.Contains(ql, "buổi")) {
		return true
	}
	return false
}

// VisionSafeUserQuestionHint strips clinic-shopping / price prompts that often
// trigger vision refusal. Align + suggest-answer still receive the full question.
func VisionSafeUserQuestionHint(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	if !questionLooksClinicShopping(q) && !questionAsksLaser(q) {
		return q
	}
	var parts []string
	ql := strings.ToLower(q)
	if strings.Contains(ql, "thâm") || strings.Contains(ql, "sắc tố") || strings.Contains(ql, "đốm") || questionAsksLaser(q) {
		parts = append(parts, "đang quan tâm thâm/sắc tố")
	}
	if questionClaimsOily(q) {
		parts = append(parts, "da nhiều dầu")
	}
	if questionImpliesSpotProduct(q) {
		parts = append(parts, "đang bôi chấm/trị tại chỗ")
	}
	if len(parts) == 0 {
		return "Chỉ mô tả những gì thấy trên ảnh da. Không tư vấn chỗ khám, giá, hay số buổi điều trị."
	}
	return strings.Join(parts, "; ") + ". Chỉ mô tả ảnh — không gợi ý phòng khám/bệnh viện, giá, hay số buổi."
}

func adminSkinTextBlob(a *dto.AdminSkinReviewAnalysis) string {
	if a == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(a.Overview)
	b.WriteByte(' ')
	b.WriteString(a.AdditionalObservations)
	b.WriteByte(' ')
	b.WriteString(a.PhotoNotes)
	b.WriteByte(' ')
	b.WriteString(a.SkinTypeNote)
	for _, ar := range a.AttentionAreas {
		b.WriteByte(' ')
		b.WriteString(ar.Note)
		b.WriteByte(' ')
		b.WriteString(ar.Region)
		b.WriteByte(' ')
		b.WriteString(ar.Concern)
	}
	return strings.ToLower(b.String())
}

func questionAsksPeriOralTham(q string) bool {
	ql := strings.ToLower(q)
	tham := strings.Contains(ql, "thâm") || strings.Contains(ql, "pigment") || strings.Contains(ql, "đốm nâu") || strings.Contains(ql, "dark mark")
	mouth := strings.Contains(ql, "mép") || strings.Contains(ql, "khóe") ||
		strings.Contains(ql, "miệng") || strings.Contains(ql, "cằm") ||
		strings.Contains(ql, "mouth") || strings.Contains(ql, "lip")
	return tham && mouth
}

func questionHasAcuteLipSignals(q string) bool {
	ql := strings.ToLower(q)
	mouthMove := strings.Contains(ql, "mở miệng") || strings.Contains(ql, "há miệng") || strings.Contains(ql, "open my mouth")
	pain := strings.Contains(ql, "đau") || strings.Contains(ql, "chằn") || strings.Contains(ql, "cộm")
	progress := (strings.Contains(ql, "nhô") || strings.Contains(ql, "nổi")) &&
		(strings.Contains(ql, "sáng") || strings.Contains(ql, "trưa") || strings.Contains(ql, "giờ") || strings.Contains(ql, "nhanh"))
	return (mouthMove && pain) || (progress && (pain || mouthMove))
}

func analysisHasStrongAcuteLipCluster(a *dto.AdminSkinReviewAnalysis) bool {
	blob := adminSkinTextBlob(a)
	if blob == "" {
		return false
	}
	peri := strings.Contains(blob, "mép") || strings.Contains(blob, "khóe") ||
		strings.Contains(blob, "viền môi") || strings.Contains(blob, "lip")
	if !peri {
		return false
	}
	// Affirmative cluster only — ignore “không chùm hạt đỏ…” denials.
	hasCluster := (strings.Contains(blob, "chùm hạt đỏ") || strings.Contains(blob, "chùm hạt đỏ sưng")) &&
		!strings.Contains(blob, "không chùm hạt đỏ") &&
		!strings.Contains(blob, "ko chùm hạt đỏ") &&
		!strings.Contains(blob, "chưa có chùm")
	hasRedSwell := strings.Contains(blob, "đỏ sưng") &&
		(strings.Contains(blob, "chùm") || strings.Contains(blob, "hạt")) &&
		!strings.Contains(blob, "không chùm") &&
		!strings.Contains(blob, "không đỏ sưng")
	hasBrightHead := strings.Contains(blob, "đầu sáng") && strings.Contains(blob, "sưng")
	return hasCluster || hasRedSwell || hasBrightHead
}

func analysisLooksWrongAcuteLipTemplate(a *dto.AdminSkinReviewAnalysis) bool {
	blob := adminSkinTextBlob(a)
	if !strings.Contains(blob, "viêm cấp sát mép") && !strings.Contains(blob, "viêm cấp sát mép miệng") {
		return false
	}
	// Deny / meta lines ("không gọi viêm cấp…", "không phải viêm cấp…") are not positive labels.
	if !hasAffirmativeViemCapLabel(blob) {
		return false
	}
	// Template-only / weak: has viêm cấp label but not a strong red cluster description.
	return !analysisHasStrongAcuteLipCluster(a)
}

// hasAffirmativeViemCapLabel is true when prose claims acute lip-edge, not when it denies it.
func hasAffirmativeViemCapLabel(blob string) bool {
	if !strings.Contains(blob, "viêm cấp sát mép") {
		return false
	}
	// Strip common denial prefixes so leftover "viêm cấp sát mép" still counts if used affirmatively elsewhere.
	cleaned := blob
	for _, denial := range []string{
		"không gọi viêm cấp sát mép miệng",
		"không gọi viêm cấp sát mép",
		"không phải viêm cấp sát mép miệng",
		"không phải viêm cấp sát mép",
		"chứ không phải viêm cấp sát mép",
		"không phải ổ viêm cấp sát mép",
		"cấm viêm cấp sát mép",
	} {
		cleaned = strings.ReplaceAll(cleaned, denial, " ")
	}
	return strings.Contains(cleaned, "viêm cấp sát mép")
}

func rewritePeriOralViemCapToTham(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	out := s
	// Handle denials / meta first so we never produce “Không gọi thâm/sắc tố…”.
	negRepls := []struct{ old, neu string }{
		{"Không gọi viêm cấp sát mép miệng khi không có sưng đau cấp.", "Không phải ổ viêm đỏ sưng cấp."},
		{"Không gọi viêm cấp sát mép khi không có sưng đau cấp.", "Không phải ổ viêm đỏ sưng cấp."},
		{"không gọi viêm cấp sát mép miệng khi không có sưng đau cấp.", "không phải ổ viêm đỏ sưng cấp."},
		{"không gọi viêm cấp sát mép khi không có sưng đau cấp.", "không phải ổ viêm đỏ sưng cấp."},
		{"Không gọi viêm cấp sát mép miệng", "Không phải ổ viêm đỏ sưng cấp"},
		{"Không gọi viêm cấp sát mép", "Không phải ổ viêm đỏ sưng cấp"},
		{"không gọi viêm cấp sát mép miệng", "không phải ổ viêm đỏ sưng cấp"},
		{"không gọi viêm cấp sát mép", "không phải ổ viêm đỏ sưng cấp"},
		{"không phải viêm cấp sát mép miệng", "không phải ổ viêm đỏ sưng cấp"},
		{"không phải viêm cấp sát mép", "không phải ổ viêm đỏ sưng cấp"},
		{"Không phải viêm cấp sát mép miệng", "Không phải ổ viêm đỏ sưng cấp"},
		{"Không phải viêm cấp sát mép", "Không phải ổ viêm đỏ sưng cấp"},
	}
	for _, r := range negRepls {
		out = strings.ReplaceAll(out, r.old, r.neu)
	}
	repls := []struct{ old, neu string }{
		{"viêm cấp sát mép miệng", "thâm/sắc tố quanh miệng"},
		{"Viêm cấp sát mép miệng", "Thâm/sắc tố quanh miệng"},
		{"viêm cấp sát mép", "thâm/sắc tố quanh miệng"},
		{"Viêm cấp sát mép", "Thâm/sắc tố quanh miệng"},
		{"chùm hạt đỏ sưng ngay viền môi", "mảng thâm nâu–xám quanh khóe miệng"},
		{"Chùm hạt đỏ sưng ngay viền môi", "Mảng thâm nâu–xám quanh khóe miệng"},
		{"không nên xử như mụn thường trên má", "đây đúng kiểu thâm quanh miệng chứ không phải ổ viêm cấp"},
		{"đừng mặc định bôi/trị như mụn có mủ", "đừng kỳ vọng hết thâm chỉ sau vài ngày tẩy mạnh"},
	}
	for _, r := range repls {
		out = strings.ReplaceAll(out, r.old, r.neu)
	}
	// Repair already-mangled meta lines from older align runs / copied few-shots.
	broken := []struct{ old, neu string }{
		{"Không gọi thâm/sắc tố quanh miệng khi không có sưng đau cấp.", "Không phải ổ viêm đỏ sưng cấp."},
		{"không gọi thâm/sắc tố quanh miệng khi không có sưng đau cấp.", "không phải ổ viêm đỏ sưng cấp."},
		{"Không gọi thâm/sắc tố quanh miệng", "Không phải ổ viêm đỏ sưng cấp"},
		{"không gọi thâm/sắc tố quanh miệng", "không phải ổ viêm đỏ sưng cấp"},
	}
	for _, r := range broken {
		out = strings.ReplaceAll(out, r.old, r.neu)
	}
	return out
}

// alignPeriOralThamVsAcute fixes mislabeled “viêm cấp sát mép” when the user
// asks about peri-oral thâm and the photo lacks a strong acute red cluster.
// Strong acute cluster on photo always wins (keeps B).
func alignPeriOralThamVsAcute(a *dto.AdminSkinReviewAnalysis, question, locale string) bool {
	if a == nil {
		return false
	}
	if analysisHasStrongAcuteLipCluster(a) {
		return false // Photo acute cluster wins — keep khung B.
	}
	q := strings.TrimSpace(question)
	blob := adminSkinTextBlob(a)
	periOralBlob := strings.Contains(blob, "mép") || strings.Contains(blob, "khóe") ||
		strings.Contains(blob, "viền môi") || strings.Contains(blob, "quanh miệng")
	wrongViemCap := analysisLooksWrongAcuteLipTemplate(a)
	// Only rewrite when peri-oral context is present — don't strip inflamed tips from unrelated chin acne.
	asksTham := questionAsksPeriOralTham(q)
	affirmativeViem := hasAffirmativeViemCapLabel(blob)
	// Denial-only “không phải/không gọi viêm cấp…” must NOT trigger a rewrite pass.
	mislabelHint := wrongViemCap || affirmativeViem ||
		(asksTham && strings.Contains(blob, "kích ứng") && !strings.Contains(blob, "thâm") && !strings.Contains(blob, "sắc tố"))
	forceA := !questionHasAcuteLipSignals(q) && periOralBlob && (wrongViemCap || (asksTham && mislabelHint))
	if !forceA && wrongViemCap && !questionHasAcuteLipSignals(q) {
		if strings.Contains(blob, "thâm") || strings.Contains(blob, "nâu") || strings.Contains(blob, "sẫm") {
			forceA = true
		}
	}
	// Always repair mangled “Không gọi thâm…” lines left by older align / copied few-shots.
	needsMangleRepair := strings.Contains(blob, "không gọi thâm") || strings.Contains(blob, "không gọi viêm cấp sát mép")
	if !forceA && !needsMangleRepair {
		return false
	}
	if needsMangleRepair && !forceA {
		changed := false
		repair := func(s string) string {
			n := rewritePeriOralViemCapToTham(s)
			if n != s {
				changed = true
			}
			return n
		}
		a.Overview = repair(a.Overview)
		a.AdditionalObservations = repair(a.AdditionalObservations)
		a.PhotoNotes = repair(a.PhotoNotes)
		a.SkinTypeNote = repair(a.SkinTypeNote)
		for i := range a.AttentionAreas {
			a.AttentionAreas[i].Note = repair(a.AttentionAreas[i].Note)
		}
		return changed
	}

	changed := false
	rewrite := func(s string) string {
		n := rewritePeriOralViemCapToTham(s)
		if n != s {
			changed = true
		}
		return n
	}
	a.Overview = rewrite(a.Overview)
	a.AdditionalObservations = rewrite(a.AdditionalObservations)
	a.PhotoNotes = rewrite(a.PhotoNotes)
	a.SkinTypeNote = rewrite(a.SkinTypeNote)
	for i := range a.AttentionAreas {
		a.AttentionAreas[i].Note = rewrite(a.AttentionAreas[i].Note)
		c := strings.ToLower(strings.TrimSpace(a.AttentionAreas[i].Concern))
		r := strings.ToLower(strings.TrimSpace(a.AttentionAreas[i].Region))
		if (r == "chin" || r == "other" || r == "cheeks") && (c == "irritation" || c == "acne" || c == "pustules" || c == "papules") {
			note := strings.ToLower(a.AttentionAreas[i].Note)
			if strings.Contains(note, "thâm") || strings.Contains(note, "sắc tố") || strings.Contains(note, "mép") || strings.Contains(note, "khóe") {
				a.AttentionAreas[i].Concern = "pigmentation"
				changed = true
			}
		}
	}

	filteredTips := make([]string, 0, len(a.SoothingTips))
	for _, tip := range a.SoothingTips {
		tl := strings.ToLower(tip)
		if tipLooksInflamedOnly(tip) || strings.Contains(tl, "không nặn") || strings.Contains(tl, "không bóc") ||
			strings.Contains(tl, "mụn có mủ") || strings.Contains(tl, "viêm cấp") {
			changed = true
			continue
		}
		filteredTips = append(filteredTips, tip)
	}
	a.SoothingTips = filteredTips
	spf := "Chống nắng đều mỗi ngày trên vùng thâm quanh miệng–cằm."
	gentle := "Giữ routine dịu, đừng chà mạnh chỗ thâm."
	if locale == "en" {
		spf = "Wear sunscreen daily on the darkened areas around the mouth/chin."
		gentle = "Keep the routine gentle — don't scrub the marks."
	}
	if !containsFold(a.SoothingTips, "chống nắng") && !containsFold(a.SoothingTips, "sunscreen") {
		a.SoothingTips = append([]string{spf}, a.SoothingTips...)
		changed = true
	}
	if !containsFold(a.SoothingTips, "dịu") && !containsFold(a.SoothingTips, "gentle") && !containsFold(a.SoothingTips, "chà") {
		a.SoothingTips = append(a.SoothingTips, gentle)
		changed = true
	}
	if len(a.SoothingTips) > 3 {
		a.SoothingTips = a.SoothingTips[:3]
	}

	causes := make([]string, 0, len(a.PossibleCauses))
	for _, c := range a.PossibleCauses {
		cl := strings.ToLower(c)
		if strings.Contains(cl, "kích ứng tại chỗ quanh mép") || (strings.Contains(cl, "kích ứng") && strings.Contains(cl, "mép")) {
			changed = true
			continue
		}
		causes = append(causes, c)
	}
	a.PossibleCauses = causes
	pigCause := "Do thâm sau mụn hoặc nắng/ma sát cục bộ quanh miệng."
	if locale == "en" {
		pigCause = "Post-acne marks or local sun/friction around the mouth."
	}
	if len(a.PossibleCauses) == 0 || (!containsFold(a.PossibleCauses, "thâm sau") && !containsFold(a.PossibleCauses, "post-acne") && !containsFold(a.PossibleCauses, "nắng")) {
		a.PossibleCauses = append([]string{pigCause}, a.PossibleCauses...)
		if len(a.PossibleCauses) > 2 {
			a.PossibleCauses = a.PossibleCauses[:2]
		}
		changed = true
	}
	return changed
}

// AdminSkinPigmentPrimary reports pigment/dark-spot dominant reviews (little acute inflammation).
func AdminSkinPigmentPrimary(a *dto.AdminSkinReviewAnalysis) bool {
	if a == nil {
		return false
	}
	pigment := false
	inflamed := false
	for _, ar := range a.AttentionAreas {
		c := strings.ToLower(strings.TrimSpace(ar.Concern))
		switch c {
		case "pigmentation", "dark_spots":
			pigment = true
		case "acne", "papules", "pustules", "redness", "irritation":
			inflamed = true
		}
	}
	blob := strings.ToLower(a.Overview + " " + a.AdditionalObservations)
	if !pigment && (strings.Contains(blob, "thâm") || strings.Contains(blob, "đốm nâu") || strings.Contains(blob, "sắc tố") || strings.Contains(blob, "pigment")) {
		pigment = true
	}
	if strings.Contains(blob, "mụn viêm") || strings.Contains(blob, "đỏ sưng") || strings.Contains(blob, "đầu trắng") {
		inflamed = true
	}
	return pigment && !inflamed
}

func causeLooksCircularPigment(cause string) bool {
	cl := strings.ToLower(strings.TrimSpace(cause))
	if cl == "" {
		return false
	}
	// "thâm sau mụn" / "nắng" are real directions — keep those.
	if strings.Contains(cl, "sau mụn") || strings.Contains(cl, "post-acne") ||
		strings.Contains(cl, "nắng") || strings.Contains(cl, "sun") ||
		strings.Contains(cl, "dầu") || strings.Contains(cl, "oil") ||
		strings.Contains(cl, "cọ") || strings.Contains(cl, "nặn") {
		return false
	}
	// Pure restatement: "do thâm", "do sắc tố", "vì đốm nâu"...
	circular := []string{
		"do thâm", "vì thâm", "từ thâm", "do sắc tố", "vì sắc tố",
		"do đốm", "vì đốm", "due to pigment", "due to dark", "from pigmentation",
		"do tăng sắc tố", "vì tăng sắc tố",
	}
	for _, n := range circular {
		if strings.Contains(cl, n) {
			return true
		}
	}
	// Bare "thâm"/"sắc tố" as the only explanation.
	stripped := strings.TrimSpace(strings.TrimPrefix(cl, "do "))
	stripped = strings.TrimSpace(strings.TrimPrefix(stripped, "vì "))
	if stripped == "thâm" || stripped == "sắc tố" || stripped == "đốm nâu" || stripped == "pigmentation" {
		return true
	}
	return false
}

func tipLooksInflamedOnly(tip string) bool {
	tl := strings.ToLower(tip)
	return strings.Contains(tl, "nặn") ||
		strings.Contains(tl, "ổ đang sưng") ||
		strings.Contains(tl, "đang sưng") ||
		strings.Contains(tl, "pick") ||
		strings.Contains(tl, "inflamed")
}

func tipLooksPauseStrongProduct(tip string) bool {
	tl := strings.ToLower(tip)
	return strings.Contains(tl, "tạm nghỉ") ||
		strings.Contains(tl, "tạm tránh") ||
		strings.Contains(tl, "pause") ||
		strings.Contains(tl, "sản phẩm mạnh") ||
		strings.Contains(tl, "sản phẩm trị mụn mạnh") ||
		strings.Contains(tl, "strong active") ||
		strings.Contains(tl, "strong treatment")
}

func tipLooksOilShineCause(cause string) bool {
	cl := strings.ToLower(cause)
	return (strings.Contains(cl, "bóng") || strings.Contains(cl, "shine") || strings.Contains(cl, "oil")) &&
		(strings.Contains(cl, "dầu") || strings.Contains(cl, "bít") || strings.Contains(cl, "clog"))
}

func questionMentionsShine(q string) bool {
	ql := strings.ToLower(q)
	return strings.Contains(ql, "bóng") || strings.Contains(ql, "shine") || strings.Contains(ql, "shiny")
}

func adminSkinLooksSingleRegionFaceCrop(a *dto.AdminSkinReviewAnalysis) bool {
	if a == nil {
		return false
	}
	blob := strings.ToLower(strings.TrimSpace(a.PhotoNotes + " " + a.Overview + " " + a.SkinTypeNote))
	for _, n := range []string{
		"chỉ thấy trán", "chỉ thấy má", "chỉ xét được trán", "chỉ xét được má",
		"crop chỉ", "close-up", "closeup", "close up", "thiếu mũi", "thiếu trán", "thiếu má",
	} {
		if strings.Contains(blob, n) {
			return true
		}
	}
	visible := 0
	for _, ar := range a.AttentionAreas {
		c := strings.ToLower(strings.TrimSpace(ar.Concern))
		if c == "" || c == "not_visible" {
			continue
		}
		visible++
	}
	return visible == 1
}

func proseDeniesWhiteheads(s string) bool {
	sl := strings.ToLower(s)
	needles := []string{
		"không thấy đầu trắng", "không có đầu trắng", "chưa thấy đầu trắng",
		"no whitehead", "no whiteheads", "without whiteheads", "no visible white",
	}
	for _, n := range needles {
		if strings.Contains(sl, n) {
			return true
		}
	}
	return false
}

func alignPustuleConcernWithProse(a *dto.AdminSkinReviewAnalysis) bool {
	if a == nil {
		return false
	}
	changed := false
	for i := range a.AttentionAreas {
		ar := &a.AttentionAreas[i]
		if strings.ToLower(strings.TrimSpace(ar.Concern)) != "pustules" {
			continue
		}
		if proseDeniesWhiteheads(ar.Note) || proseDeniesWhiteheads(a.Overview) {
			ar.Concern = "papules"
			changed = true
		}
	}
	return changed
}

var reBongDauVI = regexp.MustCompile(`(?i)bóng\s*dầu(\s+rõ(\s+rệt)?)?`)

func softenOilShineProseForSpotCream(s, locale string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	if locale == "en" {
		out := s
		out = strings.ReplaceAll(out, "oily shine", "product-film shine")
		out = strings.ReplaceAll(out, "Oily shine", "Product-film shine")
		out = strings.ReplaceAll(out, "clear oily sheen", "product-film sheen")
		out = strings.ReplaceAll(out, "very oily", "shiny from product")
		return out
	}
	return reBongDauVI.ReplaceAllString(s, "bóng kiểu lớp kem")
}

func looksMunThitProse(s string) bool {
	t := strings.ToLower(s)
	return strings.Contains(t, "mụn thịt") || strings.Contains(t, "skin tag") || strings.Contains(t, "skin-tag")
}

func adminSkinFaceRegion(r string) bool {
	switch strings.ToLower(strings.TrimSpace(r)) {
	case "cheeks", "cheek", "forehead", "chin", "nose", "jawline", "t_zone", "under_eyes":
		return true
	default:
		return false
	}
}

var (
	reTronGiongMunThit = regexp.MustCompile(`(?i)(đây\s+)?trông\s+giống\s+mụn\s+thịt(\s+ở\s+(cổ|má|nách|mặt))?`)
	reMunThitToken     = regexp.MustCompile(`(?i)mụn\s+thịt`)
	reSkinTagToken     = regexp.MustCompile(`(?i)skin[\s-]?tags?`)
)

// rewriteFaceMunThitToMilia replaces cheek/face "mụn thịt" mislabels with the
// required closed-comedone / milia phrasing. Leaves explicit denials alone.
func rewriteFaceMunThitToMilia(s, locale string) string {
	if strings.TrimSpace(s) == "" || !looksMunThitProse(s) {
		return s
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "không phải mụn thịt") || strings.Contains(lower, "không gọi mụn thịt") ||
		strings.Contains(lower, "not a skin tag") || strings.Contains(lower, "not skin tags") {
		return s
	}
	out := reTronGiongMunThit.ReplaceAllString(s, "trông giống mụn ẩn hoặc milia")
	if locale == "en" {
		out = reSkinTagToken.ReplaceAllString(out, "closed comedones or milia")
		out = strings.ReplaceAll(out, "Closed comedones or milia", "closed comedones or milia")
		return out
	}
	out = reMunThitToken.ReplaceAllString(out, "mụn ẩn hoặc milia")
	return out
}

// alignCheekSkinTagMislabel rewrites "mụn thịt" on cheeks/face (not neck/axilla)
// to the safer mụn ẩn / milia phrasing.
func alignCheekSkinTagMislabel(a *dto.AdminSkinReviewAnalysis, locale string) bool {
	if a == nil {
		return false
	}
	hasFaceMunThit := false
	for _, ar := range a.AttentionAreas {
		c := strings.ToLower(strings.TrimSpace(ar.Concern))
		if c == "not_visible" {
			continue
		}
		if adminSkinFaceRegion(ar.Region) && looksMunThitProse(ar.Note) {
			hasFaceMunThit = true
			break
		}
	}
	blob := strings.ToLower(a.Overview + " " + a.AdditionalObservations)
	mentionsMa := strings.Contains(blob, "má") || strings.Contains(blob, "cheek")
	mentionsNeckSite := strings.Contains(blob, "cổ") || strings.Contains(blob, "nách") ||
		strings.Contains(blob, "neck") || strings.Contains(blob, "axilla")
	if !hasFaceMunThit && looksMunThitProse(a.Overview) && mentionsMa && !mentionsNeckSite {
		hasFaceMunThit = true
	}
	if !hasFaceMunThit {
		return false
	}

	changed := false
	rewrite := func(s string) string {
		n := rewriteFaceMunThitToMilia(s, locale)
		if n != s {
			changed = true
		}
		return n
	}
	a.Overview = rewrite(a.Overview)
	a.AdditionalObservations = rewrite(a.AdditionalObservations)
	a.PhotoNotes = rewrite(a.PhotoNotes)
	a.SkinTypeNote = rewrite(a.SkinTypeNote)
	for i := range a.AttentionAreas {
		c := strings.ToLower(strings.TrimSpace(a.AttentionAreas[i].Concern))
		if c == "not_visible" {
			continue
		}
		if !adminSkinFaceRegion(a.AttentionAreas[i].Region) {
			continue
		}
		a.AttentionAreas[i].Note = rewrite(a.AttentionAreas[i].Note)
		if looksMunThitProse(a.AttentionAreas[i].Note) {
			continue
		}
		if c == "other" {
			a.AttentionAreas[i].Concern = "papules"
			changed = true
		}
	}

	causes := make([]string, 0, len(a.PossibleCauses))
	for _, cause := range a.PossibleCauses {
		cl := strings.ToLower(cause)
		if (strings.Contains(cl, "cổ") || strings.Contains(cl, "nách") || strings.Contains(cl, "neck") ||
			strings.Contains(cl, "axilla")) &&
			(strings.Contains(cl, "cọ") || strings.Contains(cl, "nếp") || strings.Contains(cl, "friction") ||
				strings.Contains(cl, "fold")) {
			changed = true
			continue
		}
		causes = append(causes, cause)
	}
	a.PossibleCauses = causes
	miliaCause := "Do dầu bít tắc trên má (mụn ẩn / milia)."
	if locale == "en" {
		miliaCause = "Clogged pores on the cheeks (closed comedones / milia)."
	}
	if len(a.PossibleCauses) == 0 ||
		(!containsFold(a.PossibleCauses, "mụn ẩn") && !containsFold(a.PossibleCauses, "milia") &&
			!containsFold(a.PossibleCauses, "closed comedone") && !containsFold(a.PossibleCauses, "bít tắc")) {
		a.PossibleCauses = append([]string{miliaCause}, a.PossibleCauses...)
		if len(a.PossibleCauses) > 2 {
			a.PossibleCauses = a.PossibleCauses[:2]
		}
		changed = true
	}

	hasDIYBan := false
	for _, tip := range a.SoothingTips {
		tl := strings.ToLower(tip)
		if strings.Contains(tl, "cắt") || strings.Contains(tl, "chà") || strings.Contains(tl, "nặn") ||
			strings.Contains(tl, "cut") || strings.Contains(tl, "scrub") || strings.Contains(tl, "pick") {
			hasDIYBan = true
			break
		}
	}
	if !hasDIYBan {
		diy := "Không tự cắt, nặn hay chà mạnh nốt này."
		clinic := "Muốn xử lý thì đến cơ sở y tế / da liễu — đừng tự lấy tại nhà."
		if locale == "en" {
			diy = "Don't cut, pick, or scrub these bumps hard."
			clinic = "See a clinic/dermatologist if you want them treated — don't DIY."
		}
		a.SoothingTips = append([]string{diy, clinic}, a.SoothingTips...)
		if len(a.SoothingTips) > 3 {
			a.SoothingTips = a.SoothingTips[:3]
		}
		changed = true
	}
	return changed
}

// AlignAdminSkinAnalysisWithQuestion softens close-up laterality and rewrites
// public causes/tips so they don't contradict the user's stated context.
// Returns true when any field changed.
func AlignAdminSkinAnalysisWithQuestion(a *dto.AdminSkinReviewAnalysis, question, locale string) bool {
	if a == nil {
		return false
	}
	q := strings.TrimSpace(question)
	changed := false

	if alignPustuleConcernWithProse(a) {
		changed = true
	}

	soften := adminSkinLooksCloseUpCheek(a)
	if soften {
		if s := SoftenCheekLateralityProse(a.Overview); s != a.Overview {
			a.Overview = s
			changed = true
		}
		if s := SoftenCheekLateralityProse(a.AdditionalObservations); s != a.AdditionalObservations {
			a.AdditionalObservations = s
			changed = true
		}
		if s := SoftenCheekLateralityProse(a.PhotoNotes); s != a.PhotoNotes {
			a.PhotoNotes = s
			changed = true
		}
		if s := SoftenCheekLateralityProse(a.SkinTypeNote); s != a.SkinTypeNote {
			a.SkinTypeNote = s
			changed = true
		}
		for i := range a.AttentionAreas {
			if s := SoftenCheekLateralityProse(a.AttentionAreas[i].Note); s != a.AttentionAreas[i].Note {
				a.AttentionAreas[i].Note = s
				changed = true
			}
		}
	}

	if trimRepeatedThamInAdditional(a) {
		changed = true
	}
	if trimOverviewCheekOverlap(a) {
		changed = true
	}
	if alignPeriOralThamVsAcute(a, q, locale) {
		changed = true
	}
	if alignCheekSkinTagMislabel(a, locale) {
		changed = true
	}

	// Photo-only alignments (run even when user_question is empty).
	if len(a.PossibleCauses) > 0 {
		causes := make([]string, 0, len(a.PossibleCauses))
		for _, c := range a.PossibleCauses {
			if causeLooksCircularPigment(c) {
				changed = true
				continue
			}
			causes = append(causes, c)
		}
		a.PossibleCauses = causes
	}

	pigmentPrimary := AdminSkinPigmentPrimary(a)
	if pigmentPrimary {
		filtered := make([]string, 0, len(a.SoothingTips))
		for _, tip := range a.SoothingTips {
			if tipLooksInflamedOnly(tip) || tipLooksPauseStrongProduct(tip) {
				changed = true
				continue
			}
			filtered = append(filtered, tip)
		}
		a.SoothingTips = filtered
	}

	if q == "" {
		return finalizeAdminSkinAlign(a, pigmentPrimary, locale, changed)
	}

	if questionImpliesSpotProduct(q) {
		filtered := make([]string, 0, len(a.SoothingTips))
		for _, tip := range a.SoothingTips {
			if tipLooksPauseStrongProduct(tip) {
				changed = true
				continue
			}
			filtered = append(filtered, tip)
		}
		a.SoothingTips = filtered

		causes := make([]string, 0, len(a.PossibleCauses))
		for _, c := range a.PossibleCauses {
			if tipLooksOilShineCause(c) && questionMentionsShine(q) {
				changed = true
				continue
			}
			causes = append(causes, c)
		}
		a.PossibleCauses = causes

		replacement := "Chỗ bóng đúng kiểu lớp kem/chấm mụn đang bôi — không phải da đổ dầu cả vùng."
		if locale == "en" {
			replacement = "The shine looks like spot-cream film — not oil all over the face."
		}
		if !containsFold(a.PossibleCauses, "lớp kem") && !containsFold(a.PossibleCauses, "spot cream") &&
			questionMentionsShine(q) {
			a.PossibleCauses = append([]string{replacement}, a.PossibleCauses...)
			if len(a.PossibleCauses) > 2 {
				a.PossibleCauses = a.PossibleCauses[:2]
			}
			changed = true
		}

		keepTip := "Cứ chấm đúng nốt, đừng nặn ổ đang sưng."
		if locale == "en" {
			keepTip = "Keep spot treatment on the bump — don't pick inflamed spots."
		}
		if !containsFold(a.SoothingTips, "chấm") && !containsFold(a.SoothingTips, "spot") {
			a.SoothingTips = append([]string{keepTip}, a.SoothingTips...)
			changed = true
		}

		// Spot cream + shine: don't lock oily / "bóng dầu" from product film on a crop.
		if questionMentionsShine(q) {
			if s := softenOilShineProseForSpotCream(a.Overview, locale); s != a.Overview {
				a.Overview = s
				changed = true
			}
			if s := softenOilShineProseForSpotCream(a.AdditionalObservations, locale); s != a.AdditionalObservations {
				a.AdditionalObservations = s
				changed = true
			}
			if s := softenOilShineProseForSpotCream(a.SkinTypeNote, locale); s != a.SkinTypeNote {
				a.SkinTypeNote = s
				changed = true
			}
			if s := softenOilShineProseForSpotCream(a.PhotoNotes, locale); s != a.PhotoNotes {
				a.PhotoNotes = s
				changed = true
			}
			for i := range a.AttentionAreas {
				if s := softenOilShineProseForSpotCream(a.AttentionAreas[i].Note, locale); s != a.AttentionAreas[i].Note {
					a.AttentionAreas[i].Note = s
					changed = true
				}
			}
			if !questionClaimsOily(q) && adminSkinLooksSingleRegionFaceCrop(a) &&
				strings.EqualFold(strings.TrimSpace(a.SkinType), "oily") {
				a.SkinType = "unclear"
				if locale == "en" {
					a.SkinTypeNote = "Only one face region is in frame — not enough to lock full-face skin type. The shine matches spot cream just applied — don't use that shine to call the whole face oily."
				} else {
					a.SkinTypeNote = "Chỉ thấy một vùng trên ảnh — tao chưa đủ chốt loại da cả mặt cho mày. Chỗ bóng khớp lớp kem đang bôi — không lấy bóng đó chốt da dầu cả mặt."
				}
				changed = true
			}
		}
	}

	if questionAsksWrongStep(q) {
		filtered := make([]string, 0, len(a.SoothingTips))
		for _, tip := range a.SoothingTips {
			if tipLooksPauseStrongProduct(tip) {
				changed = true
				continue
			}
			filtered = append(filtered, tip)
		}
		a.SoothingTips = filtered
		askTip := "Kể đang dùng gì / có hay nặn không — từ ảnh thôi chưa chốt sai bước nào."
		if locale == "en" {
			askTip = "Tell what you're using / if you pick — a photo alone can't name the wrong step."
		}
		if !containsFold(a.SoothingTips, "đang dùng") && !containsFold(a.SoothingTips, "what you're using") {
			a.SoothingTips = append([]string{askTip}, a.SoothingTips...)
			changed = true
		}
	}

	if questionClaimsOily(q) && strings.TrimSpace(a.SkinTypeNote) != "" {
		note := a.SkinTypeNote
		if !strings.Contains(strings.ToLower(note), "dầu") && !strings.Contains(strings.ToLower(note), "oil") {
			if locale == "en" {
				a.SkinTypeNote = note + " You said your skin runs oily — that matches the local clogged-bump pattern on the photo."
			} else {
				a.SkinTypeNote = note + " Mày nói da nhiều dầu — khớp kiểu bít tắc cục bộ trên ảnh."
			}
			changed = true
		}
	}

	laserQ := questionAsksLaser(q)
	thamQ := strings.Contains(strings.ToLower(q), "thâm") || strings.Contains(strings.ToLower(q), "pigment") || laserQ

	// Laser / thâm questions: drop pause-strong tips; ensure SPF + local-derm guidance.
	if laserQ || (thamQ && pigmentPrimary) {
		filtered := make([]string, 0, len(a.SoothingTips))
		for _, tip := range a.SoothingTips {
			if tipLooksPauseStrongProduct(tip) {
				changed = true
				continue
			}
			if pigmentPrimary && tipLooksInflamedOnly(tip) {
				changed = true
				continue
			}
			filtered = append(filtered, tip)
		}
		a.SoothingTips = filtered
		spf := "Chống nắng đều mỗi ngày trên vùng thâm."
		if locale == "en" {
			spf = "Wear sunscreen daily on the marked areas."
		}
		if !containsFold(a.SoothingTips, "chống nắng") && !containsFold(a.SoothingTips, "sunscreen") {
			a.SoothingTips = append([]string{spf}, a.SoothingTips...)
			changed = true
		}
	}
	if laserQ {
		laserTip := "Muốn laser/trị thâm thì khám bác sĩ da tại chỗ — họ xem da thật rồi mới tư vấn số buổi và chi phí."
		if locale == "en" {
			laserTip = "For laser/pigment treatment, see a local dermatologist — session count and cost need an in-person look, not just a photo."
		}
		if !containsFold(a.SoothingTips, "laser") && !containsFold(a.SoothingTips, "bác sĩ da") && !containsFold(a.SoothingTips, "dermatologist") {
			a.SoothingTips = append(a.SoothingTips, laserTip)
			changed = true
		}
	}

	return finalizeAdminSkinAlign(a, pigmentPrimary, locale, changed)
}

// rewriteAwkwardShoppingTip turns terse “đừng chốt laser/giá từ ảnh” into plain buddy wording.
func rewriteAwkwardShoppingTip(tip string) string {
	t := strings.TrimSpace(tip)
	if t == "" {
		return tip
	}
	repls := []struct{ old, neu string }{
		{
			"Muốn trị thâm chuyên sâu thì khám bác sĩ da tại chỗ — đừng chốt laser/giá từ ảnh.",
			"Muốn trị thâm chuyên sâu thì khám bác sĩ da tại chỗ để họ xem da thật rồi tư vấn.",
		},
		{
			"đừng chốt laser/giá từ ảnh",
			"họ xem da thật rồi mới tư vấn",
		},
		{
			"đừng chốt số buổi hay giá từ ảnh",
			"họ xem da thật rồi mới tư vấn số buổi và chi phí",
		},
		{
			"đừng chốt số buổi hoặc giá từ ảnh",
			"họ xem da thật rồi mới tư vấn số buổi và chi phí",
		},
		{
			"don't lock sessions or prices from a photo",
			"session count and cost need an in-person look, not just a photo",
		},
	}
	out := tip
	for _, r := range repls {
		if strings.Contains(out, r.old) {
			out = strings.ReplaceAll(out, r.old, r.neu)
		}
	}
	return out
}

func finalizeAdminSkinAlign(
	a *dto.AdminSkinReviewAnalysis,
	pigmentPrimary bool,
	locale string,
	changed bool,
) bool {
	if a == nil {
		return changed
	}
	for i := range a.SoothingTips {
		if n := rewriteAwkwardShoppingTip(a.SoothingTips[i]); n != a.SoothingTips[i] {
			a.SoothingTips[i] = n
			changed = true
		}
	}
	// Keep tips 2–3, causes 1–2.
	if len(a.SoothingTips) > 3 {
		a.SoothingTips = a.SoothingTips[:3]
		changed = true
	}
	if len(a.PossibleCauses) > 2 {
		a.PossibleCauses = a.PossibleCauses[:2]
		changed = true
	}
	// Case-aware empty fallbacks — never re-inject "đừng nặn" into pigment-primary reviews.
	if len(a.SoothingTips) == 0 {
		if pigmentPrimary {
			if locale == "en" {
				a.SoothingTips = []string{"Wear sunscreen daily on the marked areas.", "Keep the routine gentle — don't scrub the spots."}
			} else {
				a.SoothingTips = []string{"Chống nắng đều mỗi ngày trên vùng thâm.", "Giữ routine dịu, đừng chà mạnh chỗ đốm."}
			}
		} else if locale == "en" {
			a.SoothingTips = []string{"Don't pick inflamed spots.", "Cleanse gently."}
		} else {
			a.SoothingTips = []string{"Đừng nặn ổ đang sưng đỏ.", "Rửa mặt dịu nhẹ."}
		}
		changed = true
	}
	if len(a.PossibleCauses) == 0 {
		if pigmentPrimary {
			if locale == "en" {
				a.PossibleCauses = []string{"Post-acne marks or local sun exposure."}
			} else {
				a.PossibleCauses = []string{"Do thâm sau mụn hoặc nắng cục bộ."}
			}
		} else if locale == "en" {
			a.PossibleCauses = []string{"Local clogging and irritation on the visible area."}
		} else {
			a.PossibleCauses = []string{"Do dầu bít tắc và kích ứng tại chỗ."}
		}
		changed = true
	}
	return changed
}

func containsFold(items []string, needle string) bool {
	n := strings.ToLower(needle)
	for _, it := range items {
		if strings.Contains(strings.ToLower(it), n) {
			return true
		}
	}
	return false
}

// trimOverviewCheekOverlap keeps thâm/bóng detail in the cheek note and strips
// overlapping detail sentences from overview (anti-repeat overview↔note).
func trimOverviewCheekOverlap(a *dto.AdminSkinReviewAnalysis) bool {
	if a == nil || strings.TrimSpace(a.Overview) == "" {
		return false
	}
	var cheekNote string
	for _, ar := range a.AttentionAreas {
		c := strings.ToLower(strings.TrimSpace(ar.Concern))
		r := strings.ToLower(strings.TrimSpace(ar.Region))
		if (r == "cheeks" || r == "cheek") && c != "" && c != "not_visible" && c != "none" {
			cheekNote = ar.Note
			break
		}
	}
	if strings.TrimSpace(cheekNote) == "" {
		return false
	}
	noteLow := strings.ToLower(cheekNote)
	parts := splitAdminSkinSentences(a.Overview)
	kept := make([]string, 0, len(parts))
	changed := false
	for _, p := range parts {
		pl := strings.ToLower(p)
		overlapTham := (strings.Contains(pl, "thâm") || strings.Contains(pl, "đốm nâu")) &&
			(strings.Contains(noteLow, "thâm") || strings.Contains(noteLow, "đốm nâu"))
		overlapBong := (strings.Contains(pl, "bóng") || strings.Contains(pl, "shine")) &&
			(strings.Contains(noteLow, "bóng") || strings.Contains(noteLow, "shine"))
		// Keep a short overview pointer; drop detail already expanded in the cheek note.
		if overlapTham || overlapBong {
			// Keep sentence only if it's the sole overview line (avoid emptying overview).
			if len(parts) == 1 {
				kept = append(kept, strings.TrimSpace(p))
				continue
			}
			changed = true
			continue
		}
		kept = append(kept, strings.TrimSpace(p))
	}
	if !changed || len(kept) == 0 {
		return false
	}
	a.Overview = strings.Join(kept, " ")
	return true
}

// trimRepeatedThamInAdditional drops additional sentences that restate thâm
// already covered in overview + cheek notes (close-up / pigment cases).
func trimRepeatedThamInAdditional(a *dto.AdminSkinReviewAnalysis) bool {
	if a == nil || strings.TrimSpace(a.AdditionalObservations) == "" {
		return false
	}
	prior := strings.ToLower(a.Overview)
	for _, ar := range a.AttentionAreas {
		if strings.EqualFold(strings.TrimSpace(ar.Region), "cheeks") {
			prior += " " + strings.ToLower(ar.Note)
		}
	}
	if !strings.Contains(prior, "thâm") && !strings.Contains(prior, "pigment") && !strings.Contains(prior, "đốm nâu") {
		return false
	}
	parts := splitAdminSkinSentences(a.AdditionalObservations)
	kept := make([]string, 0, len(parts))
	changed := false
	for _, p := range parts {
		pl := strings.ToLower(p)
		if (strings.Contains(pl, "thâm") || strings.Contains(pl, "đốm nâu") || strings.Contains(pl, "pigment")) &&
			!strings.Contains(pl, "ánh sáng") && !strings.Contains(pl, "crop") && !strings.Contains(pl, "khung") {
			changed = true
			continue
		}
		kept = append(kept, strings.TrimSpace(p))
	}
	if !changed {
		return false
	}
	if len(kept) == 0 {
		a.AdditionalObservations = "Chỉ xét được vùng thấy trên ảnh này. Crop hẹp nên đừng quy cả mặt."
		return true
	}
	a.AdditionalObservations = strings.Join(kept, " ")
	return true
}

func splitAdminSkinSentences(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i, r := range s {
		if r == '.' || r == '!' || r == '?' || r == '。' {
			piece := strings.TrimSpace(s[start : i+1])
			if piece != "" {
				out = append(out, piece)
			}
			start = i + 1
		}
	}
	if tail := strings.TrimSpace(s[start:]); tail != "" {
		out = append(out, tail)
	}
	return out
}
