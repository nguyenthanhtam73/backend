package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/analysis"
	skincheckuc "github.com/dadiary/backend/internal/usecase/skincheck"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// SkinCheckHandler handles authenticated skin check-in routes.
type SkinCheckHandler struct {
	svc   *skincheckuc.Service
	repo  *repository.GormSkinCheckRepository
	cfg   *config.Config
}

// NewSkinCheckHandler constructs a SkinCheckHandler.
//
// The repository is used by GET /skin-checks/:id so the client can re-open
// a previous day's coach feedback (the same payload shape POST returns).
func NewSkinCheckHandler(svc *skincheckuc.Service, repo *repository.GormSkinCheckRepository, cfg *config.Config) *SkinCheckHandler {
	return &SkinCheckHandler{svc: svc, repo: repo, cfg: cfg}
}

// Create handles POST /skin-checks (multipart skin photos + metadata).
func (h *SkinCheckHandler) Create(c *fiber.Ctx) error {
	if h == nil || h.svc == nil || h.cfg == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "skin check service is not available")
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
	if len(files) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "missing_images", "field \"images\" must include at least one file")
	}
	const maxCheckInImages = 2
	if len(files) > maxCheckInImages {
		return response.Error(c, fiber.StatusBadRequest, "too_many_images", fmt.Sprintf("maximum %d photos per check-in", maxCheckInImages))
	}

	title := firstValue(form.Value["title"])
	userNote := firstValue(form.Value["user_note"])
	envNote := firstValue(form.Value["environment_note"])
	visStr := firstValue(form.Value["visibility"])
	conditions := parseTags(firstValue(form.Value["conditions"]))
	symptoms := parseTags(firstValue(form.Value["symptoms"]))
	climateRaw := strings.TrimSpace(firstValue(form.Value["climate_context"]))
	if climateRaw != "" && !json.Valid([]byte(climateRaw)) {
		return response.Error(c, fiber.StatusBadRequest, "invalid_climate_context", "field \"climate_context\" must be valid JSON when provided")
	}
	var climateJSON json.RawMessage
	if climateRaw != "" {
		climateJSON = json.RawMessage(climateRaw)
	}

	vis := normalizeVisibility(visStr)
	if vis == domain.CheckVisibility("") && strings.TrimSpace(visStr) != "" {
		return response.Error(c, fiber.StatusBadRequest, "invalid_visibility", "visibility must be public or private")
	}

	maxBytes := int64(h.cfg.Upload.MaxMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}

	images := make([]skincheckuc.UploadImage, 0, len(files))
	for _, fh := range files {
		// Reject empty uploads explicitly. This commonly happens when users paste a
		// "Copy as cURL" snippet from Chrome DevTools — Chrome strips binary file
		// bodies, leaving a 0-byte attachment. Without this check the file flows all
		// the way to the AI pipeline and surfaces as a confusing JSON-parse error.
		if fh.Size <= 0 {
			return response.Error(c, fiber.StatusBadRequest, "empty_image", "uploaded image is empty (0 bytes)")
		}
		if fh.Size > maxBytes {
			return response.Error(c, fiber.StatusRequestEntityTooLarge, "file_too_large", fmt.Sprintf("each image must be <= %d MB", h.cfg.Upload.MaxMB))
		}
		ext, ok := extFromFile(fh)
		if !ok {
			return response.Error(c, fiber.StatusBadRequest, "invalid_image", "only jpeg, png, webp, gif are allowed")
		}
		data, rerr := readAllFromMultipartHeader(fh)
		if rerr != nil {
			return response.Error(c, fiber.StatusInternalServerError, "read_failed", "could not read uploaded image")
		}
		// Verify magic bytes. Filename extension can be lied about, but
		// `http.DetectContentType` reads the actual header — catches non-image bodies
		// (e.g. a renamed .txt or a corrupt JPG) before the AI pipeline burns budget on it.
		if err := verifyImageBytes(data); err != nil {
			return response.Error(c, fiber.StatusBadRequest, "invalid_image", "uploaded file is not a recognizable image")
		}

		rel := pathJoinSlash(userID.String(), uuid.New().String()+ext)
		images = append(images, skincheckuc.UploadImage{
			Rel:         rel,
			Data:        data,
			ContentType: contentTypeForExt(ext),
		})
	}

	if vis == domain.CheckVisibility("") {
		vis = domain.CheckVisibilityPrivate
	}

	in := skincheckuc.CreateInput{
		Title:           title,
		UserNote:        userNote,
		Conditions:      conditions,
		Symptoms:        symptoms,
		ClimateContext:  climateJSON,
		EnvironmentNote: envNote,
		Visibility:      vis,
		Images:          images,
	}

	res, err := h.svc.Create(c.UserContext(), userID, in)
	if err != nil {
		return mapSkinCheckError(c, err)
	}
	return response.JSON(c, fiber.StatusCreated, res)
}

