package lobby

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
)

// RoomGate reports whether an account may enter a live invite room.
// Enter must not log the account id. It is called without the hub lock.
type RoomGate interface {
	Enter(userID uuid.UUID, code string) error
}

// ErrRoomNotFound and ErrNotRoomMember are the membership answers a RoomGate
// returns. Any other error is treated as a temporary failure.
var (
	ErrRoomNotFound  = errors.New("room not found")
	ErrNotRoomMember = errors.New("not a room member")
)

func (s *session) wireChannel() string {
	if s != nil && s.roomCode != "" {
		return ChannelRoom
	}
	return ChannelLobby
}

func (m clientMsg) ChannelOrLobby() string {
	if m.Channel == "" {
		return ChannelLobby
	}
	return m.Channel
}

func (h *Hub) placeOf(s *session) *hall {
	if s == nil {
		return nil
	}
	if s.roomCode != "" {
		if h.rooms == nil {
			return nil
		}
		return h.rooms[s.roomCode]
	}
	return h.hallAt(s.lobby)
}

func samePlace(a, b *session) bool {
	if a == nil || b == nil {
		return false
	}
	if a.roomCode != "" || b.roomCode != "" {
		return a.roomCode != "" && a.roomCode == b.roomCode
	}
	return a.lobby == b.lobby && a.lobby != 0
}

func (h *Hub) roomsFieldLocked() Rooms {
	info := roomInfo()
	if h.gate != nil {
		info.Status = "available"
	}
	return info
}

func (h *Hub) roomCountsLocked() (rooms, present int) {
	for _, room := range h.rooms {
		if len(room.members) == 0 {
			continue
		}
		rooms++
		present += len(room.members)
	}
	return rooms, present
}

// LeaveLive drops an account's live seat in code and closes that socket.
// A lobby seat is left alone. Safe to call when they are not in the room.
func (h *Hub) LeaveLive(userID uuid.UUID, code string) {
	if h == nil || userID == uuid.Nil {
		return
	}
	code, ok := NormalizeRoomCode(code)
	if !ok {
		return
	}
	h.mu.Lock()
	s := h.byUser[userID]
	if s == nil || s.roomCode != code {
		h.mu.Unlock()
		return
	}
	var peer Peer
	if s.conn != nil {
		peer = s.conn.peer
	}
	body := h.track(byeBody(ChannelRoom))
	h.removeLocked(s, true, true)
	h.mu.Unlock()
	if peer != nil {
		peer.Send(body)
		peer.Close()
	}
}

// dispatchRoomLocked runs a channel "room" frame. The hub lock is held.
// Join unlocks around RoomGate.Enter.
func (h *Hub) dispatchRoomLocked(c *connState, peer Peer, userID uuid.UUID, msg clientMsg) Outcome {
	if msg.Op != OpJoin {
		return h.roomOpLocked(c, msg)
	}
	code, ok := NormalizeRoomCode(msg.Code)
	if !ok {
		return h.err(ChannelRoom, CodeBadMessage)
	}
	gate := h.gate
	if gate == nil {
		return h.roomUnavailable()
	}
	h.mu.Unlock()
	gateErr := gate.Enter(userID, code)
	h.mu.Lock()
	c = h.byPeer[peer]
	if c == nil {
		return Outcome{}
	}
	return h.joinRoomLocked(c, msg, code, gateErr)
}

func (h *Hub) roomOpLocked(c *connState, msg clientMsg) Outcome {
	if c.session == nil || c.session.roomCode == "" {
		return h.err(ChannelRoom, CodeNotJoined)
	}
	switch msg.Op {
	case OpMoveTo:
		return h.moveLocked(c, msg)
	case OpWave, OpHeart, OpCompliment:
		return h.emoteLocked(c, msg)
	case OpHide:
		return h.hideLocked(c, msg)
	case OpLeave:
		return h.leaveLocked(c)
	default:
		return h.err(ChannelRoom, CodeBadMessage)
	}
}

