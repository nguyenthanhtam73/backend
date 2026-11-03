package handler

import (
	"strings"
	"sync"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/lobby"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/token"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/google/uuid"
)

// LobbyHandler is GET /api/v1/mascot/lobby/ws.
// The same access JWT as the rest of the API is accepted from
// Authorization: Bearer (Android), the dadiary_access_token cookie (web),
// or the access_token query (browser WebSocket, which cannot set headers).
// While Bé Da is closed, the caller must be on the admin allow-list —
// the same rule as features.mascot on GET /me.
type LobbyHandler struct {
	hub    *lobby.Hub
	tokens *token.Service
	cfg    *config.Config
	lookup middleware.UserLookup
}

// NewLobbyHandler wires the shared lobby socket.
func NewLobbyHandler(hub *lobby.Hub, tokens *token.Service, cfg *config.Config, lookup middleware.UserLookup) *LobbyHandler {
	return &LobbyHandler{hub: hub, tokens: tokens, cfg: cfg, lookup: lookup}
}

// Register attaches the upgrade route. Authorize runs before the upgrade.
func (h *LobbyHandler) Register(r fiber.Router) {
	r.Get(lobby.Route, h.Authorize, websocket.New(h.Serve, websocket.Config{
		ReadBufferSize:  1024,
		WriteBufferSize: 8192,
	}))
}

// Authorize accepts the existing access JWT and the Bé Da gate.
// It does not log the token, the email, or the account id.
func (h *LobbyHandler) Authorize(c *fiber.Ctx) error {
	raw, bad := accessToken(c)
	if bad || raw == "" {
		return response.Error(c, fiber.StatusUnauthorized, "unauthorized", "missing or invalid Authorization header")
	}
	if h == nil || h.tokens == nil {
		return response.Error(c, fiber.StatusInternalServerError, "token_service_unavailable", "JWT service not configured")
	}
	userID, err := h.tokens.ParseAccessToken(raw)
	if err != nil || userID == uuid.Nil {
		return response.Error(c, fiber.StatusUnauthorized, "invalid_token", "access token invalid or expired")
	}
	if h.lookup == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "database_unavailable", "database is not available")
	}
	user, err := h.lookup(c.UserContext(), userID)
	if err != nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "database_unavailable", "database is not available")
	}
	if user == nil {
		return response.Error(c, fiber.StatusUnauthorized, "invalid_token", "access token invalid or expired")
	}
	admin := h.cfg != nil && h.cfg.IsAdminEmail(user.Email)
	if !domain.MascotVisible(admin) {
		return response.Error(c, fiber.StatusNotFound, "not_found", "not found")
	}
	if !websocket.IsWebSocketUpgrade(c) {
		return response.Error(c, fiber.StatusUpgradeRequired, "upgrade_required", "websocket upgrade required")
	}
	c.Locals(middleware.LocalsUserID, userID)
	return c.Next()
}

// accessToken returns the access JWT and whether the Authorization header was
// present but unusable. A bad header does not fall through to a cookie.
func accessToken(c *fiber.Ctx) (string, bool) {
	header := strings.TrimSpace(c.Get("Authorization"))
	if header != "" {
		if len(header) < 7 || !strings.EqualFold(header[:7], "bearer ") {
			return "", true
		}
		tok := strings.TrimSpace(header[7:])
		if tok == "" {
			return "", true
		}
		return tok, false
	}
	for _, name := range []string{lobby.AccessCookie, "access_token"} {
		if v := strings.TrimSpace(c.Cookies(name)); v != "" {
			return v, false
		}
	}
	if v := strings.TrimSpace(c.Query("access_token")); v != "" {
		return v, false
	}
	return "", false
}

// Serve is the upgraded socket. One account has one presence; a second socket
// replaces the first. Disconnect starts the 30s grace inside the hub.
func (h *LobbyHandler) Serve(conn *websocket.Conn) {
	if h == nil || h.hub == nil || conn == nil {
		return
	}
	userID, _ := conn.Locals(middleware.LocalsUserID).(uuid.UUID)
	if userID == uuid.Nil {
		_ = conn.Close()
		return
	}
	peer := newWSPeer(conn)
	h.hub.Attach(peer, userID)
	defer func() {
		h.hub.Disconnect(peer)
		peer.Close()
		peer.wait()
	}()

	conn.SetReadLimit(lobby.MaxMessageBytes)
	_ = conn.SetReadDeadline(time.Now().Add(lobby.PongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(lobby.PongWait))
	})
	peer.start()
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(lobby.PongWait))
		out := h.hub.Handle(peer, userID, raw)
		for _, body := range out.Self {
			peer.Send(body)
		}
		if out.Kick != nil {
			out.Kick.Send(out.KickBody)
			out.Kick.Close()
		}
	}
}

// wsPeer is the hub's view of one upgraded socket. Send never blocks the hub.
type wsPeer struct {
	conn *websocket.Conn
	send chan []byte
	done chan struct{}
	once sync.Once
	wg   sync.WaitGroup
}

func newWSPeer(conn *websocket.Conn) *wsPeer {
	return &wsPeer{
		conn: conn,
		send: make(chan []byte, 32),
		done: make(chan struct{}),
	}
}

func (p *wsPeer) start() {
	p.wg.Add(1)
	go p.writeLoop()
}

func (p *wsPeer) writeLoop() {
	defer p.wg.Done()
	defer p.conn.Close()
	ping := time.NewTicker(lobby.PingEvery)
	defer ping.Stop()
	for {
		select {
		case msg := <-p.send:
			if err := p.write(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ping.C:
			if err := p.write(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-p.done:
			p.drain()
			return
		}
	}
}

func (p *wsPeer) write(kind int, msg []byte) error {
	_ = p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return p.conn.WriteMessage(kind, msg)
}

func (p *wsPeer) drain() {
	for {
		select {
		case msg := <-p.send:
			_ = p.write(websocket.TextMessage, msg)
		default:
			return
		}
	}
}

func (p *wsPeer) Send(b []byte) bool {
	select {
	case <-p.done:
		return false
	case p.send <- b:
		return true
	default:
	}
	select {
	case <-p.send:
	default:
	}
	select {
	case <-p.done:
		return false
	case p.send <- b:
		return true
	default:
		return false
	}
}

func (p *wsPeer) Close() {
	p.once.Do(func() { close(p.done) })
}

func (p *wsPeer) wait() { p.wg.Wait() }
