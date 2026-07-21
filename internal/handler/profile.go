package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"strings"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/storage"
	premiumuc "github.com/dadiary/backend/internal/usecase/premium"
	profileuc "github.com/dadiary/backend/internal/usecase/profile"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const maxOnboardingPhotos = 3

// ProfileHandler serves skin profile and onboarding endpoints.
type ProfileHandler struct {
	svc     *profileuc.Service
	cfg     *config.Config
	store   storage.Storage
	premium *premiumuc.Service
}

// NewProfileHandler constructs ProfileHandler. premium may be nil (no_ads strip skipped).
func NewProfileHandler(
	svc *profileuc.Service,
	cfg *config.Config,
	store storage.Storage,
	premium *premiumuc.Service,
) *ProfileHandler {
	return &ProfileHandler{svc: svc, cfg: cfg, store: store, premium: premium}
}

// GetSkin handles GET /profile/skin.
func (h *ProfileHandler) GetSkin(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "profile service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	res, err := h.svc.GetSkin(c.UserContext(), uid)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "profile_error", err.Error())
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// PutSkin handles PUT /profile/skin (manual edits, no AI).
func (h *ProfileHandler) PutSkin(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "profile service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body dto.PutSkinProfileRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	res, err := h.svc.PutSkin(c.UserContext(), uid, body)
	if err != nil {
		return mapProfileError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// CompleteOnboarding handles POST /profile/onboarding/complete.
// Accepts JSON or multipart (field `payload` JSON + optional `images` files).
func (h *ProfileHandler) CompleteOnboarding(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "profile service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}

	var body dto.OnboardingCompleteRequest
	var photoRels []string

	ct := string(c.Request().Header.ContentType())
	if strings.HasPrefix(ct, "multipart/form-data") {
		form, err := c.MultipartForm()
		if err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_multipart", "expected multipart form data")
		}
		defer func() { _ = form.RemoveAll() }()

		payload := form.Value["payload"]
		if len(payload) == 0 || strings.TrimSpace(payload[0]) == "" {
			return response.Error(c, fiber.StatusBadRequest, "invalid_payload", "multipart field payload is required")
		}
		if err := json.Unmarshal([]byte(payload[0]), &body); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_json", "payload must be valid JSON")
		}
		if !body.PhotosSkipped && len(form.File["images"]) > 0 {
			rels, uerr := h.saveOnboardingPhotos(c.UserContext(), uid, form.File["images"])
			if uerr != nil {
				return mapOnboardingUploadError(c, uerr)
			}
			photoRels = rels
		}
	} else {
		if err := c.BodyParser(&body); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
		}
	}

	res, err := h.svc.CompleteOnboarding(c.UserContext(), uid, body, photoRels)
	if err != nil {
		return mapProfileError(c, err)
	}
	stripAdsIfEntitled(c.UserContext(), h.premium, uid, &res.StarterRoutine.ProductSuggestions)
	return response.JSON(c, fiber.StatusOK, res)
}

func (h *ProfileHandler) saveOnboardingPhotos(ctx context.Context, userID uuid.UUID, files []*multipart.FileHeader) ([]string, error) {
	if h == nil || h.cfg == nil || h.store == nil {
		return nil, errUploadUnavailable
	}
	if len(files) == 0 {
		return nil, nil
	}
	if len(files) > maxOnboardingPhotos {
		return nil, fmt.Errorf("%w: maximum %d onboarding photos", errUploadInvalid, maxOnboardingPhotos)
	}

	maxBytes := int64(h.cfg.Upload.MaxMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}

	rels := make([]string, 0, len(files))
	for _, fh := range files {
		if fh.Size <= 0 {
			return nil, fmt.Errorf("%w: empty_image", errUploadInvalid)
		}
		if fh.Size > maxBytes {
			return nil, fmt.Errorf("%w: file_too_large", errUploadTooLarge)
		}
		ext, ok := extFromFile(fh)
		if !ok {
			return nil, fmt.Errorf("%w: invalid_image", errUploadInvalid)
		}
		data, rerr := readAllFromMultipartHeader(fh)
		if rerr != nil {
			return nil, fmt.Errorf("%w: read_failed", errUploadFailed)
		}
		if err := verifyImageBytes(data); err != nil {
			return nil, fmt.Errorf("%w: invalid_image", errUploadInvalid)
		}

		filename := uuid.New().String() + ext
		rel := pathJoinSlash(pathJoinSlash(userID.String(), "onboarding"), filename)
		if err := h.store.Save(ctx, rel, data, contentTypeForExt(ext)); err != nil {
			return nil, fmt.Errorf("%w: save_failed", errUploadFailed)
		}
		rels = append(rels, rel)
	}
	return rels, nil
}

var (
	errUploadUnavailable = errors.New("upload_unavailable")
	errUploadInvalid     = errors.New("upload_invalid")
	errUploadTooLarge    = errors.New("upload_too_large")
	errUploadFailed      = errors.New("upload_failed")
)

func mapOnboardingUploadError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, errUploadUnavailable):
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "configuration missing")
	case errors.Is(err, errUploadTooLarge):
		return response.Error(c, fiber.StatusRequestEntityTooLarge, "file_too_large", err.Error())
	case errors.Is(err, errUploadInvalid):
		return response.Error(c, fiber.StatusBadRequest, "invalid_image", err.Error())
	case errors.Is(err, errUploadFailed):
		return response.Error(c, fiber.StatusInternalServerError, "save_failed", "could not persist uploaded image")
	default:
		return response.Error(c, fiber.StatusBadRequest, "upload_error", err.Error())
	}
}

// PreviewOnboardingComplete handles POST /onboarding/preview-complete (guest trial, no DB write).
func (h *ProfileHandler) PreviewOnboardingComplete(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "profile service unavailable")
	}
	var body dto.OnboardingCompleteRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	starter, err := h.svc.PreviewOnboardingComplete(c.UserContext(), body)
	if err != nil {
		return mapProfileError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, starter)
}

// GetPreviewRoutine handles GET /onboarding/preview-routine/:id (guest poll).
func (h *ProfileHandler) GetPreviewRoutine(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "profile service unavailable")
	}
	jobID := strings.TrimSpace(c.Params("id"))
	res, ok, err := h.svc.GetPreviewRoutineJob(jobID)
	if err != nil {
		return mapProfileError(c, err)
	}
	if !ok {
		return response.Error(c, fiber.StatusNotFound, "not_found", "preview job not found or expired")
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// DeleteOnboarding handles DELETE /profile/onboarding.
func (h *ProfileHandler) DeleteOnboarding(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "profile service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	res, err := h.svc.DeleteOnboarding(c.UserContext(), uid)
	if err != nil {
		return mapProfileError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

func mapProfileError(c *fiber.Ctx, err error) error {
	if errors.Is(err, profileuc.ErrInvalidInput) {
		return response.Error(c, fiber.StatusBadRequest, "invalid_input", err.Error())
	}
	if errors.Is(err, profileuc.ErrOnboardingNotFound) {
		return response.Error(c, fiber.StatusNotFound, "onboarding_not_found", err.Error())
	}
	return response.Error(c, fiber.StatusInternalServerError, "profile_error", err.Error())
}
