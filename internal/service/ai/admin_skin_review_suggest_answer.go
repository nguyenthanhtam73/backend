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

Rules:
- Reply in English.
- 2–4 short sentences total.
- FIRST job: answer the user's question directly (what it looks like / why itchy-red / what to do next), using analysis facts.
- If the user named a product/ingredient they already used (e.g. azelaic acid), you MAY refer to that name once as context — e.g. pause that strong treatment product if the photo looks irritated — do NOT recommend new brands or prescription meds.
- Prefer: "Your cheeks…", "This looks like…". Plain words only — BAN jargon: active, actives, BHA, AHA, retinoid (unless the user already said that exact name).
- Soften only if photo_notes say the crop/light is bad.
- Gentle tips only if already in soothing_tips (no picking, gentle cleanse, pause strong treatment products).
- Never invent concerns not supported by the analysis.
- Output JSON only: {"answer":"..."}`
	}
	return `Bạn soạn câu trả lời ngắn cho bài chia sẻ Admin Skin Review DaDiary (comment FB/group).

Giọng (BẮT BUỘC): bạn thân đanh đá, ấm — xưng **tao** (người trả lời) / **mày** (user hỏi). Nói thẳng từ ảnh/analysis. Không nịnh, không vòng vo, không brochure.

Rules:
- Trả lời tiếng Việt đời thường — user FB không biết jargon skincare.
- Tổng 2–4 câu ngắn.
- Việc ĐẦU TIÊN: trả lời đúng câu hỏi user (bị gì / sao ngứa đỏ / liên quan gì tới thứ mày vừa dùng), bám analysis.
- Xưng vùng: **“má của mày” / “trán của mày” / “cằm của mày”** (không viết “Má mày” cụt).
- Nếu user đã nêu tên sản phẩm/hoạt chất họ dùng (vd. azelaic acid / azelaic 20%), ĐƯỢC nhắc lại 1 lần — kiểu tạm nghỉ sản phẩm trị mụn/mạnh đó nếu da đang đỏ ngứa. CẤM giới thiệu brand mới / thuốc kê đơn / routine sáng–tối dài.
- Ưu tiên: “Đây là…”, “Má của mày đang…”, “Trông đúng kiểu…”. CẤM nhồi hedge: “không chắc 100%”, “có thể là…”, “chưa chắc”.
- **CẤM từ jargon**: active, actives, BHA, AHA, retinoid, T-zone (trừ khi user đã tự nói đúng tên đó). Viết đời thường: “sản phẩm trị mụn mạnh”, “kem đang làm da kích ứng”, “tạm nghỉ sản phẩm mạnh đang dùng”.
- Chỉ mềm khi photo_notes nói ảnh mờ / crop kém.
- Tip nhẹ chỉ lấy từ soothing_tips nếu có — viết lại dễ hiểu (không nặn, rửa dịu, tạm nghỉ sản phẩm mạnh, chống nắng).
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
		b.WriteString("User question:\n")
	} else {
		b.WriteString("Câu hỏi của user:\n")
	}
	b.WriteString(question)
	b.WriteString("\n\n")
	if locale == "en" {
		b.WriteString("Analysis JSON (facts only — ground your reply):\n")
	} else {
		b.WriteString("Analysis JSON (chỉ bám fact này để trả lời):\n")
	}
	payload := map[string]any{}
	if analysis != nil {
		payload = map[string]any{
			"overview":                analysis.Overview,
			"skin_type":               analysis.SkinType,
			"skin_type_severity":      analysis.SkinTypeSeverity,
			"skin_type_note":          analysis.SkinTypeNote,
			"attention_areas":         analysis.AttentionAreas,
			"additional_observations": analysis.AdditionalObservations,
			"photo_notes":             analysis.PhotoNotes,
			"possible_causes":         analysis.PossibleCauses,
			"soothing_tips":           analysis.SoothingTips,
			"non_diagnostic":          analysis.NonDiagnostic,
		}
	}
	raw, _ := json.Marshal(payload)
	b.Write(raw)
	b.WriteString(`

Return {"answer":"..."} only.`)
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
