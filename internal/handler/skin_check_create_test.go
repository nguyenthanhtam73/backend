package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/storage"
	skincheckuc "github.com/dadiary/backend/internal/usecase/skincheck"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Smallest JPEG http.DetectContentType accepts.
var tinyJPEG = []byte{0xFF, 0xD8, 0xFF, 0xD9}

func TestCreateHTTP_PhotoContext(t *testing.T) {
	app, repo := newCreateApp(t)
	owner := uuid.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalsUserID, owner)
		return c.Next()
	})
	app.Post("/api/v1/skin-checks", newCreateHandler(t, repo).Create)

	status, raw := postCheck(t, app, map[string]string{
		"skip_mode":  "true",
		"user_note":  "má",
		"photo_meta": `{`,
	}, nil)
	if status != http.StatusBadRequest || createErrorCode(t, raw) != "invalid_photo_meta" {
		t.Fatalf("bad photo_meta status=%d body=%s", status, raw)
	}

	status, raw = postCheck(t, app, map[string]string{
		"skip_mode":    "true",
		"user_note":    "má",
		"skin_context": `[1]`,
	}, nil)
	if status != http.StatusBadRequest || createErrorCode(t, raw) != "invalid_skin_context" {
		t.Fatalf("bad skin_context status=%d body=%s", status, raw)
	}

	status, raw = postCheck(t, app, map[string]string{
		"photo_meta": `[{"kind":"closeup","zone":"left_cheek"},{"kind":"full_face"}]`,
	}, tinyJPEG)
	if status != http.StatusBadRequest || createErrorCode(t, raw) != "invalid_photo_meta" {
		t.Fatalf("too many meta status=%d body=%s", status, raw)
	}

	status, raw = postCheck(t, app, map[string]string{
		"photo_meta":   `[{"kind":"not_a_kind","zone":"left_cheek"}]`,
		"skin_context": `{"firmness":"rocky","duration":"months","pain":"itchy","extra":"không đổi","nope":1}`,
	}, tinyJPEG)
	if status != http.StatusCreated {
		t.Fatalf("unknown ids status=%d body=%s", status, raw)
	}
	env := decodeCreate(t, raw)
	if len(env.Data.Check.PhotoMeta) != 0 {
		t.Fatalf("unknown kind should be dropped, got %#v", env.Data.Check.PhotoMeta)
	}
	if env.Data.Check.SkinContext == nil || env.Data.Check.SkinContext.Firmness != "" || env.Data.Check.SkinContext.Duration != "months" || env.Data.Check.SkinContext.Pain != "itchy" || env.Data.Check.SkinContext.Extra != "không đổi" {
		t.Fatalf("skin_context %#v", env.Data.Check.SkinContext)
	}
	row := loadCheck(t, repo, env.Data.Check.ID)
	if !bytes.Contains(row.PhotoContext, []byte(`"duration":"months"`)) || bytes.Contains(row.PhotoContext, []byte("rocky")) {
		t.Fatalf("stored %s", row.PhotoContext)
	}

	status, raw = postCheck(t, app, map[string]string{
		"photo_meta":   `[{"kind":"closeup","zone":"left_cheek"}]`,
		"skin_context": `{"firmness":"firm","duration":"months","pain":"none"}`,
	}, tinyJPEG)
	if status != http.StatusCreated {
		t.Fatalf("valid status=%d body=%s", status, raw)
	}
	env = decodeCreate(t, raw)
	if len(env.Data.Check.PhotoMeta) != 1 || env.Data.Check.PhotoMeta[0].Kind != "closeup" || env.Data.Check.PhotoMeta[0].Zone != "left_cheek" || env.Data.Check.PhotoMeta[0].Index != 0 {
		t.Fatalf("echo %#v", env.Data.Check.PhotoMeta)
	}
	if env.Data.Check.SkinContext == nil || env.Data.Check.SkinContext.Firmness != "firm" {
		t.Fatalf("echo skin %#v", env.Data.Check.SkinContext)
	}

	status, raw = postCheck(t, app, map[string]string{
		"photo_meta": `[{"kind":"closeup"}]`,
	}, tinyJPEG)
	if status != http.StatusCreated {
		t.Fatalf("closeup without zone status=%d body=%s", status, raw)
	}
	env = decodeCreate(t, raw)
	if len(env.Data.Check.PhotoMeta) != 1 || env.Data.Check.PhotoMeta[0].Zone != "other" {
		t.Fatalf("missing close-up zone %#v", env.Data.Check.PhotoMeta)
	}
}

