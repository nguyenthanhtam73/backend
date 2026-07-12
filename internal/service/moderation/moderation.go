// Package moderation runs OpenAI safety checks before skin check-ins are persisted.
package moderation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/platform/httpx"
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
//
// The individual moderation calls (text + one per photo) are independent, so they
// run concurrently: this call blocks the POST /skin-checks response before the
// client can even show progress, so wall time is the slowest single request
// instead of the sum of all of them. The first flagged/errored input cancels the
// rest via ctx so we don't keep paying for calls whose result we'll discard.
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

	// Build one moderation part per independent input (text + each photo). Image
	// prep (decode/resize) is CPU-bound and cheap relative to the network calls,
	// so keep it inline here and let the round-trips fan out below.
	parts := make([][]map[string]any, 0, len(images)+1)
	if combined != "" {
		parts = append(parts, []map[string]any{{"type": "text", "text": combined}})
	}
	for _, data := range images {
		part, err := imageModerationPart(data)
		if err != nil {
			return err
		}
		parts = append(parts, []map[string]any{part})
	}

	if len(parts) == 1 {
		return s.moderateInput(ctx, parts[0])
	}

	// Cancel siblings as soon as one call fails (flagged content or transport
	// error) so we stop hitting the API for a result we're going to throw away.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg      sync.WaitGroup
		once    sync.Once
		firstErr error
	)
	for _, part := range parts {
		wg.Add(1)
		go func(p []map[string]any) {
			defer wg.Done()
			if err := s.moderateInput(runCtx, p); err != nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
			}
		}(part)
	}
	wg.Wait()
	return firstErr
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

	headers := map[string]string{
		"Authorization": "Bearer " + s.cfg.OpenAI.APIKey,
		"Content-Type":  "application/json",
	}
	// Retry transient failures (network, 429, 5xx). The flagged-content decision
	// below runs only after a successful 2xx, so it is never retried.
	respBody, err := httpx.WithRetry(ctx, s.cfg.AI.Retry, "openai-moderations", func(ctx context.Context) ([]byte, error) {
		return httpx.PostJSON(ctx, s.httpClient, "openai moderations", "https://api.openai.com/v1/moderations", headers, payload)
	})
	if err != nil {
		return err
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
