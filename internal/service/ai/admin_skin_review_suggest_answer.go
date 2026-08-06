package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/platform/openai"
)

// AdminSkinReviewSuggestAnswer drafts a short public reply (2–4 sentences)
// from the FB/group user question + saved analysis. Admin edits before save.
func AdminSkinReviewSuggestAnswer(
	ctx context.Context,
	cfg *config.Config,
	httpClient *http.Client,
	userQuestion string,
	analysis *dto.AdminSkinReviewAnalysis,
	localeRaw string,
) (string, error) {
	if cfg == nil || strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		return "", fmt.Errorf("admin skin review suggest answer: openai api key required")
	}
	question := strings.TrimSpace(userQuestion)
	if question == "" {
		return "", fmt.Errorf("admin skin review suggest answer: user_question required")
	}
	locale := dto.NormalizeAdminSkinReviewLocale(localeRaw)
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 90 * time.Second}
	}

	system := adminSkinReviewSuggestAnswerSystemPrompt(locale)
	user := adminSkinReviewSuggestAnswerUserMessage(question, analysis, locale)

	raw, err := openai.ChatCompletionJSON(ctx, cfg, httpClient, system, user)
	if err != nil {
		return "", err
	}
	answer, err := parseAdminSkinReviewSuggestedAnswer(raw)
	if err != nil {
		return "", err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", fmt.Errorf("admin skin review suggest answer: empty draft")
	}
	// Soft cap — keep FB comments scannable.
	if utf8.RuneCountInString(answer) > 1200 {
		runes := []rune(answer)
		answer = strings.TrimSpace(string(runes[:1200]))
	}
	return answer, nil
}

func adminSkinReviewSuggestAnswerSystemPrompt(locale string) string {
	if locale == "en" {
		return `You draft a short public reply for DaDiary Admin Skin Review share posts.

Voice (required): close friend, blunt, warm — address the reader as "you"; writer as "I". Confident about what the photo shows. No fluff, no medical diagnosis, no long morning/evening routine, no shopping list.

Priority (critical):
1) The USER QUESTION is the source of truth for context they already gave (products, habits, oily skin, why shiny, laser interest, city, etc.).
2) Use analysis only for what the PHOTO shows (spots, redness, pigment, density). Prefer "cheek near the ear" if analysis left/right looks shaky.
3) NEVER contradict what the user already said. If they say shine is from spot treatment / cream just applied, acknowledge that — do NOT blame oil clogging for the shine, and do NOT tell them to pause that product unless they ask about irritation from it.
4) Only add a tip if it directly helps their question. Prefer no tip over a mismatched tip. Do NOT invent "pause strong products" unless they ask about irritation from a product.
5) If they ask "which step am I doing wrong" but never listed their routine: say the photo shows the RESULT + use their oily-skin claim — do NOT invent a wrong step / product.
6) Laser / clinic questions: say marks look like post-acne/sun pigment if photo supports it; guide them to see a **local dermatologist where they live** for laser suitability. BAN naming specific hospitals/clinics/spas as "good/best". BAN locking session counts, packages, or prices.

Rules:
- Reply in English.
- 2–4 short sentences total.
- FIRST job: answer their actual question / concern, then briefly name what the photo shows if useful.
- If they named a product/ingredient, you MAY mention it once as context. Do NOT recommend new brands or prescription meds.
- Prefer: "Your cheek…", "This looks like…". Plain words only — BAN jargon: active, actives, BHA, AHA, retinoid (unless the user already said that exact name).
- Soften only if photo_notes say the crop/light is bad.
- Never invent concerns not supported by the analysis.
- Output JSON only: {"answer":"..."}`
	}
	return `Bạn soạn câu trả lời ngắn cho bài chia sẻ Admin Skin Review DaDiary (comment FB/group).

Giọng (BẮT BUỘC): bạn thân đanh đá, ấm — xưng **tao** (người trả lời) / **mày** (user hỏi). Nói thẳng. Không nịnh, không vòng vo, không brochure.

Ưu tiên (BẮT BUỘC — đọc kỹ):
1) **Câu hỏi của user là nguồn sự thật** về context họ đã nói (đang bôi chấm mụn, da nhiều dầu, hỏi laser HCM…).
2) Analysis chỉ mô tả **những gì thấy trên ảnh**. Nếu analysis ghi sai bên má / không chắc → viết **“má gần tai”** / **“má của mày”**.
3) **CẤM phủ nhận / đè context của user.** Ví dụ: bóng vì chấm mụn → thừa nhận lớp kem; **CẤM** “bóng do dầu”; **CẤM** “tạm nghỉ sản phẩm” khi họ đang giải thích việc đang bôi (trừ hỏi kích ứng/rát).
4) Tip chỉ khi khớp câu hỏi. **CẤM** tự bịa “tạm nghỉ sản phẩm trị mụn mạnh” trừ khi hỏi kích ứng.
5) Hỏi **“sai bước nào”** chưa kể routine → nói kết quả trên ảnh + claim da dầu; **CẤM bịa** bước sai.
6) Hỏi **laser / phòng khám / bệnh viện** (vd. HCM): nói ảnh thấy thâm/sắc tố nếu đúng; bảo **khám bác sĩ da tại chỗ** (cùng thành phố họ nêu) để tư vấn laser có hợp không. **CẤM** khen/recommend tên BV/PK/spa cụ thể là “tốt”. **CẤM** chốt số buổi, gói, giá laser.

Rules:
- Trả lời tiếng Việt đời thường — user FB không biết jargon skincare.
- Tổng 2–4 câu ngắn.
- Việc ĐẦU TIÊN: trả lời đúng điều user hỏi, rồi mới nối ngắn với ảnh nếu cần.
- Xưng vùng: **“má của mày” / “má gần tai” / “trán của mày”** (không “Má mày” cụt).
- Nếu user nêu tên sản phẩm/hoạt chất → được nhắc 1 lần. CẤM brand mới / thuốc kê đơn / routine sáng–tối dài.
- Ưu tiên: “Đây là…”, “Má gần tai của mày đang…”. CẤM hedge: “không chắc 100%”, “có thể do…”, “có thể là…”, “chưa chắc”.
- **CẤM từ jargon**: active, actives, BHA, AHA, retinoid, T-zone (trừ khi user đã tự nói).
- Chỉ mềm khi photo_notes nói ảnh mờ / crop kém.
- Không bịa dấu hiệu ngoài analysis.
- Chỉ JSON: {"answer":"..."}`
}