func (h *Hub) joinRoomLocked(c *connState, msg clientMsg, code string, gateErr error) Outcome {
	if gateErr != nil {
		switch {
		case errors.Is(gateErr, ErrRoomNotFound):
			return h.err(ChannelRoom, CodeRoomNotFound)
		case errors.Is(gateErr, ErrNotRoomMember):
			return h.err(ChannelRoom, CodeNotMember)
		default:
			return h.err(ChannelRoom, CodeUnavailable)
		}
	}
	nick, ok := CleanNickname(msg.Nickname)
	if !ok {
		return h.err(ChannelRoom, CodeNicknameRejected)
	}
	if !validItem(msg.Outfit, true) || !validItem(msg.Hair, true) || !validItem(msg.Accessory, false) {
		return h.err(ChannelRoom, CodeBadMessage)
	}
	x, y, posOK := h.position(msg, c.session)
	if !posOK {
		return h.err(ChannelRoom, CodeBadMessage)
	}
	facing, fOK := facingOr(msg.Facing, c.session, "right")
	if !fOK {
		return h.err(ChannelRoom, CodeBadMessage)
	}
	action, aOK := actionOr(msg.Action, c.session, "idle")
	if !aOK {
		return h.err(ChannelRoom, CodeBadMessage)
	}
	var hide map[string]struct{}
	if msg.Hide != nil {
		parsed, err := parseHide(*msg.Hide)
		if err != nil {
			return h.err(ChannelRoom, CodeBadMessage)
		}
		hide = parsed
	}

	if c.session != nil && c.session.roomCode == code {
		s := c.session
		s.person.Nickname = nick
		s.person.Outfit = publicOutfit(c, msg.Outfit)
		s.person.Hair = msg.Hair
		s.person.Accessory = msg.Accessory
		s.person.X = x
		s.person.Y = y
		s.person.Facing = facing
		s.person.Action = action
		if hide != nil {
			s.hide = hide
		}
		if room := h.rooms[code]; room != nil {
			room.upserts[s.id] = struct{}{}
		}
		h.attachCareLocked(code, s)
		return Outcome{Self: [][]byte{h.helloRoomLocked(s)}}
	}

	var out Outcome
	if prev := h.byUser[c.userID]; prev != nil && prev.conn != c && prev.roomCode == code {
		out.Kick, out.KickBody = h.kickLocked(prev)
	}
	n := 0
	if room := h.rooms[code]; room != nil {
		n = len(room.members)
	}
	if n >= RoomCap {
		out.Self = h.err(ChannelRoom, CodeRoomFull).Self
		return out
	}
	if c.session != nil {
		h.removeLocked(c.session, true, false)
	}
	if prev := h.byUser[c.userID]; prev != nil && prev.conn != c {
		out.Kick, out.KickBody = h.kickLocked(prev)
	}
	id, err := h.newIDLocked()
	if err != nil {
		return h.err(ChannelRoom, CodeBadMessage)
	}
	s := &session{
		id: id, userID: c.userID, conn: c, roomCode: code,
		person: Person{ID: id, Nickname: nick, Outfit: publicOutfit(c, msg.Outfit), Hair: msg.Hair, Accessory: msg.Accessory, X: x, Y: y, Facing: facing, Action: action},
		hide:   map[string]struct{}{},
	}
	if hide != nil {
		s.hide = hide
	}
	c.session = s
	h.addToRoomLocked(s, code)
	h.attachCareLocked(code, s)
	out.Self = [][]byte{h.helloRoomLocked(s)}
	return out
}

// roomPerson is a live room seat. care_streak is always present, including 0.
// Lobby seats stay on Person and do not carry this field.
type roomPerson struct {
	Person
	CareStreak int `json:"care_streak"`
}

func (s *session) roomPerson() roomPerson {
	if s == nil {
		return roomPerson{}
	}
	return roomPerson{Person: s.person, CareStreak: s.careStreak}
}

// attachCareLocked saves the persona and fills care streaks for the room.
// The callbacks run without the hub lock. No callbacks means the streak stays 0.
func (h *Hub) attachCareLocked(code string, self *session) {
	if h == nil || self == nil || (h.saveNick == nil && h.careFn == nil) {
		return
	}
	var ids []uuid.UUID
	if room := h.rooms[code]; room != nil {
		ids = make([]uuid.UUID, 0, len(room.members))
		for _, m := range room.members {
			ids = append(ids, m.userID)
		}
	}
	got := h.loadCareUnlocked(code, self.userID, self.person.Nickname, ids)
	if len(got) == 0 {
		return
	}
	room := h.rooms[code]
	if room == nil {
		if n, ok := got[self.userID]; ok {
			self.careStreak = n
		}
		return
	}
	for _, m := range room.members {
		n, ok := got[m.userID]
		if !ok || m.careStreak == n {
			continue
		}
		m.careStreak = n
		if m.id != self.id {
			room.upserts[m.id] = struct{}{}
		}
	}
}

