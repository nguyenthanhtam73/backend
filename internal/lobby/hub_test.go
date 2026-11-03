package lobby

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLobbyDefaults(t *testing.T) {
	if LobbyCap != 28 {
		t.Fatalf("cap %d", LobbyCap)
	}
	if Grace != 30*time.Second {
		t.Fatalf("grace %s", Grace)
	}
	if MessagesPerSec != 2 {
		t.Fatalf("rate %d", MessagesPerSec)
	}
	if FlushEvery != 250*time.Millisecond {
		t.Fatalf("flush %s", FlushEvery)
	}
	if RoomCap != 8 || RoomCodeLen != 6 {
		t.Fatalf("room contract %d %d", RoomCap, RoomCodeLen)
	}
	if LobbyLabel(1, 12, 28) != "Sảnh 1 · 12/28" {
		t.Fatalf("label %q", LobbyLabel(1, 12, 28))
	}
}

func TestCapacityOverflowAndSwitch(t *testing.T) {
	h, clock := testHub(t)
	peers := make([]*memPeer, 29)
	users := make([]uuid.UUID, 29)
	for i := 0; i < 28; i++ {
		peers[i] = &memPeer{}
		users[i] = uuid.New()
		out := h.Handle(peers[i], users[i], joinRaw(nick(i), nil))
		hello := decodeHello(t, only(t, out))
		if hello.Lobby != 1 {
			t.Fatalf("person %d lobby %d", i, hello.Lobby)
		}
		if hello.Cap != 28 {
			t.Fatalf("cap %d", hello.Cap)
		}
		if strings.Contains(string(only(t, out)), users[i].String()) {
			t.Fatal("hello contained the account id")
		}
	}
	peers[28] = &memPeer{}
	users[28] = uuid.New()
	out := h.Handle(peers[28], users[28], joinRaw(nick(28), nil))
	if len(out.Self) != 2 {
		t.Fatalf("29th replies %d", len(out.Self))
	}
	full := decodeFull(t, out.Self[0])
	if full.Op != OpLobbyFull || full.Lobby != 1 || full.Count != 28 || full.Cap != 28 || full.Label != "Sảnh 1 · 28/28" {
		t.Fatalf("lobby_full %+v", full)
	}
	hello := decodeHello(t, out.Self[1])
	if hello.Lobby != 2 {
		t.Fatalf("29th landed in %d", hello.Lobby)
	}
	var sawFull, sawNext bool
	for _, c := range hello.Counts {
		if c.Label == "Sảnh 1 · 28/28" {
			sawFull = true
		}
		if c.Label == "Sảnh 2 · 1/28" {
			sawNext = true
		}
	}
	if !sawFull || !sawNext {
		t.Fatalf("counts %+v", hello.Counts)
	}
	if hello.Rooms.Status != "not_available" || hello.Rooms.Size != 8 || hello.Rooms.CodeLength != 6 || hello.Rooms.InviteURL != RoomInviteURL {
		t.Fatalf("rooms %+v", hello.Rooms)
	}

	sw := h.Handle(peers[28], users[28], mustRaw(map[string]any{"v": 1, "channel": "lobby", "op": "switch", "lobby": 1}))
	full = decodeFull(t, only(t, sw))
	if full.Lobby != 1 || full.Label != "Sảnh 1 · 28/28" {
		t.Fatalf("switch full %+v", full)
	}
	if lobby, ok := h.sessionLobby(peers[28]); !ok || lobby != 2 {
		t.Fatalf("stayed lobby %d %v", lobby, ok)
	}
	_ = clock
}

func TestRateLimitPerConnection(t *testing.T) {
	h, clock := testHub(t)
	a := &memPeer{}
	b := &memPeer{}
	ua, ub := uuid.New(), uuid.New()
	if op := decodeOp(t, only(t, h.Handle(a, ua, joinRaw("Mây Hồng", nil)))); op != OpHello {
		t.Fatalf("join %s", op)
	}
	if op := decodeOp(t, only(t, h.Handle(b, ub, joinRaw("Bánh Flan", nil)))); op != OpHello {
		t.Fatalf("join b %s", op)
	}
	// Join used one token. One more move fits. The next is refused.
	move := h.Handle(a, ua, moveRaw(0.2, 0.3))
	if len(move.Self) != 0 {
		t.Fatalf("first move replies %+v", ops(t, move))
	}
	limited := h.Handle(a, ua, moveRaw(0.4, 0.5))
	errMsg := decodeErr(t, only(t, limited))
	if errMsg.Code != CodeRateLimited {
		t.Fatalf("code %s", errMsg.Code)
	}
	if x := personX(t, h, a); x != 0.2 {
		t.Fatalf("rejected move applied, x=%v", x)
	}
	// The other connection still has a token.
	if len(h.Handle(b, ub, moveRaw(0.6, 0.6)).Self) != 0 {
		t.Fatal("other connection was limited")
	}
	clock.Advance(time.Second)
	if len(h.Handle(a, ua, moveRaw(0.9, 0.9)).Self) != 0 {
		t.Fatal("window did not refill")
	}
	if x := personX(t, h, a); x != 0.9 {
		t.Fatalf("refilled move x=%v", x)
	}
}

