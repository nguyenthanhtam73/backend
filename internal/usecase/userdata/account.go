package userdata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/storage"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	photoDeleteAttempts = 3
	photoDeleteTimeout  = 20 * time.Second
)

// photoDeleteDelays are the waits before attempt 2 and attempt 3.
// Tests replace this so retries do not sleep.
var photoDeleteDelays = []time.Duration{200 * time.Millisecond, time.Second}

// ErrInvalidPassword is a wrong or empty account password.
var ErrInvalidPassword = errors.New("invalid password")

// DeleteAccount checks the password, then removes the account.
// Stored photo files are deleted after the database transaction commits,
// on a background context. The caller does not wait for object storage,
// and a storage failure does not fail the delete. A missing file is ignored.
// An active Premium subscription is removed with the account and is not refunded.
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
		if errors.Is(err, repository.ErrAccountDeletionSchema) {
			return ErrSchemaNotReady
		}
		return fmt.Errorf("delete account: %w", err)
	}
	// Object storage is independent of the request. A closed client must not
	// leave the keys behind, and the 204 must not wait on the deletes.
	s.enqueuePhotoDelete(userID, keys)
	if s.cache != nil {
		s.cache.Bust(userID)
	}
	slog.Info("account deleted", "user_id_hash", userIDHash(userID))
	return nil
}

// onPhotosDone is set by tests to observe the background delete.
func (s *Service) enqueuePhotoDelete(userID uuid.UUID, keys []string) {
	if s == nil || s.store == nil || userID == uuid.Nil {
		if s != nil && s.onPhotosDone != nil {
			s.onPhotosDone()
		}
		return
	}
	go s.removeStoredPhotos(userID, keys)
}

func (s *Service) removeStoredPhotos(userID uuid.UUID, keys []string) {
	defer func() {
		if s.onPhotosDone != nil {
			s.onPhotosDone()
		}
	}()
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("account photo delete panic", "recover", fmt.Sprint(rec), "user_id_hash", userIDHash(userID))
		}
	}()
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
		if err := s.deletePhotoKey(key); err != nil {
			s.recordOrphanPhoto(hash, key, err)
		}
	}
	for _, key := range keys {
		deleteKey(key)
	}
	// Legacy layout "{userID}/..." is not always listed on a row.
	deleteKey(userID.String() + "/")
}

func (s *Service) deletePhotoKey(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), photoDeleteTimeout)
	defer cancel()
	var err error
	for attempt := 0; attempt < photoDeleteAttempts; attempt++ {
		if attempt > 0 {
			delay := time.Duration(0)
			if attempt-1 < len(photoDeleteDelays) {
				delay = photoDeleteDelays[attempt-1]
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				if err == nil {
					err = ctx.Err()
				}
				return err
			case <-timer.C:
			}
		}
		err = s.store.DeletePrefix(ctx, key)
		if err == nil {
			return nil
		}
	}
	return err
}

func (s *Service) recordOrphanPhoto(hash, key string, cause error) {
	// Prefix is stable so a log search can find keys that still need cleanup.
	slog.Warn("account_delete_orphan_key="+key,
		"account_delete_orphan_key", key,
		"user_id_hash", hash,
		"error", cause.Error(),
	)
	if s.repo == nil {
		return
	}
	pctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.repo.SaveAccountDeleteOrphanKey(pctx, hash, key, cause.Error()); err != nil {
		slog.Warn("account_delete_orphan_key="+key+" persist failed",
			"account_delete_orphan_key", key,
			"user_id_hash", hash,
			"error", err.Error(),
		)
	}
}

func userIDHash(id uuid.UUID) string {
	sum := sha256.Sum256([]byte(id.String()))
	return hex.EncodeToString(sum[:])
}
