// Package utils holds small shared helpers (IDs, time, strings).
package utils

import "github.com/google/uuid"

// NewUUID returns a new random UUID v4.
func NewUUID() uuid.UUID {
	return uuid.New()
}
