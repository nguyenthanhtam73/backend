package lobby

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRoomCareStreakOnHelloAndTick(t *testing.T) {
	h, clock := testHub(t)
	code := "AB23CD"
	gate := &memberGate{}
	user := uuid.New()
	gate.allow(code, user)
	h.SetRoomGate(gate)
	var saved string
	h.SetRoomCare(
		func(_ context.Context, id uuid.UUID, gotCode, nick string) error {
			if id != user || gotCode != code || nick != "Mây Hồng" {
				t.Fatalf("save %s %s %q", id, gotCode, nick)
			}
			saved = nick
			return nil
		},
		func(_ context.Context, ids []uuid.UUID) map[uuid.UUID]int {
			if len(ids) != 1 || ids[0] != user {
				t.Fatalf("ids %v", ids)
			}
			return map[uuid.UUID]int{user: 4}
		},
	)
	peer := &memPeer{}
	raw := only(t, h.Handle(peer, user, roomJoinRaw("Mây Hồng", code)))
	if saved != "Mây Hồng" {
		t.Fatalf("saved %q", saved)
	}
	if !strings.Contains(string(raw), `"care_streak":4`) || strings.Contains(string(raw), user.String()) {
		t.Fatalf("hello %s", raw)
	}
	lobbyRaw := only(t, h.Handle(&memPeer{}, uuid.New(), joinRaw("Sảnh", nil)))
	if strings.Contains(string(lobbyRaw), "care_streak") {
		t.Fatalf("lobby leaked care_streak %s", lobbyRaw)
	}
	if !strings.Contains(string(mustJSON(roomPerson{})), `"care_streak":0`) {
		t.Fatal("zero streak omitted")
	}
	if strings.Contains(string(mustJSON(Person{})), "care_streak") {
		t.Fatal("lobby person gained care_streak")
	}

	clock.Advance(time.Second)
	if len(h.Handle(peer, user, roomRaw(map[string]any{"op": "move-to", "x": 0.2, "y": 0.3})).Self) != 0 {
		t.Fatal("move replied")
	}
	peer.drain()
	h.Flush()
	if !strings.Contains(string(peer.last()), `"care_streak":4`) {
		t.Fatalf("tick %s", peer.last())
	}
	h.NoteCareStreaks(code, map[uuid.UUID]int{user: 7})
	peer.drain()
	h.Flush()
	if !strings.Contains(string(peer.last()), `"care_streak":7`) {
		t.Fatalf("refresh %s", peer.last())
	}
}

