package ai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/platform/imgprep"
)

func onboardingOutputLocale(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "en":
		return "en"
	default:
		return "vi"
	}
}

// ImageBytes is one decoded onboarding photo (JPEG/PNG/WebP/GIF).
type ImageBytes struct {
	Data []byte
}

// OnboardingSkinAnalyze runs hybrid onboarding skin analysis:
// pass 1 — GPT-4o vision (structured guesses + visual_observations);
// pass 2 — Claude/text coach (coaching_notes from vision JSON).
func OnboardingSkinAnalyze(ctx context.Context, cfg *config.Config, httpClient *http.Client, images []ImageBytes, localeRaw string) (*dto.OnboardingSkinAnalyzeResponse, error) {
	if cfg == nil || strings.TrimSpace(cfg.OpenAI.APIKey) == "" {
		return nil, fmt.Errorf("onboarding skin: openai api key required")
	}
	locale := onboardingOutputLocale(localeRaw)
	if len(images) < 1 || len(images) > 3 {
		return nil, fmt.Errorf("onboarding skin: need 1 to 3 images")
	}
	prepared := make([]ImageBytes, len(images))
	for i, im := range images {
		data, err := imgprep.LimitForVisionAPI(im.Data)
		if err != nil {
			return nil, fmt.Errorf("onboarding skin: prepare image %d: %w", i+1, err)
		}
		prepared[i] = ImageBytes{Data: data}
	}
	images = prepared
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Minute}
	}
	model := strings.TrimSpace(cfg.OpenAI.VisionModel)
	if model == "" {
		model = strings.TrimSpace(cfg.OpenAI.Model)
	}
	if model == "" {
		model = "gpt-4o"
	}

	parsed, err := onboardingVisionPass(ctx, cfg, httpClient, images, locale, model)
	if err != nil {
		return nil, err
	}
	parsed.NonDiagnostic = normalizeOnboardingDisclaimer(parsed.NonDiagnostic, locale)

	coachNotes, coachMeta, coachErr := onboardingCoachPass(ctx, cfg, httpClient, parsed, locale)
	if coachErr != nil {
		slog.Warn("onboarding skin: coach pass failed, using vision fallback", "err", coachErr)
		coachNotes = fallbackOnboardingCoachingNotes(parsed, locale)
		coachMeta = "coach=error"
	}
	parsed.CoachingNotes = strings.TrimSpace(coachNotes)
	parsed.ModelUsed = fmt.Sprintf("vision=%s|coach=%s", model, coachMeta)
	return parsed, nil
}

func onboardingVisionPass(
	ctx context.Context,
	cfg *config.Config,
	httpClient *http.Client,
	images []ImageBytes,
	locale, model string,
) (*dto.OnboardingSkinAnalyzeResponse, error) {
	langHead := "**Output locale: Vietnamese (vi).** Write detailed_observations and main_concerns only in natural Vietnamese."
	if locale == "en" {
		langHead = "**Output locale: English (en).** Write detailed_observations and main_concerns only in natural English."
	}
	userText := langHead + "\n\n" + OnboardingSkinJSONSchemaBlock + "\n\nPhotos: **2–3 close, well-lit photos of facial skin** (natural light, little or no makeup). Include a **front** view plus a **slight side/profile** if possible—this flow is optimized for face-only onboarding."
	parts := []map[string]any{
		{"type": "text", "text": userText},
	}
	for _, im := range images {
		if len(im.Data) == 0 {
			continue
		}
		head := im.Data
		if len(head) > 512 {
			head = head[:512]
		}
		mime := http.DetectContentType(head)
		if !strings.HasPrefix(mime, "image/") {
			return nil, fmt.Errorf("onboarding skin: invalid image bytes")
		}
		b64 := fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(im.Data))
		parts = append(parts, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": b64,
			},
		})
	}
	if len(parts) < 3 {
		return nil, fmt.Errorf("onboarding skin: not enough valid images")
	}

	body := map[string]any{
		"model":           model,
		"temperature":     0.2,
		"response_format": map[string]any{"type": "json_object"},
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": OnboardingSkinVisionPrompt(),
			},
			{
				"role":    "user",
				"content": parts,
			},
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.OpenAI.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai onboarding vision http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var apiOut struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(b, &apiOut); err != nil {
		return nil, err
	}
	if len(apiOut.Choices) == 0 || strings.TrimSpace(apiOut.Choices[0].Message.Content) == "" {
		return nil, fmt.Errorf("openai onboarding: empty response")
	}
	raw, err := ExtractJSONObject(apiOut.Choices[0].Message.Content)
	if err != nil {
		return nil, err
	}
	var visionRaw onboardingVisionRaw
	if err := json.Unmarshal(raw, &visionRaw); err != nil {
		return nil, fmt.Errorf("parse onboarding vision json: %w", err)
	}
	parsed := mapOnboardingVisionRaw(visionRaw, locale)
	parsed.CoachingNotes = ""
	return &parsed, nil
}

