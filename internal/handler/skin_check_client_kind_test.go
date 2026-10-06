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
	authuc "github.com/dadiary/backend/internal/usecase/auth"
	skincheckuc "github.com/dadiary/backend/internal/usecase/skincheck"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCreateHTTP_ClientHeaderSelectsVoice(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:client_kind_http_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.SkinCheck{}, &domain.SkinAnalysis{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewSkinCheckRepository(db)
	enq := &handlerEnqueuer{}
	svc := skincheckuc.NewService(&config.Config{}, repo, nil, enq, nil, nil, nil)
	h := NewSkinCheckHandler(svc, repo, &config.Config{}, nil)
	owner := uuid.New()

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalsUserID, owner)
		return c.Next()
	})
	app.Post("/api/v1/skin-checks", h.Create)

	cases := []struct {
		header string
		want   string
	}{
		{"android", domain.RefreshClientAndroid},
		{"  android  ", domain.RefreshClientAndroid},
		{"", domain.RefreshClientWeb},
		{"Android", domain.RefreshClientWeb},
		{"dadiary-android", domain.RefreshClientWeb},
		{"web", domain.RefreshClientWeb},
	}
	for _, tc := range cases {
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		if err := w.WriteField("skip_mode", "true"); err != nil {
			t.Fatal(err)
		}
		if err := w.WriteField("user_note", "má hơi đỏ"); err != nil {
			t.Fatal(err)
		}
		if err := w.WriteField("climate_context", `{"client":"dadiary-android","coach_skill_level":"beginner"}`); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/skin-checks", &body)
		req.Header.Set("Content-Type", w.FormDataContentType())
		if tc.header != "" {
			req.Header.Set(authuc.ClientHeader, tc.header)
		}
		res, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("header %q status=%d body=%s", tc.header, res.StatusCode, raw)
		}
		var env struct {
			Data struct {
				Check struct {
					ID string `json:"id"`
				} `json:"check"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatal(err)
		}
		id, err := uuid.Parse(env.Data.Check.ID)
		if err != nil {
			t.Fatal(err)
		}
		row, err := repo.GetByID(context.Background(), id)
		if err != nil || row == nil {
			t.Fatal(err)
		}
		if row.ClientKind != tc.want {
			t.Fatalf("header %q stored %q want %q", tc.header, row.ClientKind, tc.want)
		}
	}
	if len(enq.ids) != len(cases) {
		t.Fatalf("enqueued %d", len(enq.ids))
	}
}

func TestReanalyzeHTTP_AndroidHeaderUpgradesStoredVoice(t *testing.T) {
	fx := setupReanalyzeHTTP(t, &handlerEnqueuer{})
	fx.caller = fx.owner
	check := fx.seed(t, json.RawMessage(`["checks/a.jpg"]`), domain.AnalysisStatusCompleted)

	status, _, raw := fx.doHeader(t, http.MethodPost, "/api/v1/skin-checks/"+check.ID.String()+"/reanalyze", "  android  ")
	if status != http.StatusOK {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	got, err := fx.repo.GetByID(context.Background(), check.ID)
	if err != nil || got == nil {
		t.Fatal(err)
	}
	if got.ClientKind != domain.RefreshClientAndroid {
		t.Fatalf("stored %q", got.ClientKind)
	}

	kept := fx.seed(t, json.RawMessage(`["checks/b.jpg"]`), domain.AnalysisStatusCompleted)
	if err := fx.repo.SetClientKind(context.Background(), kept.ID, domain.RefreshClientAndroid); err != nil {
		t.Fatal(err)
	}
	status, _, raw = fx.doHeader(t, http.MethodPost, "/api/v1/skin-checks/"+kept.ID.String()+"/reanalyze", "web")
	if status != http.StatusOK {
		t.Fatalf("web reanalyze status=%d body=%s", status, raw)
	}
	row, err := fx.repo.GetByID(context.Background(), kept.ID)
	if err != nil || row == nil {
		t.Fatal(err)
	}
	if row.ClientKind != domain.RefreshClientAndroid {
		t.Fatalf("web reanalyze stored %q", row.ClientKind)
	}
}

func (fx *reanalyzeFixture) doHeader(t *testing.T, method, path, client string) (int, apiEnvelope, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if client != "" {
		req.Header.Set(authuc.ClientHeader, client)
	}
	res, err := fx.app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return res.StatusCode, env, raw
}
