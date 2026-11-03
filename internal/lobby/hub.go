package lobby

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"math"
	"regexp"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"log/slog"

	"github.com/google/uuid"
)

// Peer is one socket. Send must not block the hub. Close ends the socket.
type Peer interface {
	Send(b []byte) bool
	Close()
}

// Config tunes a Hub. Zero values use the package defaults.
type Config struct {
	Cap            int
	MaxHalls       int
	Grace          time.Duration
	FlushEvery     time.Duration
	MessagesPerSec int
	Clock          Clock
}

// Outcome is what Handle wants the socket loop to do after it releases no locks.
// Self is written to the caller. Kick, when set, is the previous socket for this
// account: write KickBody, then Close it.
type Outcome struct {
	Self     [][]byte
	Kick     Peer
	KickBody []byte
}

// Counters are process-local traffic totals since the hub was created.
type Counters struct {
	In          uint64
	Out         uint64
	InBytes     uint64
	OutBytes    uint64
	RateLimited uint64
}

// Stats is the once-a-minute line. Every field is a number. Nicknames, session
// ids, account ids, and emails are not included.
type Stats struct {
	Lobbies       int
	Connections   int
	Present       int
	MsgsInPerSec  float64
	MsgsOutPerSec float64
	MemAllocBytes uint64
	MemHeapBytes  uint64
}

// Hub is the in-memory lobby. It is safe for concurrent Handle, Flush, and Disconnect.
type Hub struct {
	cap        int
	maxHalls   int
	grace      time.Duration
	flushEvery time.Duration
	perSec     int
	clock      Clock

	mu         sync.Mutex
	byPeer     map[Peer]*connState
	byID       map[string]*session
	byUser     map[uuid.UUID]*session
	halls      []*hall
	lastCounts []Count

	in       atomic.Uint64
	out      atomic.Uint64
	inBytes  atomic.Uint64
	outBytes atomic.Uint64
	limited  atomic.Uint64

	statsAt  time.Time
	statsIn  uint64
	statsOut uint64

	stop      chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
}

type connState struct {
	peer    Peer
	userID  uuid.UUID
	session *session
	limit   limiter
}

type session struct {
	id     string
	userID uuid.UUID
	conn   *connState
	lobby  int
	person Person
	hide   map[string]struct{}
	drop   Stopper
}

type hall struct {
	members map[string]*session
	upserts map[string]struct{}
	left    []string
	events  []Event
}

type limiter struct {
	at []time.Time
}

func (l *limiter) allow(now time.Time, max int) bool {
	if max <= 0 {
		max = MessagesPerSec
	}
	cutoff := now.Add(-time.Second)
	n := 0
	for _, ts := range l.at {
		if ts.After(cutoff) {
			l.at[n] = ts
			n++
		}
	}
	l.at = l.at[:n]
	if len(l.at) >= max {
		return false
	}
	l.at = append(l.at, now)
	return true
}

// NewHub builds a hub. Call Start to broadcast and log. Tests can Flush instead.
func NewHub(cfg Config) *Hub {
	if cfg.Cap <= 0 {
		cfg.Cap = LobbyCap
	}
	if cfg.MaxHalls <= 0 {
		cfg.MaxHalls = MaxHalls
	}
	if cfg.Grace <= 0 {
		cfg.Grace = Grace
	}
	if cfg.FlushEvery <= 0 {
		cfg.FlushEvery = FlushEvery
	}
	if cfg.MessagesPerSec <= 0 {
		cfg.MessagesPerSec = MessagesPerSec
	}
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	return &Hub{
		cap:        cfg.Cap,
		maxHalls:   cfg.MaxHalls,
		grace:      cfg.Grace,
		flushEvery: cfg.FlushEvery,
		perSec:     cfg.MessagesPerSec,
		clock:      cfg.Clock,
		byPeer:     map[Peer]*connState{},
		byID:       map[string]*session{},
		byUser:     map[uuid.UUID]*session{},
	}
}

// Start runs the 4 Hz broadcast loop and the once-a-minute stats log until Stop.
func (h *Hub) Start() {
	if h == nil {
		return
	}
	h.startOnce.Do(func() {
		h.mu.Lock()
		h.statsAt = time.Now()
		h.stop = make(chan struct{})
		h.mu.Unlock()
		go h.loopFlush()
		go h.loopStats()
	})
}

