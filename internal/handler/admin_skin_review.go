package handler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/storage"
	adminskinreviewuc "github.com/dadiary/backend/internal/usecase/adminskinreview"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// AdminSkinReviewHandler serves skin observation endpoints for full admins and
// skin-review operators. Authz is RequireSkinReview (403 otherwise).
// Free plan quotas are intentionally NOT applied.
//
// PublicShare is the unauthenticated GET by slug (registered without jwt/admin).
type AdminSkinReviewHandler struct {
	svc   *adminskinreviewuc.Service
	cfg   *config.Config
	users userNamer
}

// NewAdminSkinReviewHandler constructs the handler.
func NewAdminSkinReviewHandler(svc *adminskinreviewuc.Service, cfg *config.Config) *AdminSkinReviewHandler {
	return &AdminSkinReviewHandler{svc: svc, cfg: cfg}
}

// AttachUsers lets admin-review photo keys include the operator username (Cloudflare R2 folders).
func (h *AdminSkinReviewHandler) AttachUsers(users userNamer) {
	if h != nil {
		h.users = users
	}
}

// Create handles POST /api/v1/admin/skin-review
// Multipart: images (1 required, up to 3 optional extras), optional title, notes,
// user_question, answer, status, locale.
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
		return response.Error(c, fiber.StatusBadRequest, "missing_images", "upload 1 skin photo (required)")
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
		rel := uploadPhotoKey(c.UserContext(), h.users, adminID, storage.KindAdminSkinReview, ext)
		images = append(images, adminskinreviewuc.UploadImage{
			Rel:         rel,
			Data:        data,
			ContentType: contentTypeForExt(ext),
		})
	}

	in := adminskinreviewuc.CreateInput{
		Title:        firstValue(form.Value["title"]),
		Notes:        firstValue(form.Value["notes"]),
		UserQuestion: firstValue(form.Value["user_question"]),
		Answer:       firstValue(form.Value["answer"]),
		Status:       firstValue(form.Value["status"]),
		Locale:       firstValue(form.Value["locale"]),
		SkinContext:  firstValue(form.Value["skin_context"]),
		Images:       images,
	}

	res, err := h.svc.Create(c.UserContext(), adminID, in)
	if err != nil {
		return mapAdminSkinReviewError(c, err)
	}
	return response.JSON(c, fiber.StatusCreated, res)
}

// List handles GET /api/v1/admin/skin-reviews?status=&page=&page_size=
func (h *AdminSkinReviewHandler) List(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin skin review unavailable")
	}
	filter := repository.AdminSkinReviewListFilter{
		Status: strings.TrimSpace(c.Query("status")),
	}
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			filter.Page = n
		}
	}
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			filter.PageSize = n
		}
	}
	res, err := h.svc.List(c.UserContext(), filter)
	if err != nil {
		return mapAdminSkinReviewError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
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

// Patch handles PATCH /api/v1/admin/skin-review/:id
// (title / notes / user_question / answer / status).
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

// Reanalyze handles POST /api/v1/admin/skin-review/:id/reanalyze
// Re-runs vision on saved images with the current (or override) user_question.
func (h *AdminSkinReviewHandler) Reanalyze(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin skin review unavailable")
	}
	id, err := uuid.Parse(strings.TrimSpace(c.Params("id")))
	if err != nil || id == uuid.Nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "id must be a valid UUID")
	}
	var body dto.ReanalyzeAdminSkinReviewRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
		}
	}
	res, err := h.svc.Reanalyze(c.UserContext(), id, body)
	if err != nil {
		return mapAdminSkinReviewError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// SuggestAnswer handles POST /api/v1/admin/skin-review/:id/suggest-answer
// Drafts a short public reply from user_question + saved analysis (not persisted).
// Optional refresh_analysis re-runs vision first; tips/laterality are always aligned to the question.
func (h *AdminSkinReviewHandler) SuggestAnswer(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin skin review unavailable")
	}
	id, err := uuid.Parse(strings.TrimSpace(c.Params("id")))
	if err != nil || id == uuid.Nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "id must be a valid UUID")
	}
	var body dto.SuggestAdminSkinReviewAnswerRequest
	if len(c.Body()) > 0 {
		if err := c.BodyParser(&body); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
		}
	}
	res, err := h.svc.SuggestAnswer(c.UserContext(), id, body)
	if err != nil {
		return mapAdminSkinReviewError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// Publish handles PATCH /api/v1/admin/skin-review/:id/publish
// Generates a unique public_slug, privacy-blurs images, sets is_public=true.
func (h *AdminSkinReviewHandler) Publish(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin skin review unavailable")
	}
	id, err := uuid.Parse(strings.TrimSpace(c.Params("id")))
	if err != nil || id == uuid.Nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "id must be a valid UUID")
	}
	res, err := h.svc.Publish(c.UserContext(), id)
	if err != nil {
		return mapAdminSkinReviewError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// Unpublish handles PATCH /api/v1/admin/skin-review/:id/unpublish
func (h *AdminSkinReviewHandler) Unpublish(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin skin review unavailable")
	}
	id, err := uuid.Parse(strings.TrimSpace(c.Params("id")))
	if err != nil || id == uuid.Nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "id must be a valid UUID")
	}
	res, err := h.svc.Unpublish(c.UserContext(), id)
	if err != nil {
		return mapAdminSkinReviewError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// GetPublic handles GET /api/v1/public/skin-review/:slug (no auth).
// Returns observations + blurred image URLs + optional public Q&A —
// never admin notes / originals.
func (h *AdminSkinReviewHandler) GetPublic(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin skin review unavailable")
	}
	slug := strings.TrimSpace(c.Params("slug"))
	res, err := h.svc.GetPublic(c.UserContext(), slug)
	if err != nil {
		return mapAdminSkinReviewError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// ListPublicSitemap handles GET /api/v1/public/skin-reviews (no auth).
// Used by Next.js sitemap.xml — returns slug + timestamps only.
func (h *AdminSkinReviewHandler) ListPublicSitemap(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "admin skin review unavailable")
	}
	limit := 500
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	res, err := h.svc.ListPublicForSitemap(c.UserContext(), limit)
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
