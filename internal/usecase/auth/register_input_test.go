package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
)

// explodeRepo fails the test if registration reaches a query.
type explodeRepo struct{}

func (explodeRepo) Create(context.Context, *domain.User) error {
	return errors.New("create should not be called")
}

func (explodeRepo) GetByEmail(context.Context, string) (*domain.User, error) {
	return nil, errors.New("lookup should not be called")
}

func (explodeRepo) GetByID(context.Context, uuid.UUID) (*domain.User, error) {
	return nil, errors.New("lookup should not be called")
}

func (explodeRepo) UsernameExists(context.Context, string) (bool, error) {
	return false, errors.New("username lookup should not be called")
}

func TestRegister_RejectsStrangeInputBeforeDatabase(t *testing.T) {
	uc := NewUsecase(explodeRepo{}, &stubTokens{})
	cases := []struct {
		name string
		req  dto.RegisterRequest
	}{
		{name: "vietnamese email", req: dto.RegisterRequest{Email: "ngọc@gmail.com", Password: "password1"}},
		{name: "trimmed vietnamese email", req: dto.RegisterRequest{Email: "  NGỌC@gmail.com  ", Password: "password1"}},
		{name: "space inside email", req: dto.RegisterRequest{Email: "ten @gmail.com", Password: "password1"}},
		{name: "nul email", req: dto.RegisterRequest{Email: "a\x00b@gmail.com", Password: "password1"}},
		{name: "invalid utf-8 email", req: dto.RegisterRequest{Email: "a\xffb@gmail.com", Password: "password1"}},
		{name: "nul username", req: dto.RegisterRequest{Email: "ten@gmail.com", Password: "password1", Username: "abc\x00"}},
		{name: "invalid utf-8 username", req: dto.RegisterRequest{Email: "ten@gmail.com", Password: "password1", Username: "abc\xffzz"}},
		{name: "nul display name", req: dto.RegisterRequest{Email: "ten@gmail.com", Password: "password1", DisplayName: "Ngọc\x00"}},
		{name: "control display name", req: dto.RegisterRequest{Email: "ten@gmail.com", Password: "password1", DisplayName: "Ngọc\nLan"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := uc.Register(context.Background(), tc.req)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			ae, ok := domain.AsAppError(err)
			if !ok {
				t.Fatal("error is not AppError")
			}
			if ae.HTTPStatus != 400 || ae.Code != "invalid_input" || ae.Message != StrangeInputMessage {
				t.Fatalf("app error = status %d code %q message %q", ae.HTTPStatus, ae.Code, ae.Message)
			}
		})
	}
}

func TestRegister_AllowsVietnameseDisplayName(t *testing.T) {
	repo := newMemAuthRepo()
	uc := NewUsecase(repo, &stubTokens{})

	res, err := uc.Register(context.Background(), dto.RegisterRequest{
		Email:       "  Lan@Example.com  ",
		Password:    "password1",
		Username:    "Ngọc_Lan",
		DisplayName: "Ngọc Lan",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.User.Email != "lan@example.com" {
		t.Fatalf("email = %q", res.User.Email)
	}
	if res.User.Username != "Ngọc_Lan" || res.User.DisplayName != "Ngọc Lan" {
		t.Fatalf("user = %+v", res.User)
	}
}

func TestRegister_DatabaseErrorIsLoggedWithoutRawEmail(t *testing.T) {
	const (
		email    = "hoa.lan@example.com"
		username = "hoa_lan"
		password = "password1"
	)
	driverErr := fmt.Errorf("pq: invalid byte sequence for encoding UTF8 (email=%s username=%s password=%s)", email, username, password)

	cases := []struct {
		name string
		repo AuthRepository
		op   string
	}{
		{
			name: "get by email",
			op:   "get_by_email",
			repo: emailLookupFailRepo{memAuthRepo: newMemAuthRepo(), err: driverErr},
		},
		{
			name: "ensure unique username",
			op:   "ensure_unique_username",
			repo: usernameLookupFailRepo{memAuthRepo: newMemAuthRepo(), err: driverErr},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })

			uc := NewUsecase(tc.repo, &stubTokens{})
			_, err := uc.Register(context.Background(), dto.RegisterRequest{
				Email:    email,
				Password: password,
				Username: username,
			})
			if !errors.Is(err, ErrDatabase) {
				t.Fatalf("err = %v, want ErrDatabase", err)
			}
			ae, ok := domain.AsAppError(err)
			if !ok || ae.HTTPStatus != 503 || ae.Code != "database_unavailable" {
				t.Fatalf("app error = %+v", ae)
			}
			if ae.Message == driverErr.Error() || ae.Error() == driverErr.Error() {
				t.Fatalf("database error leaked to caller: %q", ae.Error())
			}

			var entry map[string]any
			if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
				t.Fatalf("log %q: %v", buf.String(), err)
			}
			if entry["msg"] != "auth register database error" || entry["op"] != tc.op {
				t.Fatalf("log entry = %+v", entry)
			}
			logged, _ := entry["err"].(string)
			if logged == "" || !bytes.Contains([]byte(logged), []byte("invalid byte sequence")) {
				t.Fatalf("logged err = %q", logged)
			}
			if entry["email_hash"] != emailHashPrefix(email) || len(emailHashPrefix(email)) != 8 {
				t.Fatalf("email_hash = %v", entry["email_hash"])
			}
			logText := buf.String()
			for _, secret := range []string{email, username, password} {
				if bytes.Contains(buf.Bytes(), []byte(secret)) {
					t.Fatalf("log contains %q: %s", secret, logText)
				}
			}
			for _, key := range []string{"email", "username", "password", "ip"} {
				if _, ok := entry[key]; ok {
					t.Fatalf("log field %q present: %s", key, logText)
				}
			}
		})
	}
}

func TestAppDatabase_DoesNotLogPlainCause(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	err := appDatabase(errors.New("pq: secret@example.com"))
	if !errors.Is(err, ErrDatabase) {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("plain database error was logged: %s", buf.String())
	}
}

type emailLookupFailRepo struct {
	*memAuthRepo
	err error
}

func (r emailLookupFailRepo) GetByEmail(context.Context, string) (*domain.User, error) {
	return nil, r.err
}

type usernameLookupFailRepo struct {
	*memAuthRepo
	err error
}

func (r usernameLookupFailRepo) UsernameExists(context.Context, string) (bool, error) {
	return false, r.err
}