// Stop ends the background loops. It is safe to call more than once.
func (h *Hub) Stop() {
	if h == nil {
		return
	}
	h.stopOnce.Do(func() {
		h.Start()
		close(h.stop)
	})
}

func (h *Hub) loopFlush() {
	t := time.NewTicker(h.flushEvery)
	defer t.Stop()
	for {
		select {
		case <-h.stop:
			return
		case <-t.C:
			h.Flush()
		}
	}
}

func (h *Hub) loopStats() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-h.stop:
			return
		case now := <-t.C:
			h.WriteStats(slog.Default(), now)
		}
	}
}

// Attach records a socket before its first frame so it counts as a connection.
func (h *Hub) Attach(peer Peer, userID uuid.UUID) {
	if h == nil || peer == nil || userID == uuid.Nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.byPeer[peer]; ok {
		return
	}
	h.byPeer[peer] = &connState{peer: peer, userID: userID}
}

// Disconnect starts the 30s grace when a joined socket drops.
// An explicit leave has already cleared the session, so this only forgets the socket.
// A second call for the same socket does nothing.
func (h *Hub) Disconnect(peer Peer) {
	if h == nil || peer == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.byPeer[peer]
	if c == nil {
		return
	}
	delete(h.byPeer, peer)
	c.peer = nil
	if c.session == nil || c.session.conn != c {
		return
	}
	id := c.session.id
	c.session.drop = h.clock.AfterFunc(h.grace, func() { h.expire(id) })
}