func TestProfanityRejectsJoin(t *testing.T) {
	h, _ := testHub(t)
	p := &memPeer{}
	out := h.Handle(p, uuid.New(), joinRaw("địt", nil))
	errMsg := decodeErr(t, only(t, out))
	if errMsg.Code != CodeNicknameRejected {
		t.Fatalf("code %s", errMsg.Code)
	}
	if _, ok := h.sessionLobby(p); ok {
		t.Fatal("profane join took a slot")
	}
	chat := h.Handle(p, uuid.New(), []byte(`{"v":1,"channel":"lobby","op":"join","nickname":"Mây Hồng","outfit":"outfit_pajamas","hair":"hair_sleep_buns","message":"hi"}`))
	if decodeErr(t, only(t, chat)).Code != CodeBadMessage {
		t.Fatalf("chat frame %+v", ops(t, chat))
	}
	for _, field := range []string{"email", "user_id", "photo", "level", "points", "display_name"} {
		body := []byte(`{"v":1,"channel":"lobby","op":"join","nickname":"Mây Hồng","outfit":"outfit_pajamas","hair":"hair_sleep_buns","` + field + `":"x"}`)
		got := h.Handle(&memPeer{}, uuid.New(), body)
		if decodeErr(t, only(t, got)).Code != CodeBadMessage {
			t.Fatalf("field %s accepted", field)
		}
	}
}

func TestDropAfter30s(t *testing.T) {
	h, clock := testHub(t)
	watcher := &memPeer{}
	gone := &memPeer{}
	if op := decodeOp(t, only(t, h.Handle(watcher, uuid.New(), joinRaw("Mây Hồng", nil)))); op != OpHello {
		t.Fatal(op)
	}
	hello := decodeHello(t, only(t, h.Handle(gone, uuid.New(), joinRaw("Bánh Flan", nil))))
	h.Flush()
	watcher.drain()

	h.Disconnect(gone)
	if !h.hasMember(hello.SessionID) {
		t.Fatal("left immediately on disconnect")
	}
	clock.Advance(29 * time.Second)
	if !h.hasMember(hello.SessionID) {
		t.Fatal("dropped before 30s")
	}
	clock.Advance(time.Second)
	if h.hasMember(hello.SessionID) {
		t.Fatal("still present at 30s")
	}
	h.Flush()
	tick := decodeTick(t, watcher.last())
	if !contains(tick.Left, hello.SessionID) {
		t.Fatalf("watcher left list %+v", tick.Left)
	}
}

func TestHideDirectedActions(t *testing.T) {
	h, clock := testHub(t)
	a, b, c := &memPeer{}, &memPeer{}, &memPeer{}
	ha := decodeHello(t, only(t, h.Handle(a, uuid.New(), joinRaw("Mây Hồng", nil))))
	clock.Advance(time.Second)
	hb := decodeHello(t, only(t, h.Handle(b, uuid.New(), joinRaw("Bánh Flan", nil))))
	clock.Advance(time.Second)
	hc := decodeHello(t, only(t, h.Handle(c, uuid.New(), joinRaw("Cún Con", nil))))
	clock.Advance(time.Second)
	hide := h.Handle(b, uuid.New(), mustRaw(map[string]any{
		"v": 1, "channel": "lobby", "op": "hide", "hide": []string{ha.SessionID},
	}))
	if len(hide.Self) != 0 {
		t.Fatalf("hide replies %+v", ops(t, hide))
	}
	h.Flush()
	a.drain()
	b.drain()
	c.drain()

	clock.Advance(time.Second)
	h.Handle(a, uuid.New(), mustRaw(map[string]any{"v": 1, "channel": "lobby", "op": "compliment", "target": hb.SessionID}))
	clock.Advance(time.Second)
	h.Handle(a, uuid.New(), mustRaw(map[string]any{"v": 1, "channel": "lobby", "op": "compliment", "target": hc.SessionID}))
	clock.Advance(time.Second)
	h.Handle(a, uuid.New(), mustRaw(map[string]any{"v": 1, "channel": "lobby", "op": "wave"}))
	h.Flush()

	bTick := decodeTick(t, b.last())
	if eventAimed(bTick.Events, OpCompliment, ha.SessionID, hb.SessionID) {
		t.Fatal("hidden compliment was delivered to the target")
	}
	if eventFrom(bTick.Events, OpWave, ha.SessionID) {
		t.Fatal("hidden room-wide wave was delivered")
	}
	if !eventAimed(bTick.Events, OpCompliment, ha.SessionID, hc.SessionID) {
		t.Fatalf("compliment aimed at someone else missing: %+v", bTick.Events)
	}
	cTick := decodeTick(t, c.last())
	if !eventAimed(cTick.Events, OpCompliment, ha.SessionID, hb.SessionID) || !eventFrom(cTick.Events, OpWave, ha.SessionID) {
		t.Fatalf("third person missed events %+v", cTick.Events)
	}
	_ = hc
}

