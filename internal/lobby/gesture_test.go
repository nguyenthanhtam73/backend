package lobby

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRoomGestureWhitelistAndRate(t *testing.T) {
	h, clock := testHub(t)
	code := "AB23CD"
	gate := &memberGate{}
	user := uuid.New()
	other := uuid.New()
	gate.allow(code, user, other)
	h.SetRoomGate(gate)
	peer := &memPeer{}
	otherPeer := &memPeer{}
	only(t, h.Handle(peer, user, roomJoinRaw("Mây", code)))
	only(t, h.Handle(otherPeer, other, roomJoinRaw("Lan", code)))

	clock.Advance(time.Second)
	if len(h.Handle(peer, user, roomRaw(map[string]any{"op": "cheer"})).Self) != 0 {
		t.Fatal("cheer should not reply to the sender")
	}
	clock.Advance(500 * time.Millisecond)
	limited := only(t, h.Handle(peer, user, roomRaw(map[string]any{"op": "hug"})))
	if !strings.Contains(string(limited), `"code":"rate_limited"`) {
		t.Fatalf("second gesture %s", limited)
	}

	peer.drain()
	otherPeer.drain()
	h.Flush()
	tick := string(otherPeer.last())
	if !strings.Contains(tick, `"op":"cheer"`) || strings.Contains(tick, `"op":"hug"`) {
		t.Fatalf("tick %s", tick)
	}
	if strings.Contains(tick, user.String()) {
		t.Fatal("gesture tick leaked the account id")
	}

	clock.Advance(2 * time.Second)
	bad := only(t, h.Handle(peer, user, []byte(`{"v":1,"channel":"room","op":"hug","text":"xin chào"}`)))
	if !strings.Contains(string(bad), `"code":"bad_message"`) {
		t.Fatalf("free text %s", bad)
	}
	dance := only(t, h.Handle(peer, user, roomRaw(map[string]any{"op": "dance"})))
	if !strings.Contains(string(dance), `"code":"bad_message"`) {
		t.Fatalf("unknown gesture %s", dance)
	}

	// 19 more accepted hugs, 2s apart, then the next one in the same minute fails.
	accepted := 1 // the cheer above
	for i := 0; i < 19; i++ {
		clock.Advance(2 * time.Second)
		out := h.Handle(peer, user, roomRaw(map[string]any{"op": "hug"}))
		if len(out.Self) != 0 {
			t.Fatalf("hug %d replied %s", i, out.Self[0])
		}
		accepted++
	}
	if accepted != GesturePerMinute {
		t.Fatalf("accepted %d", accepted)
	}
	clock.Advance(2 * time.Second)
	over := only(t, h.Handle(peer, user, roomRaw(map[string]any{"op": "wave"})))
	if !strings.Contains(string(over), `"code":"rate_limited"`) {
		t.Fatalf("minute cap %s", over)
	}

	msg := "Lan đang chờ bạn chăm da hôm nay đó 💕"
	h.NotifyNudge(code, user, other, msg)
	peer.drain()
	otherPeer.drain()
	h.Flush()
	nudge := string(otherPeer.last())
	if !strings.Contains(nudge, `"op":"nudge"`) || !strings.Contains(nudge, msg) {
		t.Fatalf("nudge tick %s", nudge)
	}
	if strings.Contains(nudge, user.String()) || strings.Contains(nudge, other.String()) {
		t.Fatal("nudge tick leaked an account id")
	}
}