func TestCreateHTTP_OldFormOmitsPhotoContext(t *testing.T) {
	app, repo := newCreateApp(t)
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalsUserID, uuid.New())
		return c.Next()
	})
	app.Post("/api/v1/skin-checks", newCreateHandler(t, repo).Create)

	status, raw := postCheck(t, app, map[string]string{
		"skip_mode":  "true",
		"user_note":  "má hơi đỏ",
		"conditions": "dry",
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	data := body["data"].(map[string]any)
	check := data["check"].(map[string]any)
	for _, key := range []string{"id", "user_note", "conditions", "visibility", "check_date"} {
		if _, ok := check[key]; !ok {
			t.Fatalf("old form missing %s: %s", key, raw)
		}
	}
	if _, ok := check["photo_meta"]; ok {
		t.Fatalf("old form grew photo_meta: %s", raw)
	}
	if _, ok := check["skin_context"]; ok {
		t.Fatalf("old form grew skin_context: %s", raw)
	}
	if _, ok := data["analysis"]; !ok {
		t.Fatal("analysis missing")
	}
	if _, ok := data["image_urls"]; !ok {
		t.Fatal("image_urls missing")
	}
}

func newCreateApp(t *testing.T) (*fiber.App, *repository.GormSkinCheckRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:skin_create_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.SkinCheck{}, &domain.SkinAnalysis{}); err != nil {
		t.Fatal(err)
	}
	return fiber.New(), repository.NewSkinCheckRepository(db)
}

func newCreateHandler(t *testing.T, repo *repository.GormSkinCheckRepository) *SkinCheckHandler {
	t.Helper()
	cfg := &config.Config{Upload: config.UploadConfig{Dir: t.TempDir(), MaxMB: 10}}
	store, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	svc := skincheckuc.NewService(cfg, repo, nil, &handlerEnqueuer{}, store, nil, nil)
	return NewSkinCheckHandler(svc, repo, cfg, nil)
}

func postCheck(t *testing.T, app *fiber.App, fields map[string]string, jpeg []byte) (int, []byte) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	if jpeg != nil {
		part, err := w.CreateFormFile("images", "face.jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(jpeg); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/skin-checks", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, raw
}

func createErrorCode(t *testing.T, raw []byte) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	return env.Error.Code
}

type createEnvelope struct {
	Data struct {
		Check struct {
			ID        string `json:"id"`
			PhotoMeta []struct {
				Index int    `json:"index"`
				Kind  string `json:"kind"`
				Zone  string `json:"zone"`
			} `json:"photo_meta"`
			SkinContext *struct {
				Firmness string `json:"firmness"`
				Duration string `json:"duration"`
				Pain     string `json:"pain"`
				Extra    string `json:"extra"`
			} `json:"skin_context"`
		} `json:"check"`
	} `json:"data"`
}

func decodeCreate(t *testing.T, raw []byte) createEnvelope {
	t.Helper()
	var env createEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return env
}

func loadCheck(t *testing.T, repo *repository.GormSkinCheckRepository, id string) *domain.SkinCheck {
	t.Helper()
	uid, err := uuid.Parse(id)
	if err != nil {
		t.Fatal(err)
	}
	row, err := repo.GetByID(context.Background(), uid)
	if err != nil || row == nil {
		t.Fatal(err)
	}
	return row
}
