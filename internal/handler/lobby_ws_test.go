package handler

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/lobby"
	"github.com/dadiary/backend/internal/token"
	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func TestLobbyAuthBearerCookieAndQuery(t *testing.T) {
	admin := &domain.User{ID: uuid.New(), Email: "admin@example.com", IsActive: true}
	other := &domain.User{ID: uuid.New(), Email: "member@example.com", IsActive: true}
	wsBase, httpBase, tok, _ := startLobbyServer(t, false, admin, other)

	t.Run("missing", func(t *testing.T) {
		status, body := lobbyHTTP(t, httpBase, nil)
		if status != http.StatusUnauthorized || !strings.Contains(body, "unauthorized") {
			t.Fatalf("status %d body %s", status, body)
		}
	})

	t.Run("refresh rejected", func(t *testing.T) {
		refresh, _, err := tok.SignRefresh(admin.ID)
		if err != nil {
			t.Fatal(err)
		}
		status, _ := lobbyHTTP(t, httpBase, map[string]string{"Authorization": "Bearer " + refresh})
		if status != http.StatusUnauthorized {
			t.Fatalf("status %d", status)
		}
	})

	t.Run("non mascot", func(t *testing.T) {
		access, err := tok.SignAccess(other.ID)
		if err != nil {
			t.Fatal(err)
		}
		_, resp, err := websocket.DefaultDialer.Dial(wsBase, http.Header{"Authorization": []string{"Bearer " + access}})
		if err == nil {
			t.Fatal("non-admin upgraded")
		}
		if resp == nil || resp.StatusCode != http.StatusNotFound {
			status := 0
			if resp != nil {
				status = resp.StatusCode
			}
			t.Fatalf("status %d err %v", status, err)
		}
	})

	access, err := tok.SignAccess(admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("bearer", func(t *testing.T) {
		conn := dialLobby(t, wsBase, http.Header{"Authorization": []string{"Bearer " + access}})
		defer conn.Close()
		assertHelloHidesAccount(t, joinAndHello(t, conn), admin.ID)
	})

	t.Run("cookie", func(t *testing.T) {
		conn := dialLobby(t, wsBase, http.Header{"Cookie": []string{lobby.AccessCookie + "=" + access}})
		defer conn.Close()
		assertHelloHidesAccount(t, joinAndHello(t, conn), admin.ID)
	})

	t.Run("query", func(t *testing.T) {
		conn := dialLobby(t, wsBase+"?access_token="+url.QueryEscape(access), nil)
		defer conn.Close()
		assertHelloHidesAccount(t, joinAndHello(t, conn), admin.ID)
	})

	t.Run("upgrade required", func(t *testing.T) {
		status, body := lobbyHTTP(t, httpBase, map[string]string{"Authorization": "Bearer " + access})
		if status != http.StatusUpgradeRequired || !strings.Contains(body, "upgrade_required") {
			t.Fatalf("status %d body %s", status, body)
		}
	})
}

// startLobbyServer listens on 127.0.0.1:0.
// adminGate marks every address that equals admin@example.com or starts with admin- as an admin.
// Pass markAllAdmin to put every user on the allow-list (the load test).
func startLobbyServer(t *testing.T, markAllAdmin bool, users ...*domain.User) (wsBase, httpBase string, tok *token.Service, hub *lobby.Hub) {
	t.Helper()
	tok, err := token.NewService(config.JWTConfig{
		Secret:     "test-secret-for-lobby-ws-32b!",
		AccessTTL:  time.Hour,
		RefreshTTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[uuid.UUID]*domain.User, len(users))
	var emails []string
	for _, u := range users {
		byID[u.ID] = u
		if markAllAdmin || u.Email == "admin@example.com" || strings.HasPrefix(u.Email, "admin-") {
			emails = append(emails, u.Email)
		}
	}
	hub = lobby.NewHub(lobby.Config{})
	hub.Start()
	t.Cleanup(hub.Stop)
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	NewLobbyHandler(hub, tok, &config.Config{AdminEmails: emails}, func(_ context.Context, id uuid.UUID) (*domain.User, error) {
		return byID[id], nil
	}).Register(app.Group("/api/v1"))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	host := ln.Addr().String()
	return "ws://" + host + lobby.PublicPath, "http://" + host + lobby.PublicPath, tok, hub
}

func lobbyHTTP(t *testing.T, rawURL string, headers map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(body)
}

func dialLobby(t *testing.T, rawURL string, header http.Header) *websocket.Conn {
	t.Helper()
	var last error
	for i := 0; i < 30; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(rawURL, header)
		if err == nil {
			return conn
		}
		last = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(last)
	return nil
}

func joinAndHello(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	body := []byte(`{"v":1,"channel":"lobby","op":"join","nickname":"Mây Hồng","outfit":"outfit_pajamas","hair":"hair_sleep_buns","accessory":"accessory_star_clip","x":0.4,"y":0.6,"facing":"right"}`)
	if err := conn.WriteMessage(websocket.TextMessage, body); err != nil {
		t.Fatal(err)
	}
	return readHello(t, conn)
}

func readHello(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var msg map[string]any
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatal(err)
	}
	// A counts-only tick can arrive after hello if the flush loop wins the race.
	// Keep reading until hello. Tests send join immediately.
	deadline := time.Now().Add(3 * time.Second)
	for msg["op"] != "hello" && time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(deadline)
		_, raw, err = conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatal(err)
		}
	}
	if msg["op"] != "hello" {
		t.Fatalf("op %#v", msg["op"])
	}
	return msg
}

func assertHelloHidesAccount(t *testing.T, msg map[string]any, userID uuid.UUID) {
	t.Helper()
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, userID.String()) {
		t.Fatalf("hello contains account id %s", text)
	}
	for _, forbidden := range []string{"email", "user_id", "photo", "level", "points", "display_name"} {
		if _, ok := msg[forbidden]; ok {
			t.Fatalf("hello has %s", forbidden)
		}
	}
	if msg["session_id"] == "" || msg["session_id"] == nil {
		t.Fatal("missing session_id")
	}
	people, _ := msg["people"].([]any)
	if len(people) == 0 {
		t.Fatal("hello has no people")
	}
}
