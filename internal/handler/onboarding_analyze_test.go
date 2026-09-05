package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/dadiary/backend/internal/config"
	"github.com/gofiber/fiber/v2"
)

// The onboarding UI maps `error.code` onto a specific message, so these codes are
// contract: renaming one silently downgrades the user to the generic AI error.

func newAnalyzeTestApp() *fiber.App {
	cfg := &config.Config{}
	cfg.Upload.MaxMB = 10
	h := NewOnboardingAnalyzeHandler(cfg)

	app := fiber.New(fiber.Config{BodyLimit: 100 * 1024 * 1024})
	app.Post("/analyze", h.AnalyzeSkin)
	return app
}

type analyzeImage struct {
	filename string
	size     int
}

// jpegBytes builds a payload of n bytes that starts with a real JPEG magic number.
func jpegBytes(n int) []byte {
	buf := make([]byte, n)
	copy(buf, []byte{0xFF, 0xD8, 0xFF, 0xE0})
	return buf
}

func postAnalyze(t *testing.T, app *fiber.App, images []analyzeImage) (int, string) {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, img := range images {
		part, err := w.CreateFormFile("images", img.filename)
		if err != nil {
			t.Fatalf("create form file %s: %v", img.filename, err)
		}
		if _, err := part.Write(jpegBytes(img.size)); err != nil {
			t.Fatalf("write %s: %v", img.filename, err)
		}
	}
	if err := w.WriteField("locale", "vi"); err != nil {
		t.Fatalf("write locale: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, "/analyze", &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	var envelope struct {
		Success bool `json:"success"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode envelope from %q: %v", string(raw), err)
	}
	if envelope.Success {
		t.Fatalf("expected an error envelope, got success: %s", string(raw))
	}
	return res.StatusCode, envelope.Error.Code
}

func TestAnalyzeSkinRejectsBadUploads(t *testing.T) {
	app := newAnalyzeTestApp()

	tests := []struct {
		name       string
		images     []analyzeImage
		wantStatus int
		wantCode   string
	}{
		{
			// iPhone default format: never converted, so the extension check rejects it.
			name: "heic is rejected as an unreadable image",
			images: []analyzeImage{
				{filename: "IMG_0001.HEIC", size: 2048},
				{filename: "IMG_0002.HEIC", size: 2048},
			},
			wantStatus: fiber.StatusBadRequest,
			wantCode:   "invalid_image",
		},
		{
			name: "one heic among valid jpegs still rejects",
			images: []analyzeImage{
				{filename: "front.jpg", size: 2048},
				{filename: "side.heic", size: 2048},
			},
			wantStatus: fiber.StatusBadRequest,
			wantCode:   "invalid_image",
		},
		{
			name: "oversized photo is rejected by size, not format",
			images: []analyzeImage{
				{filename: "front.jpg", size: 2048},
				{filename: "huge.jpg", size: 11 * 1024 * 1024},
			},
			wantStatus: fiber.StatusRequestEntityTooLarge,
			wantCode:   "file_too_large",
		},
		{
			name:       "a single photo is too few",
			images:     []analyzeImage{{filename: "front.jpg", size: 2048}},
			wantStatus: fiber.StatusBadRequest,
			wantCode:   "too_few_images",
		},
		{
			name: "four photos are too many",
			images: []analyzeImage{
				{filename: "a.jpg", size: 1024},
				{filename: "b.jpg", size: 1024},
				{filename: "c.jpg", size: 1024},
				{filename: "d.jpg", size: 1024},
			},
			wantStatus: fiber.StatusBadRequest,
			wantCode:   "too_many_images",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status, code := postAnalyze(t, app, tc.images)
			if status != tc.wantStatus {
				t.Errorf("status = %d, want %d", status, tc.wantStatus)
			}
			if code != tc.wantCode {
				t.Errorf("error.code = %q, want %q", code, tc.wantCode)
			}
		})
	}
}

// A failed vision run used to return err.Error() verbatim, which leaks prompt
// fragments and vendor payloads to the browser.
func TestAnalyzeSkinDoesNotLeakPipelineErrors(t *testing.T) {
	app := newAnalyzeTestApp()

	// No API key configured, so the pipeline fails without reaching a vendor.
	status, code := postAnalyze(t, app, []analyzeImage{
		{filename: "front.jpg", size: 2048},
		{filename: "side.jpg", size: 2048},
	})

	if status == fiber.StatusOK {
		t.Fatalf("expected the pipeline to fail without credentials")
	}
	if code != "openai_not_configured" && code != "analysis_failed" {
		t.Fatalf("error.code = %q, want a code the frontend maps", code)
	}
	fmt.Printf("pipeline failure surfaced as %d / %s\n", status, code)
}
