package checkinreminder

import (
	"time"

	"github.com/dadiary/backend/internal/domain"
)

// reminderEmailSuppressAfter is how many non-permanent rejections (other 4xx)
// mark an address undeliverable. HTTP 400/422 suppress on the first one.
const reminderEmailSuppressAfter = 3

type reminderEmailState struct {
	FailCount    int
	AddressHash  string
	SuppressedAt *time.Time
}

func reminderStateFromUser(u *domain.User) reminderEmailState {
	if u == nil {
		return reminderEmailState{}
	}
	return reminderEmailState{
		FailCount:    u.EmailReminderFailCount,
		AddressHash:  u.EmailReminderHash,
		SuppressedAt: u.EmailReminderSuppressedAt,
	}
}

func reminderEmailBlocked(state reminderEmailState, addressHash string) bool {
	return state.SuppressedAt != nil && addressHash != "" && state.AddressHash == addressHash
}

// applyReminderEmailRejection advances the counter for this address.
// A different address starts again at zero, so a later email change is eligible.
// newlySuppressed is true only on the transition into the suppressed state.
func applyReminderEmailRejection(
	prev reminderEmailState,
	addressHash string,
	permanent bool,
	now time.Time,
) (reminderEmailState, bool) {
	if addressHash == "" {
		return prev, false
	}
	if prev.AddressHash != addressHash {
		prev = reminderEmailState{AddressHash: addressHash}
	}
	if prev.SuppressedAt != nil {
		return prev, false
	}
	prev.FailCount++
	if permanent || prev.FailCount >= reminderEmailSuppressAfter {
		t := now.UTC()
		prev.SuppressedAt = &t
		return prev, true
	}
	return prev, false
}
