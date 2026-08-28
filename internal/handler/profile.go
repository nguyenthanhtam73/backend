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
	users   userNamer
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

// AttachUsers lets onboarding photo keys include the username (Cloudflare R2 folders).
func (h *ProfileHandler) AttachUsers(users userNamer) {
	if h != nil {
		h.users = users
	}
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
	// Product affiliate intros for every plan (Premium same as Free).
	res.OnboardingSnapshot = h.svc.EnrichStarterAffiliateSnapshot(c.UserContext(), uid, res.OnboardingSnapshot)
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
	loc := onboardingLocaleFromRequest(body.Locale)
	stripStarterRoutineAdsIfEntitled(c.UserContext(), h.premium, uid, &res.StarterRoutine, loc)
	stripOnboardingSnapshotAdsIfEntitled(c.UserContext(), h.premium, uid, &res.Profile.OnboardingSnapshot, loc)
	return response.JSON(c, fiber.StatusOK, res)
}

// AttachOnboardingPhotos handles POST /profile/onboarding/photos (multipart images).
// Used after a fast JSON onboarding complete (guest claim) to attach face photos.
func (h *ProfileHandler) AttachOnboardingPhotos(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "profile service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}

	ct := string(c.Request().Header.ContentType())
	if !strings.HasPrefix(ct, "multipart/form-data") {
		return response.Error(c, fiber.StatusBadRequest, "invalid_multipart", "expected multipart form data")
	}
	form, err := c.MultipartForm()
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_multipart", "expected multipart form data")
	}
	defer func() { _ = form.RemoveAll() }()

	files := form.File["images"]
	if len(files) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "photos_required", "upload at least one face photo")
	}
	rels, uerr := h.saveOnboardingPhotos(c.UserContext(), uid, files)
	if uerr != nil {
		return mapOnboardingUploadError(c, uerr)
	}
	res, err := h.svc.AttachOnboardingPhotos(c.UserContext(), uid, rels)
	if err != nil {
		return mapProfileError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

func onboardingLocaleFromRequest(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "en":
		return "en"
	default:
		return "vi"
	}
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

		rel := uploadPhotoKey(ctx, h.users, userID, storage.KindOnboarding, ext)
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

// PreviewOnboardingComplete handles POST /onboarding/preview-complete (guest trial).
// Writes a short-lived onboarding_preview_jobs row for AI poll; no skin_profiles write.
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
// Requires ?token= (or X-Preview-Token) matching the secret returned at preview-complete.
func (h *ProfileHandler) GetPreviewRoutine(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "profile service unavailable")
	}
	jobID := strings.TrimSpace(c.Params("id"))
	token := strings.TrimSpace(c.Query("token"))
	if token == "" {
		token = strings.TrimSpace(c.Get("X-Preview-Token"))
	}
	res, ok, err := h.svc.GetPreviewRoutineJob(c.UserContext(), jobID, token)
	if err != nil {
		return mapProfileError(c, err)
	}
	if !ok {
		return response.Error(c, fiber.StatusNotFound, "not_found", "preview job not found or expired")
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// SkipOnboarding handles POST /profile/onboarding/skip.
// Marks the user as having entered the app without finishing onboarding.
func (h *ProfileHandler) SkipOnboarding(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "profile service unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	if err := h.svc.SkipOnboarding(c.UserContext(), uid); err != nil {
		return mapProfileError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"onboarding_skipped": true,
	})
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