func (h *Hub) loadCareUnlocked(code string, userID uuid.UUID, nick string, ids []uuid.UUID) map[uuid.UUID]int {
	save := h.saveNick
	fn := h.careFn
	h.mu.Unlock()
	defer h.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if save != nil {
		_ = save(ctx, userID, code, nick)
	}
	if fn == nil {
		return nil
	}
	return fn(ctx, ids)
}

// NoteCareStreaks copies a refresh into live seats of code.
// A changed value is included in the next presence tick. Same values are not.
func (h *Hub) NoteCareStreaks(code string, streaks map[uuid.UUID]int) {
	if h == nil || len(streaks) == 0 {
		return
	}
	code, ok := NormalizeRoomCode(code)
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.rooms[code]
	if room == nil {
		return
	}
	for _, m := range room.members {
		n, ok := streaks[m.userID]
		if !ok || m.careStreak == n {
			continue
		}
		m.careStreak = n
		room.upserts[m.id] = struct{}{}
	}
}

func (h *Hub) addToRoomLocked(s *session, code string) {
	if h.rooms == nil {
		h.rooms = map[string]*hall{}
	}
	if h.rooms[code] == nil {
		h.rooms[code] = newHall()
	}
	room := h.rooms[code]
	s.roomCode = code
	s.lobby = 0
	if s.conn != nil {
		s.conn.session = s
	}
	room.members[s.id] = s
	h.byID[s.id] = s
	h.byUser[s.userID] = s
	room.upserts[s.id] = struct{}{}
	h.pushEvent(room, Event{Op: OpJoin, ID: s.id})
}

func (h *Hub) helloRoomLocked(s *session) []byte {
	people := []roomPerson{}
	if room := h.rooms[s.roomCode]; room != nil {
		people = make([]roomPerson, 0, len(room.members))
		for _, m := range room.members {
			people = append(people, m.roomPerson())
		}
		sort.Slice(people, func(i, j int) bool { return people[i].ID < people[j].ID })
	}
	return h.track(mustJSON(struct {
		V         int          `json:"v"`
		Channel   string       `json:"channel"`
		Op        string       `json:"op"`
		SessionID string       `json:"session_id"`
		Code      string       `json:"code"`
		Cap       int          `json:"cap"`
		Count     int          `json:"count"`
		People    []roomPerson `json:"people"`
	}{ProtocolVersion, ChannelRoom, OpHello, s.id, s.roomCode, RoomCap, len(people), people}))
}

func (h *Hub) collectRoomsLocked() []delivery {
	if len(h.rooms) == 0 {
		return nil
	}
	var jobs []delivery
	var empty []string
	for code, room := range h.rooms {
		if len(room.members) == 0 {
			empty = append(empty, code)
			continue
		}
		if len(room.upserts) == 0 && len(room.left) == 0 && len(room.events) == 0 {
			continue
		}
		upserts := make([]roomPerson, 0, len(room.upserts))
		for id := range room.upserts {
			if m := room.members[id]; m != nil {
				upserts = append(upserts, m.roomPerson())
			}
		}
		sort.Slice(upserts, func(a, b int) bool { return upserts[a].ID < upserts[b].ID })
		left := append([]string(nil), room.left...)
		events := append([]Event(nil), room.events...)
		if left == nil {
			left = []string{}
		}
		if events == nil {
			events = []Event{}
		}
		room.upserts = map[string]struct{}{}
		room.left = nil
		room.events = nil
		count := len(room.members)
		for _, m := range room.members {
			if m == nil || m.conn == nil || m.conn.peer == nil {
				continue
			}
			ev := filterEvents(events, m.id, m.hide)
			if ev == nil {
				ev = []Event{}
			}
			if len(upserts) == 0 && len(left) == 0 && len(ev) == 0 {
				continue
			}
			body := h.track(mustJSON(struct {
				V       int          `json:"v"`
				Channel string       `json:"channel"`
				Op      string       `json:"op"`
				Code    string       `json:"code"`
				Count   int          `json:"count"`
				Cap     int          `json:"cap"`
				Upserts []roomPerson `json:"upserts"`
				Left    []string     `json:"left"`
				Events  []Event      `json:"events"`
			}{ProtocolVersion, ChannelRoom, OpTick, code, count, RoomCap, upserts, left, ev}))
			jobs = append(jobs, delivery{peer: m.conn.peer, body: body})
		}
	}
	for _, code := range empty {
		delete(h.rooms, code)
	}
	return jobs
}
