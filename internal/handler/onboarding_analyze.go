package handler

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// OnboardingAnalyzeHandler runs AI vision for onboarding skin typing (multipart images).
type OnboardingAnalyzeHandler struct {
	cfg        *config.Config
	httpClient *http.Client
}

// NewOnboardingAnalyzeHandler constructs the handler.
func NewOnboardingAnalyzeHandler(cfg *config.Config) *OnboardingAnalyzeHandler {
	return &OnboardingAnalyzeHandler{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 6 * time.Minute,
		},
	}
}

// AnalyzeSkin handles POST /api/v1/onboarding/analyze-skin (2–3 images, multipart field "images").
func (h *OnboardingAnalyzeHandler) AnalyzeSkin(c *fiber.Ctx) error {
	if h == nil || h.cfg == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "configuration missing")
	}
	userID := middleware.UserIDFromLocals(c)
	if userID == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}

	form, err := c.MultipartForm()
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_multipart", "expected multipart form data")
	}
	defer func() { _ = form.RemoveAll() }()

	files := form.File["images"]
	if len(files) < 2 {
		return response.Error(c, fiber.StatusBadRequest, "too_few_images", "upload at least 2 face photos")
	}
	if len(files) > 3 {
		return response.Error(c, fiber.StatusBadRequest, "too_many_images", "maximum 3 photos for onboarding analysis")
	}

	maxBytes := int64(h.cfg.Upload.MaxMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}

	imgs := make([]ai.ImageBytes, 0, len(files))
	for _, fh := range files {
		if fh.Size > maxBytes {
			return response.Error(c, fiber.StatusRequestEntityTooLarge, "file_too_large", fmt.Sprintf("each image must be <= %d MB", h.cfg.Upload.MaxMB))
		}
		ext, ok := extFromFile(fh)
		if !ok {
			return response.Error(c, fiber.StatusBadRequest, "invalid_image", "only jpeg, png, webp, gif are allowed")
		}
		_ = ext
		data, rerr := readAllFromMultipartHeader(fh)
		if rerr != nil {
			return response.Error(c, fiber.StatusBadRequest, "read_failed", "could not read uploaded image")
		}
		imgs = append(imgs, ai.ImageBytes{Data: data})
	}

	out, err := ai.OnboardingSkinAnalyze(c.UserContext(), h.cfg, h.httpClient, imgs, firstFormLocale(form))
	if err != nil {
		// Surface model/parse issues without leaking stack traces.
		msg := err.Error()
		if strings.Contains(strings.ToLower(msg), "api key") {
			return response.Error(c, fiber.StatusServiceUnavailable, "openai_not_configured", "OpenAI API key required for photo analysis")
		}
		return response.Error(c, fiber.StatusUnprocessableEntity, "analysis_failed", msg)
	}
	return response.JSON(c, fiber.StatusOK, out)
}

func readAllFromMultipartHeader(fh *multipart.FileHeader) ([]byte, error) {
	src, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()
	return io.ReadAll(src)
}

func firstFormLocale(form *multipart.Form) string {
	if form == nil {
		return ""
	}
	vals := form.Value["locale"]
	if len(vals) == 0 {
		return ""
	}
	return strings.TrimSpace(vals[0])
}
