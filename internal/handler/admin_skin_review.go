package handler

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	adminskinreviewuc "github.com/dadiary/backend/internal/usecase/adminskinreview"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// AdminSkinReviewHandler serves admin-only skin observation endpoints.
// Authz is enforced by RequireAdmin middleware on every route (403 if not admin).
// Free plan quotas are intentionally NOT applied.
type AdminSkinReviewHandler struct {
	svc *adminskinreviewuc.Service
	cfg *config.Config
}

// NewAdminSkinReviewHandler constructs the handler.
func NewAdminSkinReviewHandler(svc *adminskinreviewuc.Service, cfg *config.Config) *AdminSkinReviewHandler {
	return &AdminSkinReviewHandler{svc: svc, cfg: cfg}
}

// Create handles POST /api/v1/admin/skin-review
// Multipart: images (1–3), optional title, notes, status, locale.
func (h *AdminSkinReviewHandler) Create(c *fiber.Ctx) error {
	if h == nil || h.svc == nil || h.cfg == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin skin review unavailable")
	}
	adminID := middleware.UserIDFromLocals(c)
	if adminID == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}

	form, err := c.MultipartForm()
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_multipart", "expected multipart form data")
	}
	defer func() { _ = form.RemoveAll() }()

	files := form.File["images"]
	if len(files) < 1 {
		return response.Error(c, fiber.StatusBadRequest, "missing_images", "upload at least 1 skin photo")
	}
	if len(files) > 3 {
		return response.Error(c, fiber.StatusBadRequest, "too_many_images", "maximum 3 photos for admin skin review")
	}

	maxBytes := int64(h.cfg.Upload.MaxMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}

	images := make([]adminskinreviewuc.UploadImage, 0, len(files))
	for _, fh := range files {
		if fh.Size <= 0 {
			return response.Error(c, fiber.StatusBadRequest, "empty_image", "uploaded image is empty (0 bytes)")
		}
		if fh.Size > maxBytes {
			return response.Error(c, fiber.StatusRequestEntityTooLarge, "file_too_large",
				fmt.Sprintf("each image must be <= %d MB", h.cfg.Upload.MaxMB))
		}
		ext, ok := extFromFile(fh)
		if !ok {
			return response.Error(c, fiber.StatusBadRequest, "invalid_image", "only jpeg, png, webp, gif are allowed")
		}
		data, rerr := readAllFromMultipartHeader(fh)
		if rerr != nil {
			return response.Error(c, fiber.StatusBadRequest, "read_failed", "could not read uploaded image")
		}
		if err := verifyImageBytes(data); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_image", "uploaded file is not a recognizable image")
		}
		rel := pathJoinSlash(adminID.String(), pathJoinSlash("admin-skin-review", uuid.New().String()+ext))
		images = append(images, adminskinreviewuc.UploadImage{
			Rel:         rel,
			Data:        data,
			ContentType: contentTypeForExt(ext),
		})
	}

	in := adminskinreviewuc.CreateInput{
		Title:  firstValue(form.Value["title"]),
		Notes:  firstValue(form.Value["notes"]),
		Status: firstValue(form.Value["status"]),
		Locale: firstValue(form.Value["locale"]),
		Images: images,
	}

	res, err := h.svc.Create(c.UserContext(), adminID, in)
	if err != nil {
		return mapAdminSkinReviewError(c, err)
	}
	return response.JSON(c, fiber.StatusCreated, res)
}

// Get handles GET /api/v1/admin/skin-review/:id
func (h *AdminSkinReviewHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin skin review unavailable")
	}
	id, err := uuid.Parse(strings.TrimSpace(c.Params("id")))
	if err != nil || id == uuid.Nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "id must be a valid UUID")
	}
	res, err := h.svc.Get(c.UserContext(), id)
	if err != nil {
		return mapAdminSkinReviewError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// Patch handles PATCH /api/v1/admin/skin-review/:id (title / notes / status).
func (h *AdminSkinReviewHandler) Patch(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin skin review unavailable")
	}
	id, err := uuid.Parse(strings.TrimSpace(c.Params("id")))
	if err != nil || id == uuid.Nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "id must be a valid UUID")
	}
	var body dto.PatchAdminSkinReviewRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	res, err := h.svc.Patch(c.UserContext(), id, body)
	if err != nil {
		return mapAdminSkinReviewError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

func mapAdminSkinReviewError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, adminskinreviewuc.ErrUnavailable):
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", err.Error())
	case errors.Is(err, adminskinreviewuc.ErrNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", "admin skin review not found")
	case errors.Is(err, adminskinreviewuc.ErrInvalidInput):
		return response.Error(c, fiber.StatusBadRequest, "invalid_input", err.Error())
	case errors.Is(err, adminskinreviewuc.ErrAnalysis):
		msg := err.Error()
		if strings.Contains(strings.ToLower(msg), "api key") {
			return response.Error(c, fiber.StatusServiceUnavailable, "openai_not_configured", "OpenAI API key required for photo analysis")
		}
		return response.Error(c, fiber.StatusUnprocessableEntity, "analysis_failed", msg)
	default:
		return response.Error(c, fiber.StatusInternalServerError, "internal_error", err.Error())
	}
}
