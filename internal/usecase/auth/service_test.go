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

type memSessions struct {
	byID map[uuid.UUID]*domain.RefreshSession
}

func newMemSessions() *memSessions {
	return &memSessions{byID: map[uuid.UUID]*domain.RefreshSession{}}
}

func (m *memSessions) Create(_ context.Context, session *domain.RefreshSession) error {
	cp := *session
	m.byID[session.ID] = &cp
	return nil
}

func (m *memSessions) GetByID(_ context.Context, id uuid.UUID) (*domain.RefreshSession, error) {
	s := m.byID[id]
	if s == nil {
		return nil, nil
	}
	cp := *s
	return &cp, nil
}

func (m *memSessions) RevokeByID(_ context.Context, id uuid.UUID, at time.Time) error {
	if s := m.byID[id]; s != nil && s.RevokedAt == nil {
		t := at.UTC()
		s.RevokedAt = &t
	}
	return nil
}

func (m *memSessions) RevokeAllForUser(_ context.Context, userID uuid.UUID, at time.Time) error {
	t := at.UTC()
	for _, s := range m.byID {
		if s.UserID == userID && s.RevokedAt == nil {
			s.RevokedAt = &t
		}
	}
	return nil
}

type stubTokens struct {
	refreshN int
}

func (s stubTokens) SignAccess(uuid.UUID) (string, error) { return "access", nil }
func (s *stubTokens) SignRefresh(uuid.UUID) (string, uuid.UUID, error) {
	s.refreshN++
	return "refresh-" + uuid.NewString()[:8], uuid.New(), nil
}
func (stubTokens) ParseRefreshToken(token string) (uuid.UUID, uuid.UUID, error) {
	return uuid.Nil, uuid.Nil, errors.New("not used in basic test")
}
func (stubTokens) AccessTTL() time.Duration  { return time.Hour }
func (stubTokens) RefreshTTL() time.Duration { return 24 * time.Hour }

func TestRegisterLoginMe_AppErrorSentinel(t *testing.T) {
	repo := newMemAuthRepo()
	sessions := newMemSessions()
	tok := &stubTokens{}
	uc := NewUsecase(repo, tok)
	uc.AttachSessions(sessions)

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
	if res.Tokens.RefreshToken == "" {
		t.Fatal("expected refresh token")
	}
	if len(sessions.byID) != 1 {
		t.Fatalf("expected 1 refresh session, got %d", len(sessions.byID))
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
	if err := uc.Logout(context.Background(), uuid.MustParse(login.User.ID), ""); err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions.byID {
		if s.RevokedAt == nil {
			t.Fatal("expected all refresh sessions revoked after logout")
		}
	}
}