// Get handles GET /api/v1/skin-checks/:id and returns the same
// CreateSkinCheckResponse envelope (check + analysis.coach + image_urls) so the
// frontend can re-render a previously generated AI feedback without re-running
// the pipeline. 404 if not owned by the caller (no information leak).
func (h *SkinCheckHandler) Get(c *fiber.Ctx) error {
	if h == nil || h.repo == nil || h.cfg == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "skin check service is not available")
	}
	userID := middleware.UserIDFromLocals(c)
	if userID == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	idParam := strings.TrimSpace(c.Params("id"))
	id, err := uuid.Parse(idParam)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "skin check id must be a valid uuid")
	}
	check, err := h.repo.GetByIDForOwner(c.UserContext(), id, userID)
	if err != nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "database_error", err.Error())
	}
	if check == nil {
		return response.Error(c, fiber.StatusNotFound, "not_found", "skin check not found")
	}
	if check.Analysis != nil {
		analysis.ExpireStaleAnalysis(c.UserContext(), h.repo, check.Analysis)
	}

	publicURLs := buildPublicImageURLs(check.ImageURLs)
	return response.JSON(c, fiber.StatusOK, mapSkinCheckResponse(check, publicURLs))
}

func firstValue(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return strings.TrimSpace(v[0])
}

func normalizeVisibility(v string) domain.CheckVisibility {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "public":
		return domain.CheckVisibilityPublic
	case "private", "":
		return domain.CheckVisibilityPrivate
	default:
		return domain.CheckVisibility("")
	}
}

func parseTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var arr []string
		if err := json.Unmarshal([]byte(raw), &arr); err == nil {
			return arr
		}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func pathJoinSlash(a, b string) string {
	a = strings.Trim(a, "/")
	b = strings.Trim(b, "/")
	return a + "/" + b
}

func extFromFile(fh *multipart.FileHeader) (string, bool) {
	name := strings.ToLower(fh.Filename)
	switch {
	case strings.HasSuffix(name, ".jpg"), strings.HasSuffix(name, ".jpeg"):
		return ".jpg", true
	case strings.HasSuffix(name, ".png"):
		return ".png", true
	case strings.HasSuffix(name, ".webp"):
		return ".webp", true
	case strings.HasSuffix(name, ".gif"):
		return ".gif", true
	default:
		return "", false
	}
}

// verifyImageBytes checks that the sniffed MIME type of the raw upload starts
// with "image/". Filename extensions can lie, but http.DetectContentType reads
// the actual header — catching non-image bodies (a renamed .txt, a corrupt JPG)
// before they reach storage or the AI pipeline.
func verifyImageBytes(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("file is empty")
	}
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	mime := http.DetectContentType(head)
	if !strings.HasPrefix(mime, "image/") {
		return fmt.Errorf("not an image (mime=%s)", mime)
	}
	return nil
}

// contentTypeForExt maps a validated file extension to a stored content type so
// proxied R2 objects serve with a correct MIME header.
func contentTypeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}

// buildPublicImageURLs converts the JSON-stored relative paths back into the
// "/uploads/..." URLs the frontend uses to render thumbnails.
func buildPublicImageURLs(raw json.RawMessage) []string {
	rels, _ := dto.DecodeStringSlice(raw)
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		clean := strings.TrimLeft(strings.ReplaceAll(rel, "\\", "/"), "/")
		if clean == "" {
			continue
		}
		out = append(out, "/uploads/"+clean)
	}
	return out
}

func mapSkinCheckResponse(check *domain.SkinCheck, publicURLs []string) dto.CreateSkinCheckResponse {
	if check == nil {
		return dto.CreateSkinCheckResponse{}
	}
	return dto.NewCreateSkinCheckResponse(check, check.Analysis, publicURLs)
}

func mapSkinCheckError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, skincheckuc.ErrInvalidInput):
		return response.Error(c, fiber.StatusBadRequest, "invalid_input", err.Error())
	case errors.Is(err, skincheckuc.ErrModerationRejected):
		return response.Error(c, fiber.StatusUnprocessableEntity, "moderation_failed", err.Error())
	case errors.Is(err, skincheckuc.ErrDatabase):
		return response.Error(c, fiber.StatusServiceUnavailable, "database_error", err.Error())
	default:
		return response.Error(c, fiber.StatusInternalServerError, "internal_error", "could not create skin check")
	}
}
