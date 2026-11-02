package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	authuc "github.com/dadiary/backend/internal/usecase/auth"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// registerFailRepo fails if registration proceeds past email validation.
type registerFailRepo struct{}

func (registerFailRepo) Create(context.Context, *domain.User) error {
	return errors.New("create should not be called")
}

func (registerFailRepo) GetByEmail(context.Context, string) (*domain.User, error) {
	return nil, errors.New("lookup should not be called")
}

func (registerFailRepo) GetByID(context.Context, uuid.UUID) (*domain.User, error) {
	return nil, errors.New("lookup should not be called")
}

func (registerFailRepo) UsernameExists(context.Context, string) (bool, error) {
	return false, errors.New("username lookup should not be called")
}

type registerFailTokens struct{}

func (registerFailTokens) SignAccess(uuid.UUID) (string, error) {
	return "", errors.New("token should not be issued")
}

func (registerFailTokens) SignRefresh(uuid.UUID) (string, uuid.UUID, error) {
	return "", uuid.Nil, errors.New("token should not be issued")
}

func (registerFailTokens) SignRefreshWithTTL(uuid.UUID, time.Duration) (string, uuid.UUID, time.Time, error) {
	return "", uuid.Nil, time.Time{}, errors.New("token should not be issued")
}

func (registerFailTokens) ParseRefreshToken(string) (uuid.UUID, uuid.UUID, error) {
	return uuid.Nil, uuid.Nil, errors.New("token should not be parsed")
}

func (registerFailTokens) AccessTTL() time.Duration     { return time.Hour }
func (registerFailTokens) RefreshTTL() time.Duration    { return 24 * time.Hour }
func (registerFailTokens) AppRefreshTTL() time.Duration { return 2160 * time.Hour }

func TestRegisterHandler_RejectsInvalidEmail(t *testing.T) {
	uc := authuc.NewUsecase(registerFailRepo{}, registerFailTokens{})
	app := fiber.New()
	NewAuthHandler(uc, nil).RegisterRoutes(app, nil)

	cases := []string{
		`{"email":"abc@1995","password":"password1"}`,
		`{"email":"danghaiduong@1995","password":"password1"}`,
		`{"email":"abc","password":"password1"}`,
		`{"email":"abc@gmail","password":"password1"}`,
		`{"email":"  ABC@1995  ","password":"password1"}`,
	}
	for _, body := range cases {
		req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, want 400, payload %s", body, res.StatusCode, raw)
		}
		var env struct {
			Success bool `json:"success"`
			Error   struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		if env.Success {
			t.Fatalf("body %s success=true, payload %s", body, raw)
		}
		if env.Error.Code != authuc.InvalidEmailCode || env.Error.Message != authuc.InvalidEmailMessage {
			t.Fatalf("body %s error = %+v", body, env.Error)
		}
	}
}

func TestRegisterHandler_RejectsStrangeInput(t *testing.T) {
	uc := authuc.NewUsecase(registerFailRepo{}, registerFailTokens{})
	app := fiber.New()
	NewAuthHandler(uc, nil).RegisterRoutes(app, nil)

	const wantMessage = "Email hoặc tên có ký tự lạ, bạn kiểm tra lại giúp nhé"
	cases := []string{
		`{"email":"ngọc@gmail.com","password":"password1"}`,
		`{"email":"ten @gmail.com","password":"password1"}`,
		`{"email":"  ten@gmail.com  ","password":"password1","username":"abc\u0000"}`,
		`{"email":"ab\u0000c@gmail.com","password":"password1"}`,
		`{"email":"ten@gmail.com","password":"password1","display_name":"Ngọc\u0000"}`,
	}
	for _, body := range cases {
		req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		res, err := app.Test(req, -1)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, want 400, payload %s", body, res.StatusCode, raw)
		}
		var env struct {
			Success bool `json:"success"`
			Error   struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		if env.Success || env.Error.Code != "invalid_input" || env.Error.Message != wantMessage {
			t.Fatalf("body %s error = %+v payload %s", body, env.Error, raw)
		}
	}
}

func TestRegisterHandler_DatabaseErrorIs503WithoutEmail(t *testing.T) {
	const (
		email    = "hoa.lan@example.com"
		username = "hoa_lan"
		password = "password1"
	)
	driverErr := errors.New("pq: invalid byte sequence for encoding UTF8 email=" + email + " username=" + username + " password=" + password)

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	uc := authuc.NewUsecase(registerDBErrRepo{err: driverErr}, registerFailTokens{})
	app := fiber.New()
	NewAuthHandler(uc, nil).RegisterRoutes(app, nil)

	body := `{"email":"` + email + `","password":"` + password + `","username":"` + username + `"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, payload %s", res.StatusCode, raw)
	}
	var env struct {
		Success bool `json:"success"`
		Error   struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	if env.Success || env.Error.Code != "database_unavailable" {
		t.Fatalf("error = %+v", env.Error)
	}
	for _, secret := range []string{email, username, password, "invalid byte sequence"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("response contains %q: %s", secret, raw)
		}
	}
	logText := logs.String()
	if !strings.Contains(logText, "get_by_email") || !strings.Contains(logText, "invalid byte sequence") {
		t.Fatalf("log = %s", logText)
	}
	for _, secret := range []string{email, username, password} {
		if strings.Contains(logText, secret) {
			t.Fatalf("log contains %q: %s", secret, logText)
		}
	}
}

// registerDBErrRepo fails the email lookup and allows the username probe.
type registerDBErrRepo struct {
	err error
}

func (registerDBErrRepo) Create(context.Context, *domain.User) error {
	return errors.New("create should not be called")
}

func (r registerDBErrRepo) GetByEmail(context.Context, string) (*domain.User, error) {
	return nil, r.err
}

func (registerDBErrRepo) GetByID(context.Context, uuid.UUID) (*domain.User, error) {
	return nil, errors.New("lookup should not be called")
}

func (registerDBErrRepo) UsernameExists(context.Context, string) (bool, error) {
	return false, nil
}
