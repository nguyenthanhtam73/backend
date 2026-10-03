package userdata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/storage"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ErrInvalidPassword is a wrong or empty account password.
var ErrInvalidPassword = errors.New("invalid password")

// DeleteAccount checks the password, then removes the account.
// Stored photo files are deleted only after the database transaction commits.
// A missing file is ignored. An active Premium subscription is removed with
// the account and is not refunded.
// Accounts that never set a local password (Google / Apple only) cannot
// confirm this step: there is no hash to compare.
func (s *Service) DeleteAccount(ctx context.Context, userID uuid.UUID, password string) error {
	if s == nil || s.repo == nil {
		return ErrUnavailable
	}
	if userID == uuid.Nil {
		return ErrInvalidUser
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return ErrInvalidPassword
	}
	user, err := s.repo.FindUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("load account: %w", err)
	}
	if user == nil {
		return ErrInvalidUser
	}
	if user.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return ErrInvalidPassword
	}

	keys, err := s.repo.DeleteAccount(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrAccountNotFound) {
			return ErrInvalidUser
		}
		return fmt.Errorf("delete account: %w", err)
	}
	s.removeStoredPhotos(ctx, userID, keys)
	if s.cache != nil {
		s.cache.Bust(userID)
	}
	slog.Info("account deleted", "user_id_hash", userIDHash(userID))
	return nil
}

func (s *Service) removeStoredPhotos(ctx context.Context, userID uuid.UUID, keys []string) {
	if s == nil || s.store == nil || userID == uuid.Nil {
		return
	}
	hash := userIDHash(userID)
	seen := map[string]struct{}{}
	deleteKey := func(key string) {
		key = storage.CleanKey(key)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		if err := s.store.DeletePrefix(ctx, key); err != nil {
			slog.Warn("account: delete stored photo failed", "user_id_hash", hash, "err", err)
		}
	}
	for _, key := range keys {
		deleteKey(key)
	}
	// Legacy layout "{userID}/..." is not always listed on a row.
	deleteKey(userID.String() + "/")
}

func userIDHash(id uuid.UUID) string {
	sum := sha256.Sum256([]byte(id.String()))
	return hex.EncodeToString(sum[:])
}
