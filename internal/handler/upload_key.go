package handler

import (
	"context"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/storage"
	"github.com/google/uuid"
)

// userNamer looks up a username for R2 folder labels. Optional — missing
// lookup falls back to a short userID slug inside storage.PhotoKey.
type userNamer interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
}

func lookupUsername(ctx context.Context, users userNamer, userID uuid.UUID) string {
	if users == nil || userID == uuid.Nil {
		return ""
	}
	u, err := users.GetByID(ctx, userID)
	if err != nil || u == nil {
		return ""
	}
	return u.Username
}

func uploadPhotoKey(ctx context.Context, users userNamer, userID uuid.UUID, kind, ext string) string {
	return storage.PhotoKey(userID, lookupUsername(ctx, users, userID), kind, ext, time.Now())
}
