package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/token"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGetSkinCheck_StripsStoredTechnicalCopy(t *testing.T) {
	fx := setupReanalyzeHTTP(t, &handlerEnqueuer{})
	fx.caller = fx.owner
	check := fx.seed(t, json.RawMessage(`["checks/a.jpg"]`), domain.AnalysisStatusCompleted)
	got, err := fx.repo.GetByID(context.Background(), check.ID)
	if err != nil || got == nil || got.Analysis == nil {
		t.Fatal(err)
	}
	const storedSummary = "score 0.78 vì da vẫn khỏe mạnh"
	const storedSituation = "situation_analysis: Mày thấy da đỏ"
	got.Analysis.SummaryNotes = storedSummary
	got.Analysis.SkinScores = []byte(`{
		"overall": 0.78,
		"situation_analysis": "situation_analysis: Mày thấy da đỏ",
		"skin_score_notes": {
			"overall": "score 0,78 vì da vẫn khỏe mạnh",
			"clarity": "Có 2–3 nốt, SPF50, ngày 7"
		},
		"zone_notes": [{"zone":"left_cheek","note":"Má trông giống mụn ẩn, score 0.78.","severity":"mild"}],
		"clarify_questions": ["score 0.78 vì sờ thấy cứng?"],
		"concern_alignment": "Mày ghi score 0.78, tao thấy da ổn"
	}`)
	got.Analysis.RoutineHints = []byte(`["Sáng: SPF50","Tối: ngày 7"]`)
	if err := fx.repo.SaveAnalysis(context.Background(), got.Analysis); err != nil {
		t.Fatal(err)
	}

	status, env, raw := fx.do(t, http.MethodGet, "/api/v1/skin-checks/"+check.ID.String())
	if status != http.StatusOK || !env.Success {
		t.Fatalf("GET status=%d body=%s", status, raw)
	}
	var body struct {
		Analysis struct {
			Coach *struct {
				SummaryNotes     string   `json:"summary_notes"`
				SituationSummary string   `json:"situation_summary"`
				ConcernAlignment string   `json:"concern_alignment"`
				RoutineHints     []string `json:"routine_hints"`
				ClarifyQuestions []string `json:"clarify_questions"`
				ZoneNotes        []struct {
					Zone string `json:"zone"`
					Note string `json:"note"`
				} `json:"zone_notes"`
				SkinScoreNotes *struct {
					Overall string `json:"overall"`
					Clarity string `json:"clarity"`
				} `json:"skin_score_notes"`
				SkinScoreGauges *struct {
					Overall *float64 `json:"overall"`
				} `json:"skin_score_gauges"`
			} `json:"coach"`
		} `json:"analysis"`
	}
	if err := json.Unmarshal(env.Data, &body); err != nil {
		t.Fatal(err)
	}
	coach := body.Analysis.Coach
	if coach == nil {
		t.Fatal("missing coach")
	}
	if coach.SummaryNotes != "Da vẫn khỏe mạnh" {
		t.Fatalf("summary %q", coach.SummaryNotes)
	}
	if coach.SituationSummary != "Mày thấy da đỏ" {
		t.Fatalf("situation %q", coach.SituationSummary)
	}
	if coach.ConcernAlignment != "Mày ghi, tao thấy da ổn" {
		t.Fatalf("concern %q", coach.ConcernAlignment)
	}
	if coach.SkinScoreNotes == nil || coach.SkinScoreNotes.Overall != "Da vẫn khỏe mạnh" {
		t.Fatalf("gauge reason %#v", coach.SkinScoreNotes)
	}
	if coach.SkinScoreNotes.Clarity != "Có 2–3 nốt, SPF50, ngày 7" {
		t.Fatalf("clarity %q", coach.SkinScoreNotes.Clarity)
	}
	if len(coach.ZoneNotes) != 1 || coach.ZoneNotes[0].Zone != "left_cheek" || coach.ZoneNotes[0].Note != "Má trông giống mụn ẩn." {
		t.Fatalf("zone %#v", coach.ZoneNotes)
	}
	if len(coach.ClarifyQuestions) != 1 || coach.ClarifyQuestions[0] != "Sờ thấy cứng?" {
		t.Fatalf("questions %#v", coach.ClarifyQuestions)
	}
	if len(coach.RoutineHints) != 2 || coach.RoutineHints[0] != "Sáng: SPF50" || coach.RoutineHints[1] != "Tối: ngày 7" {
		t.Fatalf("hints %#v", coach.RoutineHints)
	}
	if coach.SkinScoreGauges == nil || coach.SkinScoreGauges.Overall == nil || *coach.SkinScoreGauges.Overall != 0.78 {
		t.Fatal("numeric gauge should stay")
	}
	for _, prose := range []string{coach.SummaryNotes, coach.SituationSummary, coach.ConcernAlignment, coach.SkinScoreNotes.Overall, coach.SkinScoreNotes.Clarity, coach.ZoneNotes[0].Note, coach.ClarifyQuestions[0]} {
		if strings.Contains(strings.ToLower(prose), "score") || strings.Contains(prose, "0.78") || strings.Contains(prose, "0,78") || strings.Contains(prose, "situation_analysis") {
			t.Fatalf("prose still technical: %q", prose)
		}
	}

	still, err := fx.repo.GetByID(context.Background(), check.ID)
	if err != nil || still == nil || still.Analysis == nil {
		t.Fatal(err)
	}
	if still.Analysis.SummaryNotes != storedSummary {
		t.Fatalf("stored summary was rewritten: %q", still.Analysis.SummaryNotes)
	}
	if !strings.Contains(string(still.Analysis.SkinScores), storedSituation) {
		t.Fatalf("stored skin_scores changed: %s", still.Analysis.SkinScores)
	}
}

