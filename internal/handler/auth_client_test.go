package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/token"
	authuc "github.com/dadiary/backend/internal/usecase/auth"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const handlerAuthSecret = "handler-auth-client-secret-32b-min"

func newClientAuthApp(t *testing.T) *fiber.App {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:auth_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.RefreshSession{}); err != nil {
		t.Fatal(err)
	}
	tok, err := token.NewService(config.JWTConfig{
		Secret:        handlerAuthSecret,
		AccessTTL:     24 * time.Hour,
		RefreshTTL:    7 * 24 * time.Hour,
		AppRefreshTTL: 90 * 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	uc := authuc.NewUsecase(repository.NewAuthRepository(db), tok)
	uc.AttachSessions(repository.NewRefreshSessionRepository(db))
	app := fiber.New()
	NewAuthHandler(uc, &config.Config{}).RegisterRoutes(app, middleware.RequireAccessJWT(tok))
	return app
}

func doJSON(t *testing.T, app *fiber.App, method, path, body, auth string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("status %d body %s: %v", res.StatusCode, raw, err)
	}
	return res.StatusCode, payload
}

func tokensFrom(t *testing.T, payload map[string]any) (access, refresh string) {
	t.Helper()
	data, _ := payload["data"].(map[string]any)
	tokens, _ := data["tokens"].(map[string]any)
	access, _ = tokens["access_token"].(string)
	refresh, _ = tokens["refresh_token"].(string)
	if access == "" || refresh == "" {
		t.Fatalf("missing tokens: %+v", payload)
	}
	return access, refresh
}

func jwtExpiry(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := jwt.ParseWithClaims(raw, &jwt.RegisteredClaims{}, func(tok *jwt.Token) (any, error) {
		return []byte(handlerAuthSecret), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	claims := parsed.Claims.(*jwt.RegisteredClaims)
	return claims.ExpiresAt.Time.UTC()
}

func TestLoginClientMarkerSetsRefreshTTL(t *testing.T) {
	app := newClientAuthApp(t)
	before := time.Now().UTC()

	status, reg := doJSON(t, app, http.MethodPost, "/auth/register",
		`{"email":"phone@example.com","password":"password1","client":"ignored"}`, "",
		map[string]string{authuc.ClientHeader: "android"})
	if status != http.StatusCreated {
		t.Fatalf("register status %d body %+v", status, reg)
	}
	_, androidRefresh := tokensFrom(t, reg)
	androidExp := jwtExpiry(t, androidRefresh)
	if d := androidExp.Sub(before.Add(90 * 24 * time.Hour)); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("android exp %s", androidExp)
	}

	status, webReg := doJSON(t, app, http.MethodPost, "/auth/register",
		`{"email":"browser@example.com","password":"password1","client":"android"}`, "",
		map[string]string{authuc.ClientHeader: "web"})
	if status != http.StatusCreated {
		t.Fatalf("web register status %d body %+v", status, webReg)
	}
	_, webRefresh := tokensFrom(t, webReg)
	webExp := jwtExpiry(t, webRefresh)
	if d := webExp.Sub(before.Add(7 * 24 * time.Hour)); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("header web should beat body android, exp %s", webExp)
	}

	status, bodyLogin := doJSON(t, app, http.MethodPost, "/auth/login",
		`{"email":"phone@example.com","password":"password1","client":"android"}`, "", nil)
	if status != http.StatusOK {
		t.Fatalf("body login status %d %+v", status, bodyLogin)
	}
	_, bodyRefresh := tokensFrom(t, bodyLogin)
	bodyExp := jwtExpiry(t, bodyRefresh)
	if d := bodyExp.Sub(before.Add(90 * 24 * time.Hour)); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("body android exp %s", bodyExp)
	}

	access, _ := tokensFrom(t, bodyLogin)
	status, loggedOut := doJSON(t, app, http.MethodPost, "/auth/logout",
		`{"refresh_token":"`+bodyRefresh+`"}`, access, nil)
	if status != http.StatusOK {
		t.Fatalf("logout status %d %+v", status, loggedOut)
	}
	data, _ := loggedOut["data"].(map[string]any)
	if data["logged_out"] != true {
		t.Fatalf("logout data %+v", loggedOut)
	}

	// The register-time android session is a different refresh token and still works.
	status, refreshed := doJSON(t, app, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+androidRefresh+`"}`, "", nil)
	if status != http.StatusOK {
		t.Fatalf("other android session should refresh, status %d %+v", status, refreshed)
	}
	_, rotated := tokensFrom(t, refreshed)
	rotatedExp := jwtExpiry(t, rotated)
	if d := rotatedExp.Sub(time.Now().UTC().Add(90 * 24 * time.Hour)); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("rotated android exp %s", rotatedExp)
	}

	access2, _ := tokensFrom(t, refreshed)
	status, all := doJSON(t, app, http.MethodPost, "/auth/logout-all", `{}`, access2, nil)
	if status != http.StatusOK {
		t.Fatalf("logout-all status %d %+v", status, all)
	}
	allData, _ := all["data"].(map[string]any)
	if allData["logged_out"] != true {
		t.Fatalf("logout-all data %+v", all)
	}
	status, again := doJSON(t, app, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+rotated+`"}`, "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("refresh after logout-all status %d %+v", status, again)
	}
	// logout-all is per user. The browser account is a different session and still refreshes.
	status, webAgain := doJSON(t, app, http.MethodPost, "/auth/refresh",
		`{"refresh_token":"`+webRefresh+`"}`, "", nil)
	if status != http.StatusOK {
		t.Fatalf("other user's web session should still refresh, status %d %+v", status, webAgain)
	}
}
