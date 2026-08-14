package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dadiary/backend/internal/config"
)

// share_title_cleanup.go — rewrite a raw Facebook question into a clean page title.
//
// The derived titles are the asker's own words, which is right for search intent but ships
// chat shorthand ("kb có hqua thật k", "phác đồ đtri") into <title> and Facebook previews.
// Nobody searches those abbreviations and it reads careless. This keeps the question and
// its intent, but spells it properly.

const shareTitleSystemPromptVI = `Bạn viết tiêu đề trang cho DaDiary từ câu hỏi thật của người dùng trên Facebook.

## Nhiệm vụ
Viết lại câu hỏi thành **một tiêu đề sạch, đúng chính tả**, giữ nguyên ý người hỏi.

## BẮT BUỘC
- Giữ đúng vấn đề họ hỏi (vùng da + dấu hiệu + điều họ muốn biết).
- **Viết đủ chữ**: kb → không biết, hqua → hiệu quả, đtri → điều trị, tv → tư vấn, mn → mọi người, e/em giữ nguyên, ntn → như thế nào, k → không, trc → trước, dc/đc → được.
- Bỏ lời chào, cảm ơn, xin giúp, emoji, dấu ba chấm.
- Tối đa **70 ký tự**. Ngắn hơn thì tốt.
- Là câu hỏi thì kết bằng dấu "?"; là câu kể thì không thêm dấu hỏi.
- Viết như cách người Việt **gõ vào Google** để tìm đúng vấn đề đó.

## CẤM
- CẤM thêm tên thương hiệu, "DaDiary", tên sản phẩm.
- CẤM chẩn đoán hay hứa hẹn ("chữa khỏi", "hết mụn sau 7 ngày").
- CẤM bịa thêm chi tiết không có trong câu hỏi.
- CẤM viết hoa toàn bộ, CẤM để trong ngoặc kép.

## Ví dụ
- "Mn tv giúp e da nám như này thì phác đồ đtri như nào?" → "Da nám thì phác đồ điều trị như nào?"
- "Thấy mn cứ làm liệu trình tái tạo da nhìn ưng quá, kb có hqua thật k" → "Liệu trình tái tạo da có hiệu quả thật không?"
- "Mặt em bị vậy là mụn gì vậy ạ với cho em xin tip giảm mụn chớ em tự ti quá" → "Mặt bị mụn gì và cách giảm mụn?"

## Output
Chỉ trả về đúng 1 JSON object: {"title": "..."}`

const shareTitleSystemPromptEN = `You rewrite a real user question into a clean page title for DaDiary.

Keep their problem and intent. Expand chat shorthand, drop greetings/thanks/emoji, max 70
characters, end with "?" only if it is a question, no brand names, no diagnosis or promises,
no invented details, no quotes. Write it the way someone would type it into Google.

Return exactly one JSON object: {"title": "..."}`

// CleanShareTitle asks the text model for a tidy title, falling back to the deterministic
// TitleFromUserQuestion when the model is unavailable or returns something unusable.
func CleanShareTitle(
	ctx context.Context,
	cfg *config.Config,
	httpClient *http.Client,
	userQuestion, locale string,
) (string, bool, error) {
	fallback := TitleFromUserQuestion(userQuestion, locale)
	q := strings.Join(strings.Fields(userQuestion), " ")
	if q == "" {
		return "", false, nil
	}

	system := shareTitleSystemPromptVI
	if strings.EqualFold(strings.TrimSpace(locale), "en") {
		system = shareTitleSystemPromptEN
	}
	userMsg := "Câu hỏi gốc của người dùng:\n" + q + "\n\nViết tiêu đề sạch theo đúng quy tắc. Chỉ trả về JSON."

	res, err := TextCoachCompletion(ctx, cfg, httpClient, "share-title", system, userMsg)
	if err != nil {
		return fallback, false, err
	}
	raw, err := ExtractJSONObject(res.Text)
	if err != nil {
		return fallback, false, err
	}
	var out struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fallback, false, err
	}

	title, ok := sanitizeShareTitle(out.Title)
	if !ok {
		return fallback, false, fmt.Errorf("model title rejected: %q", out.Title)
	}
	return title, true, nil
}

// sanitizeShareTitle enforces the rules the prompt asks for, since a prompt is a request
// and not a guarantee.
func sanitizeShareTitle(s string) (string, bool) {
	s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), `"'“”`))
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return "", false
	}
	low := strings.ToLower(s)
	for _, bad := range []string{"dadiary", "http", "hết mụn sau", "chữa khỏi", "cam kết"} {
		if strings.Contains(low, bad) {
			return "", false
		}
	}
	if n := len([]rune(s)); n < 10 {
		return "", false
	}
	s = clipRunesAtWord(s, maxShareTitleRunes)
	return upperFirst(s), true
}