func TestProgressSnippet_StripsScoreOnRead(t *testing.T) {
	when := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	row := domain.SkinCheck{
		ID:        uuid.New(),
		CheckDate: when,
		CreatedAt: when,
		Analysis: &domain.SkinAnalysis{
			Status:       domain.AnalysisStatusCompleted,
			SummaryNotes: "score 0.78 vì da vẫn khỏe mạnh",
		},
	}
	timeline := dto.NewProgressTimelineResponse([]domain.SkinCheck{row}, 30, "")
	if len(timeline.Entries) != 1 || timeline.Entries[0].Snippet != "Da vẫn khỏe mạnh" {
		t.Fatalf("snippet %#v", timeline.Entries)
	}
}

func TestCoachCopyAudit_AdminCountsExcludeExample(t *testing.T) {
	app, db, tok := newCoachAuditApp(t)
	now := time.Now().UTC()
	real := seedStatsUser(t, db, "coach-audit@dadiary.test", "realaudit", now, nil, nil, nil)
	other := seedStatsUser(t, db, "other-audit@dadiary.test", "otheraudit", now, nil, nil, nil)
	bot := seedStatsUser(t, db, "bot@example.com", "botaudit", now, nil, nil, nil)
	admin := seedStatsUser(t, db, adminEmail, "adminaudit", now, nil, nil, nil)
	member := seedStatsUser(t, db, "member-audit@dadiary.test", "memberaudit", now, nil, nil, nil)

	seedAuditCheck(t, db, real.ID, "web", now, "Mày thấy score 0.78 vì da đỏ", `{"overall":0.62,"situation_analysis":"da ổn"}`)
	seedAuditCheck(t, db, real.ID, "web", now, "Da ổn, có 2–3 nốt.", `{"overall":0.5}`)
	seedAuditCheck(t, db, other.ID, "android", now, "Da bạn ổn.", `{"overall":0.4,"skin_score_notes":{"overall":"da khỏe"}}`)
	seedAuditCheck(t, db, bot.ID, "web", now, "score 0.78 tao thấy", `{"overall":0.2}`)
	seedAuditCheck(t, db, real.ID, "web", now.AddDate(0, 0, -40), "score 0.11 mày ơi", `{"overall":0.1}`)

	status, raw := getCoachAudit(t, app, "", "days=7")
	if status != http.StatusUnauthorized {
		t.Fatalf("no auth status=%d body=%s", status, raw)
	}
	status, raw = getCoachAudit(t, app, bearer(t, tok, member.ID), "days=7")
	if status != http.StatusForbidden {
		t.Fatalf("member status=%d body=%s", status, raw)
	}
	status, raw = getCoachAudit(t, app, bearer(t, tok, admin.ID), "days=0")
	if status != http.StatusBadRequest || !strings.Contains(string(raw), "invalid_days") {
		t.Fatalf("days status=%d body=%s", status, raw)
	}

	status, raw = getCoachAudit(t, app, bearer(t, tok, admin.ID), "days=7")
	if status != http.StatusOK {
		t.Fatalf("admin status=%d body=%s", status, raw)
	}
	var env struct {
		Success bool                            `json:"success"`
		Data    dto.AdminCoachCopyAuditResponse `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	if !env.Success || env.Data.Days != 7 {
		t.Fatalf("envelope %#v", env)
	}
	byKind := map[string]dto.AdminCoachCopyClientCount{}
	for _, row := range env.Data.ByClientKind {
		byKind[row.ClientKind] = row
	}
	if byKind["web"].ChecksCompleted != 2 || byKind["web"].DistinctUsers != 1 {
		t.Fatalf("web %#v", byKind["web"])
	}
	if byKind["android"].ChecksCompleted != 1 || byKind["android"].DistinctUsers != 1 {
		t.Fatalf("android %#v", byKind["android"])
	}
	if env.Data.NotesMatchingScoreLeak != 1 || env.Data.NotesMatchingTaoMay != 1 {
		t.Fatalf("leaks score=%d tao=%d", env.Data.NotesMatchingScoreLeak, env.Data.NotesMatchingTaoMay)
	}
	body := string(raw)
	for _, secret := range []string{real.ID.String(), other.ID.String(), bot.ID.String(), "coach-audit@dadiary.test", "bot@example.com", "Mày thấy", "0.78"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response leaked %q: %s", secret, body)
		}
	}
}

func newCoachAuditApp(t *testing.T) (*fiber.App, *gorm.DB, *token.Service) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:coach_audit_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.SkinCheck{}, &domain.SkinAnalysis{}); err != nil {
		t.Fatal(err)
	}
	tok, err := token.NewService(config.JWTConfig{
		Secret:     "test-secret-for-coach-audit-32b!",
		AccessTTL:  time.Hour,
		RefreshTTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AdminEmails: []string{adminEmail}}
	app := fiber.New()
	Router(app, cfg, db, tok, nil)
	return app, db, tok
}

func seedAuditCheck(t *testing.T, db *gorm.DB, userID uuid.UUID, kind string, at time.Time, summary, scores string) {
	t.Helper()
	check := &domain.SkinCheck{
		UserID:     userID,
		ImageURLs:  []byte(`[]`),
		Visibility: domain.CheckVisibilityPrivate,
		CheckDate:  at,
		ClientKind: kind,
		CreatedAt:  at,
	}
	if err := db.Create(check).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(check).Update("created_at", at).Error; err != nil {
		t.Fatal(err)
	}
	row := &domain.SkinAnalysis{
		SkinCheckID:  check.ID,
		Status:       domain.AnalysisStatusCompleted,
		SummaryNotes: summary,
		SkinScores:   []byte(scores),
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
}

func getCoachAudit(t *testing.T, app *fiber.App, auth, rawQuery string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/coach-copy-audit?"+rawQuery, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, body
}
