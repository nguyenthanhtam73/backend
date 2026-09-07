package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	skincheckuc "github.com/dadiary/backend/internal/usecase/skincheck"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type handlerEnqueuer struct {
	mu    sync.Mutex
	ids   []uuid.UUID
	after func(uuid.UUID)
}

func (e *handlerEnqueuer) EnqueueAnalysis(id uuid.UUID) {
	e.mu.Lock()
	e.ids = append(e.ids, id)
	e.mu.Unlock()
	if e.after != nil {
		e.after(id)
	}
}

func (e *handlerEnqueuer) n() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.ids)
}

type reanalyzeFixture struct {
	app    *fiber.App
	repo   *repository.GormSkinCheckRepository
	enq    *handlerEnqueuer
	owner  uuid.UUID
	other  uuid.UUID
	caller uuid.UUID
}

func setupReanalyzeHTTP(t *testing.T, enq *handlerEnqueuer) *reanalyzeFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:reanalyze_http_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.SkinCheck{}, &domain.SkinAnalysis{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewSkinCheckRepository(db)
	svc := skincheckuc.NewService(&config.Config{}, repo, nil, enq, nil, nil, nil)
	h := NewSkinCheckHandler(svc, repo, &config.Config{}, nil)

	fx := &reanalyzeFixture{
		repo:   repo,
		enq:    enq,
		owner:  uuid.New(),
		other:  uuid.New(),
		caller: uuid.Nil,
	}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		if fx.caller != uuid.Nil {
			c.Locals(middleware.LocalsUserID, fx.caller)
		}
		return c.Next()
	})
	app.Post("/api/v1/skin-checks/:id/reanalyze", h.Reanalyze)
	app.Get("/api/v1/skin-checks/:id", h.Get)
	fx.app = app
	return fx
}

