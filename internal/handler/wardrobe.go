package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	usageuc "github.com/dadiary/backend/internal/usecase/usage"
	wardrobeuc "github.com/dadiary/backend/internal/usecase/wardrobe"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// WardrobeHandler serves /wardrobe routes.
type WardrobeHandler struct {
	svc        *wardrobeuc.Service
	cfg        *config.Config
	httpClient *http.Client
	profiles   *repository.GormSkinProfileRepository
	checks     *repository.GormSkinCheckRepository
}

// NewWardrobeHandler constructs WardrobeHandler. cfg may be nil (scan disabled).
func NewWardrobeHandler(svc *wardrobeuc.Service, cfg *config.Config) *WardrobeHandler {
	return &WardrobeHandler{
		svc: svc,
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 3 * time.Minute,
		},
	}
}

// AttachSkinSources wires profile + recent check-ins into the cabinet card.
// Both may be nil; the card then stays "maybe" / "chưa nên" instead of guessing a skin type.
func (h *WardrobeHandler) AttachSkinSources(
	profiles *repository.GormSkinProfileRepository,
	checks *repository.GormSkinCheckRepository,
) {
	if h == nil {
		return
	}
	h.profiles = profiles
	h.checks = checks
}

// CreateProduct handles POST /wardrobe/products.
func (h *WardrobeHandler) CreateProduct(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "wardrobe unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	var body dto.CreateWardrobeProductRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	res, err := h.svc.Create(c.UserContext(), uid, body)
	if err != nil {
		return mapWardrobeWriteError(c, err)
	}
	return response.JSON(c, fiber.StatusCreated, res)
}

// ScanProduct handles POST /wardrobe/products/scan — vision OCR suggestion only (no persist).
func (h *WardrobeHandler) ScanProduct(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "wardrobe unavailable")
	}
	if h.cfg == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "configuration missing")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	// Scan is suggestion-only (not persisted). Do not apply the Free create
	// slot gate — that gate belongs on POST /wardrobe/products.

	form, err := c.MultipartForm()
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_multipart", "expected multipart form data")
	}
	defer func() { _ = form.RemoveAll() }()

	files := form.File["image"]
	if len(files) == 0 {
		files = form.File["images"]
	}
	if len(files) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "missing_image", "upload one product label photo")
	}
	if len(files) > 1 {
		return response.Error(c, fiber.StatusBadRequest, "too_many_images", "upload exactly one photo")
	}
	fh := files[0]
	maxBytes := int64(h.cfg.Upload.MaxMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = 10 * 1024 * 1024
	}
	if fh.Size > maxBytes {
		return response.Error(c, fiber.StatusRequestEntityTooLarge, "file_too_large", fmt.Sprintf("image must be <= %d MB", h.cfg.Upload.MaxMB))
	}
	if _, ok := extFromFile(fh); !ok {
		return response.Error(c, fiber.StatusBadRequest, "invalid_image", "only jpeg, png, webp, gif are allowed")
	}
	data, rerr := readAllFromMultipartHeader(fh)
	if rerr != nil || len(data) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "read_failed", "could not read uploaded image")
	}

	out, err := ai.WardrobeLabelScan(c.UserContext(), h.cfg, h.httpClient, data, firstFormLocale(form))
	if err != nil {
		msg := err.Error()
		if strings.Contains(strings.ToLower(msg), "api key") {
			return response.Error(c, fiber.StatusServiceUnavailable, "openai_not_configured", "OpenAI API key required for label scan")
		}
		return response.Error(c, fiber.StatusUnprocessableEntity, "scan_failed", msg)
	}
	return response.JSON(c, fiber.StatusOK, out)
}