// Handle applies one client text frame. The caller sends Outcome after Handle returns.
func (h *Hub) Handle(peer Peer, userID uuid.UUID, raw []byte) Outcome {
	if h == nil || peer == nil {
		return Outcome{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	c := h.byPeer[peer]
	if c == nil {
		if userID == uuid.Nil {
			return Outcome{}
		}
		c = &connState{peer: peer, userID: userID}
		h.byPeer[peer] = c
	}
	h.in.Add(1)
	h.inBytes.Add(uint64(len(raw)))

	if len(raw) > MaxMessageBytes {
		return h.err(ChannelLobby, CodeBadMessage)
	}
	msg, err := parseClient(raw)
	if err != nil {
		if !c.limit.allow(h.clock.Now(), h.perSec) {
			h.limited.Add(1)
			return h.err(ChannelLobby, CodeRateLimited)
		}
		return h.err(ChannelLobby, CodeBadMessage)
	}
	if msg.Channel == ChannelRoom {
		return h.roomUnavailable()
	}
	if msg.Channel != "" && msg.Channel != ChannelLobby {
		return h.err(ChannelLobby, CodeBadMessage)
	}
	if msg.V > ProtocolVersion {
		return h.err(ChannelLobby, CodeUnsupportedVersion)
	}
	if msg.Op != OpLeave && !c.limit.allow(h.clock.Now(), h.perSec) {
		h.limited.Add(1)
		return h.err(ChannelLobby, CodeRateLimited)
	}
	switch msg.Op {
	case OpJoin:
		return h.joinLocked(c, msg)
	case OpMoveTo:
		return h.moveLocked(c, msg)
	case OpWave, OpHeart, OpCompliment:
		return h.emoteLocked(c, msg)
	case OpSwitch:
		return h.switchLocked(c, msg)
	case OpHide:
		return h.hideLocked(c, msg)
	case OpLeave:
		return h.leaveLocked(c)
	default:
		return h.err(ChannelLobby, CodeBadMessage)
	}
}

// Flush sends at most one tick to each joined socket. Idle halls produce nothing.
func (h *Hub) Flush() {
	if h == nil {
		return
	}
	jobs := h.collect()
	for _, j := range jobs {
		j.peer.Send(j.body)
	}
}

// Sizes reports hall count, open sockets, and people still in a hall
// (including the disconnect grace).
func (h *Hub) Sizes() (lobbies, connections, present int) {
	if h == nil {
		return 0, 0, 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.halls), len(h.byPeer), h.presentLocked()
}

// Counters returns traffic totals.
func (h *Hub) Counters() Counters {
	if h == nil {
		return Counters{}
	}
	return Counters{
		In:          h.in.Load(),
		Out:         h.out.Load(),
		InBytes:     h.inBytes.Load(),
		OutBytes:    h.outBytes.Load(),
		RateLimited: h.limited.Load(),
	}
}

// WriteStats logs one lobby_stats line. The record has counts and byte sizes only.
func (h *Hub) WriteStats(log *slog.Logger, now time.Time) {
	if h == nil {
		return
	}
	if log == nil {
		log = slog.Default()
	}
	st := h.snapshot(now)
	log.Info("lobby_stats",
		"lobbies", st.Lobbies,
		"connections", st.Connections,
		"present", st.Present,
		"msgs_in_per_sec", st.MsgsInPerSec,
		"msgs_out_per_sec", st.MsgsOutPerSec,
		"mem_alloc_bytes", st.MemAllocBytes,
		"mem_heap_bytes", st.MemHeapBytes,
	)
}

func (h *Hub) snapshot(now time.Time) Stats {
	h.mu.Lock()
	if h.statsAt.IsZero() {
		h.statsAt = now.Add(-time.Minute)
	}
	dt := now.Sub(h.statsAt).Seconds()
	if dt <= 0 {
		dt = 1
	}
	in := h.in.Load()
	out := h.out.Load()
	st := Stats{
		Lobbies:       len(h.halls),
		Connections:   len(h.byPeer),
		Present:       h.presentLocked(),
		MsgsInPerSec:  float64(in-h.statsIn) / dt,
		MsgsOutPerSec: float64(out-h.statsOut) / dt,
	}
	h.statsAt = now
	h.statsIn = in
	h.statsOut = out
	h.mu.Unlock()

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	st.MemAllocBytes = ms.Alloc
	st.MemHeapBytes = ms.HeapAlloc
	return st
}

func (h *Hub) presentLocked() int {
	n := 0
	for _, hall := range h.halls {
		n += len(hall.members)
	}
	return n
}

func (h *Hub) expire(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.byID[id]
	if s == nil {
		return
	}
	if s.conn != nil && s.conn.peer != nil {
		return
	}
	h.removeLocked(s, true, true)
}

type clientMsg struct {
	V         int       `json:"v"`
	Channel   string    `json:"channel"`
	Op        string    `json:"op"`
	Nickname  string    `json:"nickname"`
	Outfit    string    `json:"outfit"`
	Hair      string    `json:"hair"`
	Accessory string    `json:"accessory"`
	X         *float64  `json:"x"`
	Y         *float64  `json:"y"`
	Facing    string    `json:"facing"`
	Action    string    `json:"action"`
	Lobby     *int      `json:"lobby"`
	Target    string    `json:"target"`
	Hide      *[]string `json:"hide"`
	Code      string    `json:"code"`
}

func parseClient(raw []byte) (clientMsg, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var msg clientMsg
	if err := dec.Decode(&msg); err != nil {
		return clientMsg{}, err
	}
	if dec.More() {
		return clientMsg{}, errTrailing
	}
	msg.Channel = trim(msg.Channel)
	msg.Op = trim(msg.Op)
	msg.Target = trim(msg.Target)
	msg.Facing = trim(msg.Facing)
	msg.Action = trim(msg.Action)
	return msg, nil
}

var errTrailing = errString("trailing")

type errString string

func (e errString) Error() string { return string(e) }

func trim(s string) string {
	i := 0
	j := len(s)
	for i < j && (s[i] == ' ' || s[i] == '\n' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\n' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

func (h *Hub) joinLocked(c *connState, msg clientMsg) Outcome {
	nick, ok := CleanNickname(msg.Nickname)
	if !ok {
		return h.err(ChannelLobby, CodeNicknameRejected)
	}
	if !validItem(msg.Outfit, true) || !validItem(msg.Hair, true) || !validItem(msg.Accessory, false) {
		return h.err(ChannelLobby, CodeBadMessage)
	}
	x, y, posOK := h.position(msg, c.session)
	if !posOK {
		return h.err(ChannelLobby, CodeBadMessage)
	}
	facing, fOK := facingOr(msg.Facing, c.session, "right")
	if !fOK {
		return h.err(ChannelLobby, CodeBadMessage)
	}
	action, aOK := actionOr(msg.Action, c.session, "idle")
	if !aOK {
		return h.err(ChannelLobby, CodeBadMessage)
	}
	var hide map[string]struct{}
	if msg.Hide != nil {
		parsed, err := parseHide(*msg.Hide)
		if err != nil {
			return h.err(ChannelLobby, CodeBadMessage)
		}
		hide = parsed
	}

	req := requestedLobby(msg.Lobby)
	if req != 0 && !h.lobbyIndexOK(req) {
		return h.err(ChannelLobby, CodeInvalidLobby)
	}

	var out Outcome
	if c.session == nil {
		if prev := h.byUser[c.userID]; prev != nil && prev.conn != c {
			out.Kick, out.KickBody = h.kickLocked(prev)
		}
		id, err := h.newIDLocked()
		if err != nil {
			return h.err(ChannelLobby, CodeBadMessage)
		}
		s := &session{
			id: id, userID: c.userID, conn: c,
			person: Person{ID: id, Nickname: nick, Outfit: msg.Outfit, Hair: msg.Hair, Accessory: msg.Accessory, X: x, Y: y, Facing: facing, Action: action},
			hide:   map[string]struct{}{},
		}
		if hide != nil {
			s.hide = hide
		}
		c.session = s
		n, full, placed := h.placeNewLocked(s, req)
		if !placed {
			c.session = nil
			if full != nil {
				out.Self = [][]byte{full}
				return out
			}
			out.Self = h.err(ChannelLobby, CodeInvalidLobby).Self
			return out
		}
		s.lobby = n
		self := [][]byte{}
		if full != nil {
			self = append(self, full)
		}
		self = append(self, h.helloLocked(s))
		out.Self = self
		return out
	}

	s := c.session
	s.person.Nickname = nick
	s.person.Outfit = msg.Outfit
	s.person.Hair = msg.Hair
	s.person.Accessory = msg.Accessory
	s.person.X = x
	s.person.Y = y
	s.person.Facing = facing
	s.person.Action = action
	if hide != nil {
		s.hide = hide
	}
	want := s.lobby
	if msg.Lobby != nil && *msg.Lobby != 0 {
		want = *msg.Lobby
	}
	if want != s.lobby {
		return h.moveToLocked(s, want, false)
	}
	if hall := h.hallAt(s.lobby); hall != nil {
		hall.upserts[s.id] = struct{}{}
	}
	return Outcome{Self: [][]byte{h.helloLocked(s)}}
}

func (h *Hub) lobbyIndexOK(n int) bool {
	return n >= 1 && n <= h.maxHalls && n <= len(h.halls)+1
}

func requestedLobby(n *int) int {
	if n == nil || *n == 0 {
		return 0
	}
	return *n
}

// placeNewLocked puts a brand-new session into a hall.
// requested 0 auto-picks the lowest open hall. A full explicit request
// auto-picks as well and returns a lobby_full frame for the hall that was full.
func (h *Hub) placeNewLocked(s *session, requested int) (int, []byte, bool) {
	var full []byte
	if requested == 0 {
		if len(h.halls) > 0 && len(h.halls[0].members) >= h.cap {
			full = h.fullLocked(1)
		}
		n := h.lowestOpenLocked()
		if n == 0 {
			return 0, full, false
		}
		h.addToHallLocked(s, n, true)
		return n, full, true
	}
	if requested < 1 || requested > len(h.halls)+1 || requested > h.maxHalls {
		return 0, nil, false
	}
	if requested <= len(h.halls) && len(h.halls[requested-1].members) >= h.cap {
		full = h.fullLocked(requested)
		n := h.lowestOpenLocked()
		if n == 0 {
			return 0, full, false
		}
		h.addToHallLocked(s, n, true)
		return n, full, true
	}
	if requested == len(h.halls)+1 {
		h.halls = append(h.halls, newHall())
	}
	h.addToHallLocked(s, requested, true)
	return requested, nil, true
}

func (h *Hub) addToHallLocked(s *session, n int, asJoin bool) {
	for len(h.halls) < n {
		h.halls = append(h.halls, newHall())
	}
	hall := h.halls[n-1]
	s.lobby = n
	hall.members[s.id] = s
	h.byID[s.id] = s
	h.byUser[s.userID] = s
	hall.upserts[s.id] = struct{}{}
	if asJoin {
		h.pushEvent(hall, Event{Op: OpJoin, ID: s.id})
	}
}

func (h *Hub) moveLocked(c *connState, msg clientMsg) Outcome {
	if c.session == nil {
		return h.err(ChannelLobby, CodeNotJoined)
	}
	if msg.X == nil || msg.Y == nil || !validPos(*msg.X, *msg.Y) {
		return h.err(ChannelLobby, CodeBadMessage)
	}
	if msg.Facing != "" && msg.Facing != "left" && msg.Facing != "right" {
		return h.err(ChannelLobby, CodeBadMessage)
	}
	if msg.Action != "" && !validAction(msg.Action) {
		return h.err(ChannelLobby, CodeBadMessage)
	}
	s := c.session
	s.person.X = roundUnit(*msg.X)
	s.person.Y = roundUnit(*msg.Y)
	if msg.Facing != "" {
		s.person.Facing = msg.Facing
	}
	if msg.Action != "" {
		s.person.Action = msg.Action
	} else {
		s.person.Action = "walk"
	}
	if hall := h.hallAt(s.lobby); hall != nil {
		hall.upserts[s.id] = struct{}{}
	}
	return Outcome{}
}

func (h *Hub) emoteLocked(c *connState, msg clientMsg) Outcome {
	if c.session == nil {
		return h.err(ChannelLobby, CodeNotJoined)
	}
	s := c.session
	target := msg.Target
	if msg.Op == OpCompliment && target == "" {
		return h.err(ChannelLobby, CodeBadMessage)
	}
	if target != "" {
		if !validSessionID(target) {
			return h.err(ChannelLobby, CodeUnknownTarget)
		}
		other := h.byID[target]
		if other == nil || other.lobby != s.lobby || (other.id == s.id && msg.Op == OpCompliment) {
			return h.err(ChannelLobby, CodeUnknownTarget)
		}
	}
	switch msg.Op {
	case OpWave:
		s.person.Action = "wave"
	case OpHeart:
		s.person.Action = "heart"
	default:
		s.person.Action = "compliment"
	}
	if hall := h.hallAt(s.lobby); hall != nil {
		hall.upserts[s.id] = struct{}{}
		ev := Event{Op: msg.Op, From: s.id}
		if target != "" {
			ev.Target = target
		}
		h.pushEvent(hall, ev)
	}
	return Outcome{}
}

func (h *Hub) switchLocked(c *connState, msg clientMsg) Outcome {
	if c.session == nil {
		return h.err(ChannelLobby, CodeNotJoined)
	}
	if msg.Lobby == nil || *msg.Lobby <= 0 {
		return h.err(ChannelLobby, CodeInvalidLobby)
	}
	return h.moveToLocked(c.session, *msg.Lobby, true)
}

// moveToLocked relocates an existing session. auto is false for switch:
// a full hall returns lobby_full and the person stays.
func (h *Hub) moveToLocked(s *session, n int, strict bool) Outcome {
	if n < 1 || n > len(h.halls)+1 || n > h.maxHalls {
		return h.err(ChannelLobby, CodeInvalidLobby)
	}
	if n == s.lobby {
		return Outcome{Self: [][]byte{h.helloLocked(s)}}
	}
	if n <= len(h.halls) && len(h.halls[n-1].members) >= h.cap {
		if strict {
			return Outcome{Self: [][]byte{h.fullLocked(n)}}
		}
		full := h.fullLocked(n)
		next := h.lowestOpenLocked()
		if next == 0 || next == s.lobby {
			return Outcome{Self: [][]byte{full}}
		}
		h.detachHallLocked(s)
		h.addToHallLocked(s, next, true)
		return Outcome{Self: [][]byte{full, h.helloLocked(s)}}
	}
	h.detachHallLocked(s)
	if n == len(h.halls)+1 {
		h.halls = append(h.halls, newHall())
	}
	h.addToHallLocked(s, n, true)
	return Outcome{Self: [][]byte{h.helloLocked(s)}}
}

func (h *Hub) detachHallLocked(s *session) {
	hall := h.hallAt(s.lobby)
	if hall == nil {
		return
	}
	delete(hall.members, s.id)
	delete(hall.upserts, s.id)
	hall.left = appendCapped(hall.left, s.id)
	h.pushEvent(hall, Event{Op: OpLeave, ID: s.id})
}

func (h *Hub) hideLocked(c *connState, msg clientMsg) Outcome {
	if c.session == nil {
		return h.err(ChannelLobby, CodeNotJoined)
	}
	if msg.Hide == nil {
		return h.err(ChannelLobby, CodeBadMessage)
	}
	hide, err := parseHide(*msg.Hide)
	if err != nil {
		return h.err(ChannelLobby, CodeBadMessage)
	}
	c.session.hide = hide
	return Outcome{}
}

func (h *Hub) leaveLocked(c *connState) Outcome {
	if c.session == nil {
		return h.err(ChannelLobby, CodeNotJoined)
	}
	h.removeLocked(c.session, true, false)
	return Outcome{Self: [][]byte{h.track(byeBody())}}
}

func (h *Hub) kickLocked(old *session) (Peer, []byte) {
	var peer Peer
	if old.conn != nil {
		peer = old.conn.peer
	}
	h.removeLocked(old, true, true)
	if peer == nil {
		return nil, nil
	}
	return peer, h.track(replacedBody())
}

func (h *Hub) removeLocked(s *session, queueLeave, dropPeer bool) {
	if s.drop != nil {
		s.drop.Stop()
		s.drop = nil
	}
	if hall := h.hallAt(s.lobby); hall != nil {
		delete(hall.members, s.id)
		delete(hall.upserts, s.id)
		if queueLeave {
			hall.left = appendCapped(hall.left, s.id)
			h.pushEvent(hall, Event{Op: OpLeave, ID: s.id})
		}
	}
	delete(h.byID, s.id)
	if h.byUser[s.userID] == s {
		delete(h.byUser, s.userID)
	}
	if s.conn != nil {
		if dropPeer && s.conn.peer != nil {
			delete(h.byPeer, s.conn.peer)
			s.conn.peer = nil
		}
		s.conn.session = nil
	}
}

func (h *Hub) lowestOpenLocked() int {
	for i, hall := range h.halls {
		if len(hall.members) < h.cap {
			return i + 1
		}
	}
	if len(h.halls) >= h.maxHalls {
		return 0
	}
	h.halls = append(h.halls, newHall())
	return len(h.halls)
}

func (h *Hub) hallAt(n int) *hall {
	if n < 1 || n > len(h.halls) {
		return nil
	}
	return h.halls[n-1]
}

func (h *Hub) pruneLocked() {
	for _, hall := range h.halls {
		if len(hall.members) == 0 {
			hall.left = nil
			hall.events = nil
			hall.upserts = map[string]struct{}{}
		}
	}
	for len(h.halls) > 0 && len(h.halls[len(h.halls)-1].members) == 0 {
		h.halls = h.halls[:len(h.halls)-1]
	}
}

func (h *Hub) countsLocked() []Count {
	if len(h.halls) == 0 {
		return []Count{}
	}
	out := make([]Count, 0, len(h.halls))
	for i, hall := range h.halls {
		n := i + 1
		c := len(hall.members)
		out = append(out, Count{Lobby: n, Count: c, Cap: h.cap, Label: LobbyLabel(n, c, h.cap)})
	}
	return out
}

func (h *Hub) fullLocked(n int) []byte {
	count := 0
	if hall := h.hallAt(n); hall != nil {
		count = len(hall.members)
	}
	label := ""
	if n >= 1 {
		label = LobbyLabel(n, count, h.cap)
	}
	return h.track(mustJSON(struct {
		V       int    `json:"v"`
		Channel string `json:"channel"`
		Op      string `json:"op"`
		Lobby   int    `json:"lobby"`
		Count   int    `json:"count"`
		Cap     int    `json:"cap"`
		Label   string `json:"label,omitempty"`
	}{ProtocolVersion, ChannelLobby, OpLobbyFull, n, count, h.cap, label}))
}

func (h *Hub) helloLocked(s *session) []byte {
	people := []Person{}
	if hall := h.hallAt(s.lobby); hall != nil {
		people = make([]Person, 0, len(hall.members))
		for _, m := range hall.members {
			people = append(people, m.person)
		}
		sort.Slice(people, func(i, j int) bool { return people[i].ID < people[j].ID })
	}
	return h.track(mustJSON(struct {
		V         int      `json:"v"`
		Channel   string   `json:"channel"`
		Op        string   `json:"op"`
		SessionID string   `json:"session_id"`
		Lobby     int      `json:"lobby"`
		Cap       int      `json:"cap"`
		Counts    []Count  `json:"counts"`
		People    []Person `json:"people"`
		Rooms     Rooms    `json:"rooms"`
	}{ProtocolVersion, ChannelLobby, OpHello, s.id, s.lobby, h.cap, h.countsLocked(), people, roomInfo()}))
}

func (h *Hub) err(channel, code string) Outcome {
	body := struct {
		V       int    `json:"v"`
		Channel string `json:"channel"`
		Op      string `json:"op"`
		Code    string `json:"code"`
		Rooms   *Rooms `json:"rooms,omitempty"`
	}{V: ProtocolVersion, Channel: channel, Op: OpError, Code: code}
	if code == CodeNotAvailable {
		info := roomInfo()
		body.Rooms = &info
	}
	return Outcome{Self: [][]byte{h.track(mustJSON(body))}}
}

func (h *Hub) roomUnavailable() Outcome {
	return h.err(ChannelRoom, CodeNotAvailable)
}

func byeBody() []byte {
	return mustJSON(struct {
		V       int    `json:"v"`
		Channel string `json:"channel"`
		Op      string `json:"op"`
	}{ProtocolVersion, ChannelLobby, OpBye})
}

func replacedBody() []byte {
	return mustJSON(struct {
		V       int    `json:"v"`
		Channel string `json:"channel"`
		Op      string `json:"op"`
	}{ProtocolVersion, ChannelLobby, OpReplaced})
}

func (h *Hub) track(b []byte) []byte {
	h.out.Add(1)
	h.outBytes.Add(uint64(len(b)))
	return b
}

type delivery struct {
	peer Peer
	body []byte
}

func (h *Hub) collect() []delivery {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Drop events on empty halls (nobody there still needs them) and remove
	// trailing empty halls so counts in this tick match what clients should show.
	h.pruneLocked()

	type delta struct {
		upserts []Person
		left    []string
		events  []Event
		active  bool
	}
	deltas := make([]delta, len(h.halls))
	any := false
	for i, hall := range h.halls {
		if len(hall.upserts) == 0 && len(hall.left) == 0 && len(hall.events) == 0 {
			continue
		}
		any = true
		d := delta{active: true, left: append([]string(nil), hall.left...), events: append([]Event(nil), hall.events...)}
		if d.left == nil {
			d.left = []string{}
		}
		if d.events == nil {
			d.events = []Event{}
		}
		d.upserts = make([]Person, 0, len(hall.upserts))
		for id := range hall.upserts {
			if m := hall.members[id]; m != nil {
				d.upserts = append(d.upserts, m.person)
			}
		}
		sort.Slice(d.upserts, func(a, b int) bool { return d.upserts[a].ID < d.upserts[b].ID })
		hall.upserts = map[string]struct{}{}
		hall.left = nil
		hall.events = nil
		deltas[i] = d
	}
	counts := h.countsLocked()
	countsChanged := !sameCounts(h.lastCounts, counts)
	if !any && !countsChanged {
		return nil
	}
	h.lastCounts = append([]Count(nil), counts...)

	jobs := make([]delivery, 0, len(h.byPeer))
	for _, c := range h.byPeer {
		if c.peer == nil || c.session == nil {
			continue
		}
		idx := c.session.lobby - 1
		if idx < 0 || idx >= len(deltas) {
			continue
		}
		d := deltas[idx]
		ev := filterEvents(d.events, c.session.id, c.session.hide)
		if !d.active && !countsChanged {
			continue
		}
		if len(d.upserts) == 0 && len(d.left) == 0 && len(ev) == 0 && !countsChanged {
			continue
		}
		if d.upserts == nil {
			d.upserts = []Person{}
		}
		if d.left == nil {
			d.left = []string{}
		}
		if ev == nil {
			ev = []Event{}
		}
		body := h.track(mustJSON(struct {
			V       int      `json:"v"`
			Channel string   `json:"channel"`
			Op      string   `json:"op"`
			Lobby   int      `json:"lobby"`
			Counts  []Count  `json:"counts"`
			Upserts []Person `json:"upserts"`
			Left    []string `json:"left"`
			Events  []Event  `json:"events"`
		}{ProtocolVersion, ChannelLobby, OpTick, c.session.lobby, counts, d.upserts, d.left, ev}))
		jobs = append(jobs, delivery{peer: c.peer, body: body})
	}
	return jobs
}

func filterEvents(ev []Event, self string, hide map[string]struct{}) []Event {
	if len(hide) == 0 || len(ev) == 0 {
		if ev == nil {
			return []Event{}
		}
		return ev
	}
	out := make([]Event, 0, len(ev))
	for _, e := range ev {
		if _, hidden := hide[e.From]; hidden && aimedAt(e, self) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func aimedAt(e Event, self string) bool {
	switch e.Op {
	case OpWave, OpHeart:
		return e.Target == "" || e.Target == self
	case OpCompliment:
		return e.Target == self
	default:
		return false
	}
}

func sameCounts(a, b []Count) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (h *Hub) position(msg clientMsg, s *session) (float64, float64, bool) {
	if msg.X == nil && msg.Y == nil {
		if s != nil {
			return s.person.X, s.person.Y, true
		}
		return 0.5, 0.7, true
	}
	if msg.X == nil || msg.Y == nil || !validPos(*msg.X, *msg.Y) {
		return 0, 0, false
	}
	return roundUnit(*msg.X), roundUnit(*msg.Y), true
}

func facingOr(v string, s *session, def string) (string, bool) {
	if v == "" {
		if s != nil && s.person.Facing != "" {
			return s.person.Facing, true
		}
		return def, true
	}
	if v != "left" && v != "right" {
		return "", false
	}
	return v, true
}

func actionOr(v string, s *session, def string) (string, bool) {
	if v == "" {
		if s != nil && s.person.Action != "" {
			return s.person.Action, true
		}
		return def, true
	}
	if !validAction(v) {
		return "", false
	}
	return v, true
}

func validAction(s string) bool {
	switch s {
	case "idle", "walk", "wave", "heart", "compliment":
		return true
	default:
		return false
	}
}

func validPos(x, y float64) bool {
	if math.IsNaN(x) || math.IsNaN(y) || math.IsInf(x, 0) || math.IsInf(y, 0) {
		return false
	}
	return x >= 0 && x <= 1 && y >= 0 && y <= 1
}

func roundUnit(v float64) float64 {
	return math.Round(v*10000) / 10000
}

var itemIDRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

func validItem(id string, required bool) bool {
	if id == "" {
		return !required
	}
	return itemIDRe.MatchString(id)
}

var sessionIDRe = regexp.MustCompile(`^[abcdefghjkmnpqrstuvwxyz23456789]{12}$`)

func validSessionID(id string) bool {
	return sessionIDRe.MatchString(id)
}

func parseHide(ids []string) (map[string]struct{}, error) {
	if len(ids) > MaxHide {
		return nil, errTrailing
	}
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if !validSessionID(id) {
			return nil, errTrailing
		}
		out[id] = struct{}{}
	}
	return out, nil
}

const sessionAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

func (h *Hub) newIDLocked() (string, error) {
	for i := 0; i < 5; i++ {
		id, err := randomID()
		if err != nil {
			return "", err
		}
		if _, ok := h.byID[id]; !ok {
			return id, nil
		}
	}
	return "", errTrailing
}

func randomID() (string, error) {
	raw := make([]byte, SessionIDLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	out := make([]byte, SessionIDLen)
	for i, b := range raw {
		out[i] = sessionAlphabet[int(b)%len(sessionAlphabet)]
	}
	return string(out), nil
}

func newHall() *hall {
	return &hall{members: map[string]*session{}, upserts: map[string]struct{}{}}
}

func (h *Hub) pushEvent(hall *hall, e Event) {
	if hall == nil {
		return
	}
	hall.events = append(hall.events, e)
	if len(hall.events) > 128 {
		hall.events = append([]Event(nil), hall.events[len(hall.events)-128:]...)
	}
}

func appendCapped(dst []string, id string) []string {
	dst = append(dst, id)
	if len(dst) > 128 {
		dst = append([]string(nil), dst[len(dst)-128:]...)
	}
	return dst
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"v":1,"channel":"lobby","op":"error","code":"bad_message"}`)
	}
	return b
}

// sessionLobby reports the hall number for tests in this package.
func (h *Hub) sessionLobby(peer Peer) (int, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.byPeer[peer]
	if c == nil || c.session == nil {
		return 0, false
	}
	return c.session.lobby, true
}
