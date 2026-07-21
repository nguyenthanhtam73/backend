package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
)

type memAuthRepo struct {
	byEmail map[string]*domain.User
	byID    map[uuid.UUID]*domain.User
	names   map[string]bool
}

func newMemAuthRepo() *memAuthRepo {
	return &memAuthRepo{
		byEmail: map[string]*domain.User{},
		byID:    map[uuid.UUID]*domain.User{},
		names:   map[string]bool{},
	}
}

func (m *memAuthRepo) Create(_ context.Context, user *domain.User) error {
	if user.ID == uuid.Nil {
		user.ID = uuid.New()
	}
	cp := *user
	m.byEmail[user.Email] = &cp
	m.byID[user.ID] = &cp
	m.names[user.Username] = true
	return nil
}

func (m *memAuthRepo) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	u := m.byEmail[email]
	if u == nil {
		return nil, nil
	}
	cp := *u
	return &cp, nil
}

func (m *memAuthRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	u := m.byID[id]
	if u == nil {
		return nil, nil
	}
	cp := *u
	return &cp, nil
}

func (m *memAuthRepo) UsernameExists(_ context.Context, username string) (bool, error) {
	return m.names[username], nil
}

type stubTokens struct{}

func (stubTokens) SignAccess(uuid.UUID) (string, error)  { return "access", nil }
func (stubTokens) SignRefresh(uuid.UUID) (string, error) { return "refresh", nil }
func (stubTokens) AccessTTL() time.Duration              { return time.Hour }

func TestRegisterLoginMe_AppErrorSentinel(t *testing.T) {
	repo := newMemAuthRepo()
	uc := NewUsecase(repo, stubTokens{})

	res, err := uc.Register(context.Background(), dto.RegisterRequest{
		Email:    "alice@example.com",
		Password: "password1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Tokens.AccessToken == "" || res.User.Email != "alice@example.com" {
		t.Fatalf("register result: %+v", res)
	}

	_, err = uc.Register(context.Background(), dto.RegisterRequest{
		Email:    "alice@example.com",
		Password: "password1",
	})
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("want ErrEmailTaken, got %v", err)
	}
	if _, ok := domain.AsAppError(err); !ok {
		t.Fatal("expected AppError wrap")
	}

	login, err := uc.Login(context.Background(), dto.LoginRequest{
		Email:    "alice@example.com",
		Password: "password1",
	})
	if err != nil {
		t.Fatal(err)
	}
	me, err := uc.GetMe(context.Background(), uuid.MustParse(login.User.ID))
	if err != nil {
		t.Fatal(err)
	}
	if me.Email != "alice@example.com" {
		t.Fatalf("me=%+v", me)
	}
	if err := uc.Logout(context.Background(), uuid.MustParse(login.User.ID)); err != nil {
		t.Fatal(err)
	}
}
