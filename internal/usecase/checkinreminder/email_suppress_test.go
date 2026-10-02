package checkinreminder

import (
	"testing"
	"time"
)

func TestApplyReminderEmailRejection(t *testing.T) {
	now := time.Date(2026, 9, 25, 1, 0, 0, 0, time.UTC)
	hash := "abc"

	next, newly := applyReminderEmailRejection(reminderEmailState{}, hash, true, now)
	if !newly || next.SuppressedAt == nil || next.FailCount != 1 || next.AddressHash != hash {
		t.Fatalf("permanent: newly=%v state=%+v", newly, next)
	}
	again, newly := applyReminderEmailRejection(next, hash, true, now.Add(time.Hour))
	if newly || again.SuppressedAt == nil || !again.SuppressedAt.Equal(*next.SuppressedAt) {
		t.Fatalf("already suppressed: newly=%v state=%+v", newly, again)
	}

	var soft reminderEmailState
	for i := 1; i < reminderEmailSuppressAfter; i++ {
		var marked bool
		soft, marked = applyReminderEmailRejection(soft, hash, false, now)
		if marked || soft.SuppressedAt != nil || soft.FailCount != i {
			t.Fatalf("rejection %d: marked=%v state=%+v", i, marked, soft)
		}
	}
	soft, newly = applyReminderEmailRejection(soft, hash, false, now)
	if !newly || soft.SuppressedAt == nil || soft.FailCount != reminderEmailSuppressAfter {
		t.Fatalf("threshold: newly=%v state=%+v", newly, soft)
	}

	changed, newly := applyReminderEmailRejection(soft, "other", true, now)
	if !newly || changed.AddressHash != "other" || changed.FailCount != 1 {
		t.Fatalf("new address should start over: newly=%v state=%+v", newly, changed)
	}
	if reminderEmailBlocked(soft, "other") {
		t.Fatal("old suppression must not block a new address")
	}
	if !reminderEmailBlocked(soft, hash) {
		t.Fatal("same address stays blocked")
	}
}

func TestReminderSenderWide(t *testing.T) {
	if reminderSenderWide(1, 1) || reminderSenderWide(2, 4) || reminderSenderWide(3, 10) {
		t.Fatal("small or minority failures are still per recipient")
	}
	if !reminderSenderWide(4, 10) || !reminderSenderWide(2, 2) || !reminderSenderWide(3, 5) {
		t.Fatal("more than 3 users, or a majority of the run, is a sender error")
	}
}
