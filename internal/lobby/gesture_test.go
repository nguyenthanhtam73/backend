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
	limited := only(t, h.Handle(peer, user, roomRaw(map[string]any{"op": "poke"})))
	if !strings.Contains(string(limited), `"code":"rate_limited"`) {
		t.Fatalf("second gesture %s", limited)
	}

	peer.drain()
	otherPeer.drain()
	h.Flush()
	tick := string(otherPeer.last())
	if !strings.Contains(tick, `"op":"cheer"`) || strings.Contains(tick, `"op":"poke"`) {
		t.Fatalf("tick %s", tick)
	}
	if strings.Contains(tick, user.String()) {
		t.Fatal("gesture tick leaked the account id")
	}

	clock.Advance(2 * time.Second)
	bad := only(t, h.Handle(peer, user, []byte(`{"v":1,"channel":"room","op":"poke","text":"xin chào"}`)))
	if !strings.Contains(string(bad), `"code":"bad_message"`) {
		t.Fatalf("free text %s", bad)
	}
	dance := only(t, h.Handle(peer, user, roomRaw(map[string]any{"op": "dance"})))
	if !strings.Contains(string(dance), `"code":"bad_message"`) {
		t.Fatalf("unknown gesture %s", dance)
	}

	// 19 more accepted pokes, 2s apart, then the next one in the same minute fails.
	accepted := 1 // the cheer above
	for i := 0; i < 19; i++ {
		clock.Advance(2 * time.Second)
		out := h.Handle(peer, user, roomRaw(map[string]any{"op": "poke"}))
		if len(out.Self) != 0 {
			t.Fatalf("poke %d replied %s", i, out.Self[0])
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

func TestNotifyNudgeSkipsOfflineTarget(t *testing.T) {
	h, _ := testHub(t)
	code := "AB23CD"
	gate := &memberGate{}
	sender := uuid.New()
	offline := uuid.New()
	gate.allow(code, sender, offline)
	h.SetRoomGate(gate)
	peer := &memPeer{}
	only(t, h.Handle(peer, sender, roomJoinRaw("Mây", code)))
	h.Flush()
	peer.drain()

	h.NotifyNudge(code, sender, offline, "Lan đang chờ bạn chăm da hôm nay đó 💕")
	h.Flush()
	if strings.Contains(string(peer.last()), `"op":"nudge"`) {
		t.Fatalf("offline nudge reached the room %s", peer.last())
	}

	target := &memPeer{}
	only(t, h.Handle(target, offline, roomJoinRaw("Lan", code)))
	h.Flush()
	peer.drain()
	target.drain()
	h.NotifyNudge(code, sender, offline, "Lan đang chờ bạn chăm da hôm nay đó 💕")
	h.Flush()
	got := string(target.last())
	if !strings.Contains(got, `"op":"nudge"`) || !strings.Contains(got, `"target":`) {
		t.Fatalf("online nudge %s", got)
	}
}

func TestGestureBudgetEvictsStale(t *testing.T) {
	h, clock := testHub(t)
	code := "AB23CD"
	gate := &memberGate{}
	first := uuid.New()
	second := uuid.New()
	gate.allow(code, first, second)
	h.SetRoomGate(gate)
	a := &memPeer{}
	b := &memPeer{}
	only(t, h.Handle(a, first, roomJoinRaw("Mây", code)))
	only(t, h.Handle(b, second, roomJoinRaw("Lan", code)))
	clock.Advance(time.Second)
	if len(h.Handle(a, first, roomRaw(map[string]any{"op": "cheer"})).Self) != 0 {
		t.Fatal("cheer replied")
	}
	if len(h.gestures) != 1 {
		t.Fatalf("budgets %d", len(h.gestures))
	}
	clock.Advance(2 * time.Minute)
	if len(h.Handle(b, second, roomRaw(map[string]any{"op": "poke"})).Self) != 0 {
		t.Fatal("poke replied")
	}
	if _, ok := h.gestures[first]; ok {
		t.Fatal("stale gesture budget kept")
	}
	if _, ok := h.gestures[second]; !ok || len(h.gestures) != 1 {
		t.Fatalf("budgets %#v", h.gestures)
	}
}