func onboardingCoachPass(
	ctx context.Context,
	cfg *config.Config,
	httpClient *http.Client,
	vision *dto.OnboardingSkinAnalyzeResponse,
	locale string,
) (notes string, meta string, err error) {
	if vision == nil {
		return "", "", fmt.Errorf("onboarding coach: nil vision")
	}
	if !cfg.HasAnthropicKey() && !cfg.HasOpenAIKey() {
		return "", "", fmt.Errorf("onboarding coach: no text model configured")
	}
	visionJSON, err := json.Marshal(vision)
	if err != nil {
		return "", "", err
	}
	userMsg := BuildOnboardingCoachUserMessage(visionJSON, locale)
	result, err := TextCoachCompletion(ctx, cfg, httpClient, "onboarding-skin", OnboardingCoachSystemPrompt(), userMsg)
	if err != nil {
		return "", "", err
	}
	raw, err := ExtractJSONObject(result.Text)
	if err != nil {
		return "", "", err
	}
	var out struct {
		CoachingNotes string `json:"coaching_notes"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("parse onboarding coach json: %w", err)
	}
	if strings.TrimSpace(out.CoachingNotes) == "" {
		return "", "", fmt.Errorf("onboarding coach: empty coaching_notes")
	}
	meta = fmt.Sprintf("%s(%s", result.Model, result.Provider)
	if result.Fallback {
		meta += ",fallback"
	}
	meta += ")"
	return out.CoachingNotes, meta, nil
}

func fallbackOnboardingCoachingNotes(vision *dto.OnboardingSkinAnalyzeResponse, locale string) string {
	if vision == nil {
		return ""
	}
	obs := strings.TrimSpace(vision.DetailedObservations)
	if obs == "" && len(vision.VisualObservations) > 0 {
		obs = strings.Join(vision.VisualObservations, ". ")
	}
	primaryConcern := ""
	if len(vision.MainConcerns) > 0 {
		primaryConcern = vision.MainConcerns[0]
	} else if len(vision.Concerns) > 0 {
		primaryConcern = vision.Concerns[0]
	}
	if strings.EqualFold(locale, "en") {
		p1 := obs
		if p1 == "" {
			p1 = "The photos were hard to read in detail — lighting or angle may be limiting."
		} else if !strings.HasPrefix(strings.ToLower(p1), "on the photo") {
			p1 = "On the photos I can see: " + p1
		}
		skin := friendlySkinType(vision.SkinTypeGuess, locale)
		tone := friendlyUndertone(vision.UndertoneGuess, locale)
		concern := friendlyConcern(primaryConcern, locale)
		p2 := fmt.Sprintf("Overall your skin looks like %s with a %s — the main thing to focus on is %s.",
			skin, tone, concern)
		p3 := "Soft read from photos only — tweak anything that doesn't match how your skin feels."
		p4 := fmt.Sprintf("Start simple: gentle cleanser + moisturizer + morning sunscreen; focus on %s first.", concern)
		return strings.Join([]string{p1, p2, p3, p4}, "\n\n")
	}
	p1 := obs
	if p1 == "" {
		p1 = "Ảnh hơi khó nhìn chi tiết — có thể do ánh sáng hoặc góc chụp."
	} else if !strings.HasPrefix(strings.ToLower(p1), "trên ảnh") {
		p1 = "Trên ảnh mình thấy: " + p1
	}
	skin := friendlySkinType(vision.SkinTypeGuess, locale)
	tone := friendlyUndertone(vision.UndertoneGuess, locale)
	concern := friendlyConcern(primaryConcern, locale)
	p2 := fmt.Sprintf("Tóm lại da bạn có vẻ %s, %s — vấn đề chính là %s.", skin, tone, concern)
	p3 := "Đây chỉ là đọc nhẹ từ ảnh thôi — chỉnh lại nếu không khớp cảm nhận của bạn nhé."
	p4 := fmt.Sprintf("Bắt đầu đơn giản: rửa dịu + dưỡng ẩm + kem chống nắng buổi sáng; ưu tiên xử lý %s trước.", concern)
	return strings.Join([]string{p1, p2, p3, p4}, "\n\n")
}
