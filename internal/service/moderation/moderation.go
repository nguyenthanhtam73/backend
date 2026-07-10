// Package moderation runs OpenAI safety checks before skin check-ins are persisted.
package moderation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/platform/imgprep"
)

// Service calls OpenAI moderation endpoints.
type Service struct {
	httpClient *http.Client
	cfg        *config.Config
}

// New constructs a moderation Service from application config.
func New(cfg *config.Config) *Service {
	return &Service{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

// CheckSkinContent runs omni moderation on free-form text and each image's raw bytes.
//
// OpenAI omni-moderation accepts at most one image per request, so we send text
// alone (when present) and then one request per photo instead of batching images.
// Images are passed as in-memory bytes (already read during upload) so moderation
// does not depend on where the file is ultimately stored (disk vs R2).
func (s *Service) CheckSkinContent(ctx context.Context, text string, images [][]byte) error {
	if s == nil || s.cfg == nil {
		return fmt.Errorf("moderation: not configured")
	}
	if s.cfg.Moderation.Skip {
		return nil
	}
	if strings.TrimSpace(s.cfg.OpenAI.APIKey) == "" {
		return fmt.Errorf("moderation requires DADIARY_OPENAI_API_KEY or set DADIARY_MODERATION_SKIP=true for local dev")
	}

	combined := strings.TrimSpace(text)
	if combined == "" && len(images) == 0 {
		return fmt.Errorf("nothing to moderate")
	}

	if combined != "" {
		if err := s.moderateInput(ctx, []map[string]any{
			{"type": "text", "text": combined},
		}); err != nil {
			return err
		}
	}

	for _, data := range images {
		part, err := imageModerationPart(data)
		if err != nil {
			return err
		}
		if err := s.moderateInput(ctx, []map[string]any{part}); err != nil {
			return err
		}
	}
	return nil
}

func imageModerationPart(data []byte) (map[string]any, error) {
	data, err := imgprep.LimitForVisionAPI(data)
	if err != nil {
		return nil, fmt.Errorf("prepare image for moderation: %w", err)
	}
	mime := "image/jpeg"
	b64 := base64.StdEncoding.EncodeToString(data)
	url := fmt.Sprintf("data:%s;base64,%s", mime, b64)
	return map[string]any{
		"type": "image_url",
		"image_url": map[string]any{
			"url": url,
		},
	}, nil
}

func (s *Service) moderateInput(ctx context.Context, parts []map[string]any) error {
	if len(parts) == 0 {
		return fmt.Errorf("nothing to moderate")
	}

	body := map[string]any{
		"model": "omni-moderation-latest",
		"input": parts,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/moderations", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.OpenAI.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("openai moderations request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("openai moderations http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed struct {
		Results []struct {
			Flagged bool `json:"flagged"`
		} `json:"results"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return fmt.Errorf("decode moderations response: %w", err)
	}
	for _, r := range parsed.Results {
		if r.Flagged {
			return fmt.Errorf("content failed moderation checks")
		}
	}
	return nil
}
