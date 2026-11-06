package handler

import (
	"net/url"
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
//
// Web (Origin https://dadiary.vn or https://www.dadiary.vn) sends the access
// JWT in the dadiary_access_token cookie. Android sends no Origin and must
// use Authorization: Bearer. A cookie on a request with no Origin is rejected.
// A query access_token is accepted only when Origin is absent and no Bearer
// token was sent. Any other Origin is rejected before the token is read.
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

// Authorize checks Origin before it reads a cookie or token, then the Bé Da gate.
// It does not log the token, the email, the origin, or the account id.
func (h *LobbyHandler) Authorize(c *fiber.Ctx) error {
	var cfg *config.Config
	if h != nil {
		cfg = h.cfg
	}
	origin := strings.TrimSpace(c.Get("Origin"))
	if origin != "" && !lobbyOriginAllowed(cfg, origin) {
		return response.Error(c, fiber.StatusForbidden, "forbidden", "origin not allowed")
	}
	// Browsers always send Origin. They must use the cookie, not a URL token.
	if origin != "" && strings.TrimSpace(c.Query("access_token")) != "" {
		return response.Error(c, fiber.StatusForbidden, "forbidden", "query token not allowed")
	}
	raw, bad := accessToken(c, origin == "")
	// No-Origin callers are Android and must send Authorization: Bearer.
	// A cookie alone is not a credential on that request.
	if origin == "" && !bad && raw == "" && hasBrowserAccessCookie(c) {
		return response.Error(c, fiber.StatusForbidden, "forbidden", "cookie requires origin")
	}
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
	if !domain.MascotVisibleFor(admin, androidTrialClient(c)) {
		return response.Error(c, fiber.StatusNotFound, "not_found", "not found")
	}
	if !websocket.IsWebSocketUpgrade(c) {
		return response.Error(c, fiber.StatusUpgradeRequired, "upgrade_required", "websocket upgrade required")
	}
	c.Locals(middleware.LocalsUserID, userID)
	c.Locals(mascotClosetAdminKey, admin)
	return c.Next()
}

// accessToken returns the access JWT and whether the Authorization header was
// present but unusable. A bad header does not fall through to a cookie.
// allowQuery is true only when the request has no Origin header. In that
// case the cookie is ignored: no-Origin upgrades must use Bearer (or, when
// no Bearer header is present, the query token).
func accessToken(c *fiber.Ctx, allowQuery bool) (string, bool) {
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
	if !allowQuery {
		for _, name := range []string{lobby.AccessCookie, "access_token"} {
			if v := strings.TrimSpace(c.Cookies(name)); v != "" {
				return v, false
			}
		}
	}
	if allowQuery {
		if v := strings.TrimSpace(c.Query("access_token")); v != "" {
			return v, false
		}
	}
	return "", false
}

// hasBrowserAccessCookie reports a non-empty lobby access cookie.
// The value is not returned and must not be logged.
func hasBrowserAccessCookie(c *fiber.Ctx) bool {
	for _, name := range []string{lobby.AccessCookie, "access_token"} {
		if strings.TrimSpace(c.Cookies(name)) != "" {
			return true
		}
	}
	return false
}

// lobbyWebOrigins are the production browser pages that may open the lobby.
var lobbyWebOrigins = map[string]struct{}{
	"https://dadiary.vn":     {},
	"https://www.dadiary.vn": {},
}

// lobbyOriginAllowed reports whether a non-empty Origin may open the socket.
// Localhost is allowed only when this process is not DADIARY_ENV=production.
func lobbyOriginAllowed(cfg *config.Config, origin string) bool {
	canonical, ok := canonicalOrigin(origin)
	if !ok {
		return false
	}
	if _, ok := lobbyWebOrigins[canonical]; ok {
		return true
	}
	if cfg != nil && cfg.IsProduction() {
		return false
	}
	return localDevOrigin(canonical)
}

func canonicalOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	if p := u.EscapedPath(); p != "" && p != "/" {
		return "", false
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), true
}

func localDevOrigin(canonical string) bool {
	u, err := url.Parse(canonical)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
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
	if admin, _ := conn.Locals(mascotClosetAdminKey).(bool); admin {
		h.hub.NoteClosetAdmin(peer, true)
	}
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