// ProductInsight handles POST /wardrobe/products/:id/insight.
// One structured cabinet card for a saved product. Not a chat endpoint.
// The card is stored on the product and returned on this response and on GET /wardrobe.
func (h *WardrobeHandler) ProductInsight(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "wardrobe unavailable")
	}
	if h.cfg == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "configuration missing")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	productID, err := parseWardrobeProductID(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "product id must be a UUID")
	}
	product, err := h.svc.GetOwned(c.UserContext(), uid, productID)
	if err != nil {
		return mapWardrobeWriteError(c, err)
	}
	profile, recent, err := h.loadInsightSkin(c, uid)
	if err != nil {
		slog.Warn("wardrobe insight: skin context failed", "error", err)
		return response.Error(c, fiber.StatusInternalServerError, "wardrobe_error", "could not load skin context")
	}
	card, err := ai.GenerateWardrobeProductInsight(c.UserContext(), h.cfg, h.httpClient, ai.WardrobeProductInsightRequest{
		Name:     product.Name,
		Brand:    product.Brand,
		Category: product.Category,
		Notes:    product.Notes,
		Profile:  profile,
		Recent:   recent,
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "api key") {
			return response.Error(c, fiber.StatusServiceUnavailable, "openai_not_configured", "OpenAI API key required for product insight")
		}
		slog.Warn("wardrobe insight: model failed", "error", err)
		return response.Error(c, fiber.StatusUnprocessableEntity, "insight_failed", "could not build product insight")
	}
	res, err := h.svc.SaveInsight(c.UserContext(), uid, productID, card)
	if err != nil {
		return mapWardrobeWriteError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

func (h *WardrobeHandler) loadInsightSkin(c *fiber.Ctx, userID uuid.UUID) (*domain.SkinProfile, []domain.SkinCheck, error) {
	var profile *domain.SkinProfile
	var recent []domain.SkinCheck
	if h != nil && h.profiles != nil {
		p, err := h.profiles.GetByUserID(c.UserContext(), userID)
		if err != nil {
			return nil, nil, err
		}
		profile = p
	}
	if h != nil && h.checks != nil {
		rows, err := h.checks.ListRecentForCoach(c.UserContext(), userID, uuid.Nil, 5)
		if err != nil {
			return nil, nil, err
		}
		recent = rows
	}
	return profile, recent, nil
}

// UpdateProduct handles PATCH /wardrobe/products/:id.
func (h *WardrobeHandler) UpdateProduct(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "wardrobe unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	productID, err := parseWardrobeProductID(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "product id must be a UUID")
	}
	var body dto.UpdateWardrobeProductRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_json", "body must be valid JSON")
	}
	res, err := h.svc.Update(c.UserContext(), uid, productID, body)
	if err != nil {
		return mapWardrobeWriteError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, res)
}

// DeleteProduct handles DELETE /wardrobe/products/:id.
func (h *WardrobeHandler) DeleteProduct(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "wardrobe unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	productID, err := parseWardrobeProductID(c)
	if err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_id", "product id must be a UUID")
	}
	if err := h.svc.Delete(c.UserContext(), uid, productID); err != nil {
		return mapWardrobeWriteError(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

// List handles GET /wardrobe.
func (h *WardrobeHandler) List(c *fiber.Ctx) error {
	if h == nil || h.svc == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "wardrobe unavailable")
	}
	uid := middleware.UserIDFromLocals(c)
	if uid == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing user")
	}
	res, err := h.svc.List(c.UserContext(), uid)
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, "wardrobe_error", "could not load wardrobe")
	}
	return response.JSON(c, fiber.StatusOK, res)
}

func parseWardrobeProductID(c *fiber.Ctx) (uuid.UUID, error) {
	return uuid.Parse(strings.TrimSpace(c.Params("id")))
}

func mapWardrobeWriteError(c *fiber.Ctx, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, wardrobeuc.ErrInvalidInput) {
		return response.Error(c, fiber.StatusBadRequest, "invalid_input", err.Error())
	}
	if errors.Is(err, wardrobeuc.ErrNotFound) {
		return response.Error(c, fiber.StatusNotFound, "not_found", "product not found")
	}
	if errors.Is(err, usageuc.ErrPremiumRequired) || errors.Is(err, usageuc.ErrQuotaExceeded) {
		return mapPremiumGateError(c, domain.FeatureWardrobeFull, err)
	}
	return response.Error(c, fiber.StatusInternalServerError, "wardrobe_error", "could not save product")
}