func TestReconnectDuringGraceTakesOneSlot(t *testing.T) {
	h, clock := testHub(t)
	first := &memPeer{}
	user := uuid.New()
	hello := decodeHello(t, only(t, h.Handle(first, user, joinRaw("Mây Hồng", nil))))
	h.Disconnect(first)
	clock.Advance(10 * time.Second)
	if !h.hasMember(hello.SessionID) {
		t.Fatal("grace ended early")
	}
	second := &memPeer{}
	again := decodeHello(t, only(t, h.Handle(second, user, joinRaw("Mây Hồng", nil))))
	if again.SessionID == hello.SessionID {
		t.Fatal("reconnect reused the session id")
	}
	if h.hasMember(hello.SessionID) {
		t.Fatal("old session still held a slot")
	}
	if lobby, ok := h.sessionLobby(second); !ok || lobby != 1 {
		t.Fatalf("lobby %d %v", lobby, ok)
	}
	if len(h.memberIDs(1)) != 1 {
		t.Fatalf("slots %+v", h.memberIDs(1))
	}
	clock.Advance(30 * time.Second)
	if !h.hasMember(again.SessionID) {
		t.Fatal("the new session was dropped with the old timer")
	}
}

func (h *Hub) memberIDs(lobby int) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	hall := h.hallAt(lobby)
	if hall == nil {
		return nil
	}
	ids := make([]string, 0, len(hall.members))
	for id := range hall.members {
		ids = append(ids, id)
	}
	return ids
}

func TestBroadcastCoalescesMoves(t *testing.T) {
	h, clock := testHub(t)
	p := &memPeer{}
	h.Handle(p, uuid.New(), joinRaw("Mây Hồng", nil))
	clock.Advance(time.Second)
	h.Handle(p, uuid.New(), moveRaw(0.2, 0.2))
	clock.Advance(time.Second)
	h.Handle(p, uuid.New(), moveRaw(0.8, 0.4))
	if len(p.inbox) != 0 {
		t.Fatal("move was broadcast before flush")
	}
	h.Flush()
	tick := decodeTick(t, p.last())
	if len(tick.Upserts) != 1 || tick.Upserts[0].X != 0.8 || tick.Upserts[0].Action != "walk" {
		t.Fatalf("upserts %+v", tick.Upserts)
	}
}

func TestRoomChannelNotAvailable(t *testing.T) {
	h, _ := testHub(t)
	p := &memPeer{}
	out := h.Handle(p, uuid.New(), []byte(`{"v":1,"channel":"room","op":"join","code":"AB12CD"}`))
	errMsg := decodeErr(t, only(t, out))
	if errMsg.Channel != ChannelRoom || errMsg.Code != CodeNotAvailable || errMsg.Rooms == nil || errMsg.Rooms.InviteURL != RoomInviteURL {
		t.Fatalf("%+v", errMsg)
	}
	if _, ok := h.sessionLobby(p); ok {
		t.Fatal("room join created a presence")
	}
}

func TestStatsLineHasNoPII(t *testing.T) {
	h, clock := testHub(t)
	secretNick := "Ten Bi Mat"
	user := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	p := &memPeer{}
	hello := decodeHello(t, only(t, h.Handle(p, user, joinRaw(secretNick, nil))))
	var buf bytes.Buffer
	h.WriteStats(slog.New(slog.NewTextHandler(&buf, nil)), clock.Now().Add(time.Minute))
	line := buf.String()
	for _, forbidden := range []string{secretNick, user.String(), hello.SessionID, "example.com", "@"} {
		if strings.Contains(line, forbidden) {
			t.Fatalf("stats leaked %q in %s", forbidden, line)
		}
	}
	for _, key := range []string{"lobbies", "connections", "present", "rooms", "room_present", "msgs_in_per_sec", "msgs_out_per_sec", "mem_alloc_bytes"} {
		if !strings.Contains(line, key) {
			t.Fatalf("stats missing %s in %s", key, line)
		}
	}
}

