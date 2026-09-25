package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"golang.org/x/crypto/bcrypt"
)

func TestValidAccountEmail(t *testing.T) {
	accept := []string{
		"ten@gmail.com",
		"a.b+tag@sub.domain.vn",
		"user.name+tag@example.co.uk",
	}
	for _, email := range accept {
		if !validAccountEmail(email) {
			t.Errorf("validAccountEmail(%q) = false, want true", email)
		}
	}

	// Inputs are already trimmed and lowercased, matching normalizeEmail.
	reject := []string{
		"",
		"abc",
		"abc@1995",
		"danghaiduong@1995",
		"abc@gmail",
		"abc @gmail.com",
		"ten <ten@gmail.com>",
		"<ten@gmail.com>",
		"user@gmail.c",
		"user@domain.123",
		"user@localhost",
		"@gmail.com",
		"a@",
	}
	for _, email := range reject {
		if validAccountEmail(email) {
			t.Errorf("validAccountEmail(%q) = true, want false", email)
		}
	}
}

func TestRegister_RejectsEmailWithoutDomain(t *testing.T) {
	repo := newMemAuthRepo()
	uc := NewUsecase(repo, &stubTokens{})

	cases := []string{
		"abc@1995",
		"danghaiduong@1995",
		"abc",
		"abc@gmail",
		"abc @gmail.com",
		"  ABC@1995  ",
	}
	for _, email := range cases {
		_, err := uc.Register(context.Background(), dto.RegisterRequest{
			Email:    email,
			Password: "password1",
		})
		if !errors.Is(err, ErrInvalidEmail) {
			t.Fatalf("Register(%q) err = %v, want ErrInvalidEmail", email, err)
		}
		ae, ok := domain.AsAppError(err)
		if !ok {
			t.Fatalf("Register(%q) error is not AppError", email)
		}
		if ae.HTTPStatus != 400 || ae.Code != InvalidEmailCode || ae.Message != InvalidEmailMessage {
			t.Fatalf("Register(%q) app error = status %d code %q message %q", email, ae.HTTPStatus, ae.Code, ae.Message)
		}
	}
	if len(repo.byEmail) != 0 {
		t.Fatalf("rejected emails were stored: %d", len(repo.byEmail))
	}
}

func TestRegister_NormalizesValidEmail(t *testing.T) {
	repo := newMemAuthRepo()
	uc := NewUsecase(repo, &stubTokens{})

	res, err := uc.Register(context.Background(), dto.RegisterRequest{
		Email:    "  Ten@Gmail.COM  ",
		Password: "password1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.User.Email != "ten@gmail.com" {
		t.Fatalf("stored email = %q", res.User.Email)
	}
	if _, ok := repo.byEmail["ten@gmail.com"]; !ok {
		t.Fatal("normalized email was not stored")
	}
}

func TestLogin_AllowsExistingAddressWithoutTLD(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password1"), bcryptCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := newMemAuthRepo()
	user := &domain.User{
		Email:        "danghaiduong@1995",
		Username:     "danghaiduong",
		PasswordHash: string(hash),
		Provider:     domain.AuthProviderLocal,
		IsActive:     true,
	}
	if err := repo.Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	uc := NewUsecase(repo, &stubTokens{})

	res, err := uc.Login(context.Background(), dto.LoginRequest{
		Email:    "  DangHaiDuong@1995  ",
		Password: "password1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.User.Email != "danghaiduong@1995" {
		t.Fatalf("login email = %q", res.User.Email)
	}
}