func (fx *reanalyzeFixture) seed(t *testing.T, images json.RawMessage, status domain.AnalysisStatus) *domain.SkinCheck {
	t.Helper()
	check := &domain.SkinCheck{
		UserID:     fx.owner,
		Title:      "check-in",
		ImageURLs:  images,
		Visibility: domain.CheckVisibilityPrivate,
		CheckDate:  time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	row := &domain.SkinAnalysis{
		Status:       status,
		SummaryNotes: "original",
		ModelVersion: "v-original",
	}
	if status == domain.AnalysisStatusCompleted {
		now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
		row.AnalyzedAt = &now
	}
	if err := fx.repo.CreateWithAnalysis(context.Background(), check, row); err != nil {
		t.Fatal(err)
	}
	got, err := fx.repo.GetByID(context.Background(), check.ID)
	if err != nil || got == nil {
		t.Fatalf("reload: %v", err)
	}
	return got
}

type apiEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (fx *reanalyzeFixture) do(t *testing.T, method, path string) (int, apiEnvelope, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
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

func TestReanalyzeHTTP_OwnerNotFoundAndNoPhotos(t *testing.T) {
	fx := setupReanalyzeHTTP(t, &handlerEnqueuer{})
	check := fx.seed(t, json.RawMessage(`["checks/a.jpg"]`), domain.AnalysisStatusCompleted)

	fx.caller = uuid.Nil
	status, env, _ := fx.do(t, http.MethodPost, "/api/v1/skin-checks/"+check.ID.String()+"/reanalyze")
	if status != fiber.StatusUnauthorized || env.Error == nil || env.Error.Code != "unauthorized" {
		t.Fatalf("missing auth: status=%d env=%+v", status, env.Error)
	}

	fx.caller = fx.other
	status, env, _ = fx.do(t, http.MethodPost, "/api/v1/skin-checks/"+check.ID.String()+"/reanalyze")
	if status != fiber.StatusNotFound || env.Error == nil || env.Error.Code != "not_found" {
		t.Fatalf("non-owner: status=%d env=%+v", status, env.Error)
	}

	skip := fx.seed(t, json.RawMessage(`[]`), domain.AnalysisStatusCompleted)
	fx.caller = fx.owner
	status, env, _ = fx.do(t, http.MethodPost, "/api/v1/skin-checks/"+skip.ID.String()+"/reanalyze")
	if status != fiber.StatusUnprocessableEntity || env.Error == nil || env.Error.Code != "photos_required" {
		t.Fatalf("no photos: status=%d env=%+v", status, env.Error)
	}
}

func TestReanalyzeHTTP_ProcessingIdempotentAndPollGet(t *testing.T) {
	var fx *reanalyzeFixture
	enq := &handlerEnqueuer{}
	fx = setupReanalyzeHTTP(t, enq)
	check := fx.seed(t, json.RawMessage(`["checks/a.jpg"]`), domain.AnalysisStatusCompleted)
	origDate := check.CheckDate
	origPhotos := append([]byte(nil), check.ImageURLs...)

	fx.caller = fx.owner
	status, env, _ := fx.do(t, http.MethodPost, "/api/v1/skin-checks/"+check.ID.String()+"/reanalyze")
	if status != fiber.StatusOK || !env.Success {
		t.Fatalf("first reanalyze: status=%d body=%s", status, env.Data)
	}
	var first struct {
		Check struct {
			ID        string `json:"id"`
			CheckDate string `json:"check_date"`
		} `json:"check"`
		Analysis struct {
			Status string `json:"status"`
		} `json:"analysis"`
		ImageURLs []string `json:"image_urls"`
	}
	if err := json.Unmarshal(env.Data, &first); err != nil {
		t.Fatal(err)
	}
	if first.Check.ID != check.ID.String() {
		t.Fatalf("id=%s", first.Check.ID)
	}
	if first.Analysis.Status != string(domain.AnalysisStatusProcessing) {
		t.Fatalf("status=%s", first.Analysis.Status)
	}

	status, env, _ = fx.do(t, http.MethodPost, "/api/v1/skin-checks/"+check.ID.String()+"/reanalyze")
	if status != fiber.StatusOK || !env.Success {
		t.Fatalf("second reanalyze: status=%d", status)
	}
	if enq.n() != 1 {
		t.Fatalf("double job: %d", enq.n())
	}

	// Finish the job the way Process would: same analysis row, new notes.
	got, err := fx.repo.GetByID(context.Background(), check.ID)
	if err != nil || got == nil || got.Analysis == nil {
		t.Fatal(err)
	}
	got.Analysis.Status = domain.AnalysisStatusCompleted
	got.Analysis.SummaryNotes = "soi lai"
	now := time.Now().UTC()
	got.Analysis.AnalyzedAt = &now
	if err := fx.repo.SaveAnalysis(context.Background(), got.Analysis); err != nil {
		t.Fatal(err)
	}

	status, env, raw := fx.do(t, http.MethodGet, "/api/v1/skin-checks/"+check.ID.String())
	if status != fiber.StatusOK || !env.Success {
		t.Fatalf("GET poll: status=%d raw=%s", status, raw)
	}
	var polled struct {
		Check struct {
			ID        string `json:"id"`
			CheckDate string `json:"check_date"`
		} `json:"check"`
		Analysis struct {
			Status string `json:"status"`
			Coach  *struct {
				SummaryNotes string `json:"summary_notes"`
			} `json:"coach"`
		} `json:"analysis"`
		ImageURLs []string `json:"image_urls"`
	}
	if err := json.Unmarshal(env.Data, &polled); err != nil {
		t.Fatal(err)
	}
	if polled.Check.ID != check.ID.String() {
		t.Fatalf("poll id changed")
	}
	if polled.Check.CheckDate != origDate.UTC().Format("2006-01-02") {
		t.Fatalf("check_date=%s", polled.Check.CheckDate)
	}
	if polled.Analysis.Status != string(domain.AnalysisStatusCompleted) {
		t.Fatalf("poll status=%s", polled.Analysis.Status)
	}
	if polled.Analysis.Coach == nil || polled.Analysis.Coach.SummaryNotes != "soi lai" {
		t.Fatalf("poll did not show new analysis: %+v", polled.Analysis.Coach)
	}
	if len(polled.ImageURLs) != 1 || polled.ImageURLs[0] != "/uploads/checks/a.jpg" {
		t.Fatalf("photos=%v", polled.ImageURLs)
	}
	still, err := fx.repo.GetByID(context.Background(), check.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(still.ImageURLs) != string(origPhotos) {
		t.Fatalf("stored photos mutated")
	}
}

func TestReanalyzeHTTP_InvalidID(t *testing.T) {
	fx := setupReanalyzeHTTP(t, &handlerEnqueuer{})
	fx.caller = fx.owner
	status, env, _ := fx.do(t, http.MethodPost, "/api/v1/skin-checks/not-a-uuid/reanalyze")
	if status != fiber.StatusBadRequest || env.Error == nil || env.Error.Code != "invalid_id" {
		t.Fatalf("invalid id: status=%d env=%+v", status, env.Error)
	}
}