func testHub(t *testing.T) (*Hub, *fakeClock) {
	t.Helper()
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return NewHub(Config{Clock: clock}), clock
}

type memPeer struct {
	inbox  [][]byte
	closed bool
}

func (p *memPeer) Send(b []byte) bool {
	if p.closed {
		return false
	}
	p.inbox = append(p.inbox, append([]byte(nil), b...))
	return true
}

func (p *memPeer) Close() { p.closed = true }

func (p *memPeer) drain() { p.inbox = nil }

func (p *memPeer) last() []byte {
	if len(p.inbox) == 0 {
		return nil
	}
	return p.inbox[len(p.inbox)-1]
}

func joinRaw(nick string, lobby *int) []byte {
	m := map[string]any{
		"v": 1, "channel": "lobby", "op": "join",
		"nickname": nick, "outfit": "outfit_pajamas", "hair": "hair_sleep_buns",
		"accessory": "accessory_star_clip", "x": 0.4, "y": 0.6, "facing": "right",
	}
	if lobby != nil {
		m["lobby"] = *lobby
	}
	return mustRaw(m)
}

func moveRaw(x, y float64) []byte {
	return mustRaw(map[string]any{
		"v": 1, "channel": "lobby", "op": "move-to",
		"x": x, "y": y, "facing": "left", "action": "walk",
	})
}

func mustRaw(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func nick(i int) string {
	return "Ban " + itoa(i)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

type helloMsg struct {
	Op        string   `json:"op"`
	Channel   string   `json:"channel"`
	SessionID string   `json:"session_id"`
	Lobby     int      `json:"lobby"`
	Code      string   `json:"code"`
	Cap       int      `json:"cap"`
	Count     int      `json:"count"`
	Counts    []Count  `json:"counts"`
	People    []Person `json:"people"`
	Rooms     Rooms    `json:"rooms"`
}

type fullMsg struct {
	Op    string `json:"op"`
	Lobby int    `json:"lobby"`
	Count int    `json:"count"`
	Cap   int    `json:"cap"`
	Label string `json:"label"`
}

type errMsg struct {
	Channel string `json:"channel"`
	Op      string `json:"op"`
	Code    string `json:"code"`
	Rooms   *Rooms `json:"rooms"`
}

type tickMsg struct {
	Op      string   `json:"op"`
	Channel string   `json:"channel"`
	Lobby   int      `json:"lobby"`
	Code    string   `json:"code"`
	Count   int      `json:"count"`
	Cap     int      `json:"cap"`
	Upserts []Person `json:"upserts"`
	Left    []string `json:"left"`
	Events  []Event  `json:"events"`
}

func only(t *testing.T, out Outcome) []byte {
	t.Helper()
	if len(out.Self) != 1 {
		t.Fatalf("replies %d %+v", len(out.Self), ops(t, out))
	}
	return out.Self[0]
}

func ops(t *testing.T, out Outcome) []string {
	t.Helper()
	got := make([]string, 0, len(out.Self))
	for _, b := range out.Self {
		got = append(got, decodeOp(t, b))
	}
	return got
}

func decodeOp(t *testing.T, raw []byte) string {
	t.Helper()
	var m struct {
		Op string `json:"op"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m.Op
}

func decodeHello(t *testing.T, raw []byte) helloMsg {
	t.Helper()
	var m helloMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Op != OpHello {
		t.Fatalf("op %s body %s", m.Op, raw)
	}
	return m
}

func decodeFull(t *testing.T, raw []byte) fullMsg {
	t.Helper()
	var m fullMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func decodeErr(t *testing.T, raw []byte) errMsg {
	t.Helper()
	var m errMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Op != OpError {
		t.Fatalf("op %s body %s", m.Op, raw)
	}
	return m
}

func decodeTick(t *testing.T, raw []byte) tickMsg {
	t.Helper()
	if len(raw) == 0 {
		t.Fatal("missing tick")
	}
	var m tickMsg
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m.Op != OpTick {
		t.Fatalf("op %s body %s", m.Op, raw)
	}
	return m
}

func personX(t *testing.T, h *Hub, p Peer) float64 {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.byPeer[p]
	if c == nil || c.session == nil {
		t.Fatal("no session")
	}
	return c.session.person.X
}

func (h *Hub) hasMember(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.byID[id]
	return ok
}

func contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func eventAimed(ev []Event, op, from, target string) bool {
	for _, e := range ev {
		if e.Op == op && e.From == from && e.Target == target {
			return true
		}
	}
	return false
}

func eventFrom(ev []Event, op, from string) bool {
	for _, e := range ev {
		if e.Op == op && e.From == from {
			return true
		}
	}
	return false
}