func TestRoomCodeAlphabet(t *testing.T) {
	for _, bad := range []rune{'0', 'O', '1', 'I'} {
		if strings.ContainsRune(RoomCodeAlphabet, bad) {
			t.Fatalf("alphabet contains %q", string(bad))
		}
	}
	if len(RoomCodeAlphabet) < 16 {
		t.Fatalf("alphabet %s", RoomCodeAlphabet)
	}
	for i := 0; i < 40; i++ {
		code, err := NewRoomCode()
		if err != nil {
			t.Fatal(err)
		}
		got, ok := NormalizeRoomCode(code)
		if !ok || got != code {
			t.Fatalf("generated %s", code)
		}
	}
	canon, ok := NormalizeRoomCode(" ab23cd ")
	if !ok || canon != "AB23CD" {
		t.Fatalf("normalize %s %v", canon, ok)
	}
	for _, bad := range []string{"", "ABCDE", "ABCDEFG", "OOOIII", "AB23C0", "AB23CI", "AB23C1", "AB23CO"} {
		if _, ok := NormalizeRoomCode(bad); ok {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestRoomJoinOpsRateGraceAndFull(t *testing.T) {
	h, clock := testHub(t)
	code := "AB23CD"
	gate := &memberGate{}
	gate.allow(code)
	h.SetRoomGate(gate)

	outsider := &memPeer{}
	out := h.Handle(outsider, uuid.New(), roomJoinRaw("Mây Hồng", code))
	if decodeErr(t, only(t, out)).Code != CodeNotMember {
		t.Fatalf("missing member %+v", ops(t, out))
	}

	missing := h.Handle(&memPeer{}, uuid.New(), roomJoinRaw("Mây Hồng", "ZZZZZZ"))
	if decodeErr(t, only(t, missing)).Code != CodeRoomNotFound {
		t.Fatalf("missing room %+v", ops(t, missing))
	}
	for _, bad := range []string{"OOOIII", "AB23C0", "ab"} {
		got := h.Handle(&memPeer{}, uuid.New(), roomJoinRaw("Mây Hồng", bad))
		if decodeErr(t, only(t, got)).Code != CodeBadMessage {
			t.Fatalf("code %s accepted %+v", bad, ops(t, got))
		}
	}

	users := make([]uuid.UUID, RoomCap+1)
	peers := make([]*memPeer, RoomCap+1)
	for i := 0; i < RoomCap+1; i++ {
		users[i] = uuid.New()
		peers[i] = &memPeer{}
		gate.allow(code, users[i])
	}
	var firstHello helloMsg
	for i := 0; i < RoomCap; i++ {
		raw := only(t, h.Handle(peers[i], users[i], roomJoinRaw(nick(i), "ab23cd")))
		if strings.Contains(string(raw), users[i].String()) {
			t.Fatal("hello contained the account id")
		}
		hello := decodeHello(t, raw)
		if hello.Channel != ChannelRoom || hello.Code != code || hello.Cap != RoomCap || hello.Count != i+1 {
			t.Fatalf("hello %+v", hello)
		}
		if i == 0 {
			firstHello = hello
		}
	}
	full := h.Handle(peers[RoomCap], users[RoomCap], roomJoinRaw("Them", code))
	errMsg := decodeErr(t, only(t, full))
	if errMsg.Channel != ChannelRoom || errMsg.Code != CodeRoomFull {
		t.Fatalf("full %+v", errMsg)
	}
	if _, ok := h.sessionLobby(peers[RoomCap]); ok {
		t.Fatal("full join took a seat")
	}

	lobbyHello := decodeHello(t, only(t, h.Handle(&memPeer{}, uuid.New(), joinRaw("Sảnh", nil))))
	if lobbyHello.Rooms.Status != "available" || lobbyHello.Rooms.Size != RoomCap {
		t.Fatalf("lobby rooms %+v", lobbyHello.Rooms)
	}

	move := roomRaw(map[string]any{"op": "move-to", "x": 0.2, "y": 0.3, "facing": "left"})
	if len(h.Handle(peers[0], users[0], move).Self) != 0 {
		t.Fatal("first move replied")
	}
	limited := h.Handle(peers[0], users[0], roomRaw(map[string]any{"op": "move-to", "x": 0.9, "y": 0.9}))
	if decodeErr(t, only(t, limited)).Code != CodeRateLimited {
		t.Fatalf("rate %+v", ops(t, limited))
	}
	if x := personX(t, h, peers[0]); x != 0.2 {
		t.Fatalf("limited move applied x=%v", x)
	}
	left := h.Handle(peers[1], users[1], roomRaw(map[string]any{"op": "leave"}))
	if decodeOp(t, only(t, left)) != OpBye {
		t.Fatalf("leave %+v", ops(t, left))
	}

	clock.Advance(time.Second)
	h.Handle(peers[0], users[0], roomRaw(map[string]any{"op": "wave", "target": firstHello.SessionID}))
	h.Handle(peers[2], users[2], roomRaw(map[string]any{
		"op": "compliment", "target": firstHello.SessionID,
	}))
	if decodeErr(t, only(t, h.Handle(peers[2], users[2], roomRaw(map[string]any{"op": "compliment"})))).Code != CodeBadMessage {
		t.Fatal("compliment without a target was accepted")
	}
	h.Handle(peers[3], users[3], roomRaw(map[string]any{"op": "heart"}))
	h.Handle(peers[3], users[3], roomRaw(map[string]any{"op": "hide", "hide": []string{firstHello.SessionID}}))
	if decodeErr(t, only(t, h.Handle(peers[0], users[0], mustRaw(map[string]any{
		"v": 1, "channel": "room", "op": "switch", "lobby": 1,
	})))).Code != CodeBadMessage {
		t.Fatal("switch is a room op")
	}

	peers[0].drain()
	h.Flush()
	tick := decodeTick(t, peers[0].last())
	if tick.Channel != ChannelRoom || tick.Code != code || tick.Cap != RoomCap {
		t.Fatalf("tick %+v", tick)
	}
	var mover *Person
	for i := range tick.Upserts {
		if tick.Upserts[i].ID == firstHello.SessionID {
			mover = &tick.Upserts[i]
		}
	}
	if mover == nil || mover.X != 0.2 || mover.Action != "wave" {
		t.Fatalf("upserts %+v", tick.Upserts)
	}
	var sawCompliment, sawHeart bool
	for _, ev := range tick.Events {
		if ev.Op == OpCompliment && ev.Target == firstHello.SessionID {
			sawCompliment = true
		}
		if ev.Op == OpHeart && ev.Target == "" {
			sawHeart = true
		}
	}
	if !sawCompliment || !sawHeart {
		t.Fatalf("events %+v", tick.Events)
	}
	// peers[3] hid firstHello, so a wave aimed at peers[3] from firstHello is dropped.
	clock.Advance(time.Second)
	h.Handle(peers[0], users[0], roomRaw(map[string]any{"op": "wave", "target": sessionID(t, h, peers[3])}))
	peers[3].drain()
	h.Flush()
	hidden := decodeTick(t, peers[3].last())
	for _, ev := range hidden.Events {
		if ev.Op == OpWave && ev.From == firstHello.SessionID {
			t.Fatalf("hidden wave delivered %+v", hidden.Events)
		}
	}

	gone := peers[4]
	goneID := sessionID(t, h, gone)
	h.Disconnect(gone)
	if !h.hasMember(goneID) {
		t.Fatal("room seat dropped before grace")
	}
	clock.Advance(29 * time.Second)
	if !h.hasMember(goneID) {
		t.Fatal("dropped before 30s")
	}
	clock.Advance(time.Second)
	if h.hasMember(goneID) {
		t.Fatal("still present at 30s")
	}

	var buf bytes.Buffer
	h.WriteStats(slog.New(slog.NewTextHandler(&buf, nil)), clock.Now().Add(time.Minute))
	line := buf.String()
	for _, forbidden := range []string{code, users[0].String(), firstHello.SessionID, "Mây"} {
		if strings.Contains(line, forbidden) {
			t.Fatalf("stats leaked %q in %s", forbidden, line)
		}
	}
	if !strings.Contains(line, "rooms=") || !strings.Contains(line, "room_present=") {
		t.Fatalf("stats %s", line)
	}

	gate.fail = errors.New("database")
	down := h.Handle(&memPeer{}, users[0], roomJoinRaw("Mây Hồng", code))
	if decodeErr(t, only(t, down)).Code != CodeUnavailable {
		t.Fatalf("gate failure %+v", ops(t, down))
	}
}

func TestRoomJoinReplacesLobbyPresence(t *testing.T) {
	h, _ := testHub(t)
	code := "AB23CD"
	user := uuid.New()
	gate := &memberGate{}
	gate.allow(code, user)
	h.SetRoomGate(gate)
	lobbyPeer := &memPeer{}
	if decodeHello(t, only(t, h.Handle(lobbyPeer, user, joinRaw("Mây Hồng", nil)))).Channel != ChannelLobby {
		t.Fatal("lobby hello")
	}
	roomPeer := &memPeer{}
	out := h.Handle(roomPeer, user, roomJoinRaw("Mây Hồng", code))
	hello := decodeHello(t, only(t, out))
	if hello.Channel != ChannelRoom || hello.Code != code || hello.Count != 1 {
		t.Fatalf("room hello %+v", hello)
	}
	if out.Kick != lobbyPeer {
		t.Fatal("lobby socket was not replaced")
	}
	if _, ok := h.sessionLobby(lobbyPeer); ok {
		t.Fatal("account still has a lobby seat")
	}
}

func TestRoomReconnectDuringGraceFreesTheSeat(t *testing.T) {
	h, clock := testHub(t)
	code := "K7MNPQ"
	user := uuid.New()
	gate := &memberGate{}
	gate.allow(code, user)
	h.SetRoomGate(gate)
	first := &memPeer{}
	hello := decodeHello(t, only(t, h.Handle(first, user, roomJoinRaw("Mây Hồng", code))))
	h.Disconnect(first)
	clock.Advance(5 * time.Second)
	if !h.hasMember(hello.SessionID) {
		t.Fatal("grace ended early")
	}
	second := &memPeer{}
	again := decodeHello(t, only(t, h.Handle(second, user, roomJoinRaw("Mây Hồng", code))))
	if again.Count != 1 || again.SessionID == hello.SessionID {
		t.Fatalf("reconnect %+v", again)
	}
	if h.hasMember(hello.SessionID) {
		t.Fatal("old grace seat still held")
	}
}

type memberGate struct {
	members map[string]map[uuid.UUID]struct{}
	fail    error
}

func (g *memberGate) allow(code string, ids ...uuid.UUID) {
	if g.members == nil {
		g.members = map[string]map[uuid.UUID]struct{}{}
	}
	if g.members[code] == nil {
		g.members[code] = map[uuid.UUID]struct{}{}
	}
	for _, id := range ids {
		g.members[code][id] = struct{}{}
	}
}

func (g *memberGate) Enter(userID uuid.UUID, code string) error {
	if g.fail != nil {
		return g.fail
	}
	if _, ok := g.members[code]; !ok {
		return ErrRoomNotFound
	}
	if _, ok := g.members[code][userID]; !ok {
		return ErrNotRoomMember
	}
	return nil
}

func roomJoinRaw(nick, code string) []byte {
	return mustRaw(map[string]any{
		"v": 1, "channel": "room", "op": "join", "code": code,
		"nickname": nick, "outfit": "outfit_pajamas", "hair": "hair_sleep_buns",
		"accessory": "accessory_star_clip", "x": 0.4, "y": 0.6, "facing": "right",
	})
}

func roomRaw(fields map[string]any) []byte {
	m := map[string]any{"v": 1, "channel": "room"}
	for k, v := range fields {
		m[k] = v
	}
	return mustRaw(m)
}

func sessionID(t *testing.T, h *Hub, p Peer) string {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.byPeer[p]
	if c == nil || c.session == nil {
		t.Fatal("no session")
	}
	return c.session.id
}