func adminSkinReviewSuggestAnswerUserMessage(
	question string,
	analysis *dto.AdminSkinReviewAnalysis,
	locale string,
) string {
	var b strings.Builder
	if locale == "en" {
		b.WriteString("USER QUESTION (highest priority — do not contradict their context):\n")
	} else {
		b.WriteString("CÂU HỎI CỦA USER (ưu tiên cao nhất — đừng phủ nhận context họ đã nói):\n")
	}
	b.WriteString(question)
	b.WriteString("\n\n")
	if locale == "en" {
		b.WriteString("Analysis JSON (photo facts only — support the answer; never override the user's stated reason):\n")
	} else {
		b.WriteString("Analysis JSON (chỉ fact từ ảnh — hỗ trợ trả lời; không đè lý do user đã nói):\n")
	}
	// Photo facts only. Omit possible_causes / soothing_tips — those generic lines
	// often override the user's stated context (e.g. shine from spot cream → "oil clog").
	// Soften má trái/phải only on close-up cheek crops (full-face can keep a correct side).
	payload := map[string]any{}
	if analysis != nil {
		soften := adminSkinLooksCloseUpCheek(analysis)
		note := func(s string) string {
			if soften {
				return SoftenCheekLateralityProse(s)
			}
			return s
		}
		areas := make([]map[string]any, 0, len(analysis.AttentionAreas))
		for _, a := range analysis.AttentionAreas {
			areas = append(areas, map[string]any{
				"region":    a.Region,
				"concern":   a.Concern,
				"severity": a.Severity,
				"note":      note(a.Note),
			})
		}
		payload = map[string]any{
			"overview":                note(analysis.Overview),
			"skin_type":               analysis.SkinType,
			"skin_type_severity":      analysis.SkinTypeSeverity,
			"skin_type_note":          note(analysis.SkinTypeNote),
			"attention_areas":         areas,
			"additional_observations": note(analysis.AdditionalObservations),
			"photo_notes":             note(analysis.PhotoNotes),
		}
	}
	raw, _ := json.Marshal(payload)
	b.Write(raw)
	if locale == "en" {
		b.WriteString(`

Example: if user says shine is from spot treatment cream, answer that the shine looks like product film — do NOT blame oil, do NOT tell them to pause that product.
Return {"answer":"..."} only.`)
	} else {
		b.WriteString(`

Ví dụ 1: user nói đang bôi chấm mụn nên bóng → chỗ bóng đúng kiểu lớp kem; CẤM bóng do dầu; CẤM bảo nghỉ sản phẩm đang bôi.
Ví dụ 2: user hỏi “sai bước nào” + “da nhiều dầu” mà chưa kể routine → nhận da dầu + mô tả cụm viêm trên ảnh; CẤM bịa sai bước; bảo kể đang dùng gì / đừng nặn đầu trắng.
Ví dụ 3: user hỏi laser trị thâm ở HCM → nhận thâm trên ảnh nếu có; bảo khám BS da tại HCM/tại chỗ; CẤM gọi tên BV/PK; CẤM số buổi/giá.
Chỉ trả {"answer":"..."}.`)
	}
	return b.String()
}

func parseAdminSkinReviewSuggestedAnswer(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty response")
	}
	var wrapped struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil {
		if s := strings.TrimSpace(wrapped.Answer); s != "" {
			return s, nil
		}
	}
	// Tolerate fenced JSON or bare text fallback.
	if i := strings.Index(raw, "{"); i >= 0 {
		if j := strings.LastIndex(raw, "}"); j > i {
			if err := json.Unmarshal([]byte(raw[i:j+1]), &wrapped); err == nil {
				if s := strings.TrimSpace(wrapped.Answer); s != "" {
					return s, nil
				}
			}
		}
	}
	// Last resort: treat whole content as the answer if it looks like prose.
	if !strings.HasPrefix(raw, "{") && utf8.RuneCountInString(raw) > 20 {
		return raw, nil
	}
	return "", fmt.Errorf("could not parse suggested answer JSON")
}
