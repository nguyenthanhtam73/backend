// Package lobby is the in-memory Bé Da shared lobby (Sảnh chung) and the
// live presence of an invite room (phòng).
//
// Room membership is decided by RoomGate. This package does not write the
// database. Wire messages are JSON text frames. Session ids are random per
// connection and are not account ids. The hub never puts an email, photo,
// display name, level, or point total on the wire. Room hello and presence
// add care_streak, the consecutive full-routine day count.
package lobby

import (
	"fmt"
	"time"
)

const (
	// ProtocolVersion is the current client/server message version.
	ProtocolVersion = 1

	// PublicPath is the WebSocket route on the API origin.
	// Full URL: wss://api.dadiary.vn/api/v1/mascot/lobby/ws
	PublicPath = "/api/v1/mascot/lobby/ws"

	// Route is the path registered on the /api/v1 group.
	Route = "/mascot/lobby/ws"

	// AccessCookie is the browser cookie that carries the same access JWT
	// Android sends as Authorization: Bearer. The access_token cookie name
	// is accepted too. Browsers do not pass the token on the query string.
	AccessCookie = "dadiary_access_token"

	// LobbyCap is how many people one Sảnh holds, including someone in the
	// 30s disconnect grace.
	LobbyCap = 28

	// MaxHalls is the most Sảnh numbers the process will open.
	MaxHalls = 64

	// Grace is how long a disconnected person stays in the hall.
	Grace = 30 * time.Second

	// FlushEvery is the broadcast period. 250ms is 4 Hz, inside the 2–4 Hz band.
	FlushEvery = 250 * time.Millisecond

	// MessagesPerSec is the per-connection budget for client text frames.
	// leave is exempt so a client can always exit. The window is rolling.
	MessagesPerSec = 2

	// MaxMessageBytes is the largest accepted client text frame.
	MaxMessageBytes = 4096

	// MaxNicknameRunes is the trimmed nickname length.
	MaxNicknameRunes = 24

	// MaxHide is the most session ids a connection may hide.
	MaxHide = 64

	// SessionIDLen is the random per-connection id length.
	SessionIDLen = 12

	// PingEvery is the server WebSocket ping period.
	PingEvery = 20 * time.Second

	// PongWait is how long the server waits for a pong or any client frame
	// before treating the socket as dead. The 30s grace starts after that.
	PongWait = 45 * time.Second

	// RoomCap is how many people one invite room holds, including someone
	// in the 30s disconnect grace. It is not how many days the room lasts.
	// That length is domain.MascotRoomDuration. RoomCodeLen is the invite
	// code length. RoomCodeAlphabet has no 0, O, 1, or I.
	RoomCap          = 8
	RoomCodeLen      = 6
	RoomCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	RoomInviteURL    = "https://dadiary.vn/be-da/phong/{code}"
)

// Client ops.
const (
	OpJoin       = "join"
	OpMoveTo     = "move-to"
	OpWave       = "wave"
	OpHeart      = "heart"
	OpCompliment = "compliment"
	OpSwitch     = "switch"
	OpHide       = "hide"
	OpLeave      = "leave"
)

// Server ops.
const (
	OpHello     = "hello"
	OpTick      = "tick"
	OpLobbyFull = "lobby_full"
	OpError     = "error"
	OpBye       = "bye"
	OpReplaced  = "replaced"
)

// Error codes on op "error".
const (
	CodeRateLimited        = "rate_limited"
	CodeNicknameRejected   = "nickname_rejected"
	CodeBadMessage         = "bad_message"
	CodeNotJoined          = "not_joined"
	CodeNotAvailable       = "not_available"
	CodeUnsupportedVersion = "unsupported_version"
	CodeInvalidLobby       = "invalid_lobby"
	CodeUnknownTarget      = "unknown_target"
	CodeRoomFull           = "room_full"
	CodeRoomNotFound       = "room_not_found"
	CodeRoomEnded          = "room_ended"
	CodeNotMember          = "not_a_member"
	CodeUnavailable        = "unavailable"
)

// Channels. "room" is an 8-person invite room. With no RoomGate the hub
// still answers channel "room" with not_available.
const (
	ChannelLobby = "lobby"
	ChannelRoom  = "room"
)

// Person is the only public shape for someone in a hall.
// id is a random session id, not a user id.
type Person struct {
	ID        string  `json:"id"`
	Nickname  string  `json:"nickname"`
	Outfit    string  `json:"outfit"`
	Hair      string  `json:"hair"`
	Accessory string  `json:"accessory"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Facing    string  `json:"facing"`
	Action    string  `json:"action"`
}

// Count is one Sảnh occupancy line, e.g. label "Sảnh 1 · 12/28".
type Count struct {
	Lobby int    `json:"lobby"`
	Count int    `json:"count"`
	Cap   int    `json:"cap"`
	Label string `json:"label"`
}

// Rooms describes invite rooms on a lobby hello.
// Status is "available" once a RoomGate is set, and "not_available" until then.
type Rooms struct {
	Status     string `json:"status"`
	Size       int    `json:"size"`
	CodeLength int    `json:"code_length"`
	InviteURL  string `json:"invite_url"`
}

// Event is one thing that happened since the previous tick.
// join and leave use id. wave, heart, and compliment use from and target.
type Event struct {
	Op     string `json:"op"`
	ID     string `json:"id,omitempty"`
	From   string `json:"from,omitempty"`
	Target string `json:"target,omitempty"`
}

func roomInfo() Rooms {
	return Rooms{
		Status:     "not_available",
		Size:       RoomCap,
		CodeLength: RoomCodeLen,
		InviteURL:  RoomInviteURL,
	}
}

// LobbyLabel formats the switcher line. The dot is U+00B7.
func LobbyLabel(lobby, count, cap int) string {
	return fmt.Sprintf("Sảnh %d · %d/%d", lobby, count, cap)
}
