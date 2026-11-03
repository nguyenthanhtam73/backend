package lobby

import (
	"crypto/rand"
	"strings"
)

// NormalizeRoomCode returns the canonical invite code.
// Letters are uppercased. 0, O, 1, and I are rejected, as are any other
// characters outside RoomCodeAlphabet.
func NormalizeRoomCode(raw string) (string, bool) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if len(code) != RoomCodeLen {
		return "", false
	}
	for _, r := range code {
		if !strings.ContainsRune(RoomCodeAlphabet, r) {
			return "", false
		}
	}
	return code, true
}

// NewRoomCode draws a 6-character invite code from RoomCodeAlphabet.
func NewRoomCode() (string, error) {
	raw := make([]byte, RoomCodeLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	out := make([]byte, RoomCodeLen)
	for i, b := range raw {
		out[i] = RoomCodeAlphabet[int(b)%len(RoomCodeAlphabet)]
	}
	return string(out), nil
}

// RoomInvite returns the public phòng URL for a canonical code.
func RoomInvite(code string) string {
	return strings.ReplaceAll(RoomInviteURL, "{code}", code)
}
