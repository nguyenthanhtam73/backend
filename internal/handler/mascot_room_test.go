package handler

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/lobby"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/token"
	"github.com/fasthttp/websocket"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMascotRoomRESTAndSocket(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:room_http_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.MascotRoom{}, &domain.MascotRoomMember{}); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	mk := func(email string) *domain.User {
		t.Helper()
		u := &domain.User{Email: email, Username: "room-" + uuid.NewString(), IsActive: true}
		if err := users.Create(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		return u
	}
	owner := mk("owner-admin@example.com")
	guest := mk("guest-admin@example.com")
	stranger := mk("stranger@example.com")
	tok, err := token.NewService(config.JWTConfig{
		Secret:     "test-secret-for-room-http-32b!",
		AccessTTL:  time.Hour,
		RefreshTTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[uuid.UUID]*domain.User{owner.ID: owner, guest.ID: guest, stranger.ID: stranger}
	cfg := &config.Config{AdminEmails: []string{owner.Email, guest.Email}}
	hub := lobby.NewHub(lobby.Config{})
	hub.Start()
	t.Cleanup(hub.Stop)
	repo := repository.NewMascotRoomRepository(db)
	hub.SetRoomGate(newMascotRoomGate(repo))
	lookup := func(_ context.Context, id uuid.UUID) (*domain.User, error) { return byID[id], nil }
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	jwt := middleware.RequireAccessJWT(tok, lookup)
	api := app.Group("/api/v1")
	NewMascotRoomHandler(repo, cfg, hub).Register(api, jwt)
	NewLobbyHandler(hub, tok, cfg, lookup).Register(api)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	base := "http://" + ln.Addr().String() + "/api/v1"
	wsBase := "ws://" + ln.Addr().String() + lobby.PublicPath

	sign := func(u *domain.User) string {
		t.Helper()
		raw, err := tok.SignAccess(u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	ownerTok := sign(owner)
	guestTok := sign(guest)
	strangerTok := sign(stranger)

	status, body := roomJSONCall(t, http.MethodPost, base+"/mascot/rooms", strangerTok, "")
	if status != http.StatusNotFound || roomCodeOf(t, body) != "not_found" {
		t.Fatalf("stranger create %d %s", status, body)
	}

	status, body = roomJSONCall(t, http.MethodPost, base+"/mascot/rooms", ownerTok, "")
	if status != http.StatusCreated {
		t.Fatalf("create %d %s", status, body)
	}
	created := roomData(t, body)
	code, _ := created["code"].(string)
	if _, ok := lobby.NormalizeRoomCode(code); !ok || created["member_count"] != float64(1) || created["owner"] != true {
		t.Fatalf("create data %+v", created)
	}
	if strings.Contains(body, owner.ID.String()) || strings.Contains(body, owner.Email) {
		t.Fatal("create response leaked the account")
	}
	invite, _ := created["invite_url"].(string)
	if invite != lobby.RoomInvite(code) {
		t.Fatalf("invite %s", invite)
	}

	status, body = roomJSONCall(t, http.MethodGet, base+"/mascot/rooms/"+strings.ToLower(code), guestTok, "")
	if status != http.StatusOK {
		t.Fatalf("get %d %s", status, body)
	}
	got := roomData(t, body)
	if got["code"] != code || got["joined"] != false || got["member_count"] != float64(1) {
		t.Fatalf("get data %+v", got)
	}

	status, body = roomJSONCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/join", guestTok, "")
	if status != http.StatusOK {
		t.Fatalf("join %d %s", status, body)
	}
	if roomData(t, body)["member_count"] != float64(2) {
		t.Fatalf("join data %s", body)
	}
	status, body = roomJSONCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/join", guestTok, "")
	if status != http.StatusOK || roomData(t, body)["member_count"] != float64(2) {
		t.Fatalf("second join %d %s", status, body)
	}

	status, body = roomJSONCall(t, http.MethodGet, base+"/mascot/rooms/OOOOOO", ownerTok, "")
	if status != http.StatusBadRequest || roomCodeOf(t, body) != "bad_code" {
		t.Fatalf("bad code %d %s", status, body)
	}

	var fillers []*domain.User
	for i := 0; i < lobby.RoomCap-2; i++ {
		u := mk("fill-" + uuid.NewString() + "@example.com")
		cfg.AdminEmails = append(cfg.AdminEmails, u.Email)
		byID[u.ID] = u
		fillers = append(fillers, u)
		status, body = roomJSONCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/join", sign(u), "")
		if status != http.StatusOK {
			t.Fatalf("fill %d %s", status, body)
		}
	}
	ninth := mk("ninth-admin@example.com")
	cfg.AdminEmails = append(cfg.AdminEmails, ninth.Email)
	byID[ninth.ID] = ninth
	status, body = roomJSONCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/join", sign(ninth), "")
	if status != http.StatusConflict || roomCodeOf(t, body) != "room_full" {
		t.Fatalf("full %d %s", status, body)
	}

	notMember := dialLobby(t, wsBase, http.Header{"Authorization": []string{"Bearer " + sign(ninth)}})
	defer notMember.Close()
	if err := notMember.WriteMessage(websocket.TextMessage, roomFrame(code, "join")); err != nil {
		t.Fatal(err)
	}
	msg := readWSJSON(t, notMember)
	if msg["op"] != "error" || msg["code"] != "not_a_member" || msg["channel"] != "room" {
		t.Fatalf("ws non-member %+v", msg)
	}

	ownerConn := dialLobby(t, wsBase, http.Header{"Authorization": []string{"Bearer " + ownerTok}})
	defer ownerConn.Close()
	guestConn := dialLobby(t, wsBase, http.Header{"Authorization": []string{"Bearer " + guestTok}})
	defer guestConn.Close()
	if err := ownerConn.WriteMessage(websocket.TextMessage, roomFrame(code, "join")); err != nil {
		t.Fatal(err)
	}
	hello := readUntil(t, ownerConn, "hello")
	if hello["channel"] != "room" || hello["code"] != code {
		t.Fatalf("owner hello %+v", hello)
	}
	raw, _ := json.Marshal(hello)
	if strings.Contains(string(raw), owner.ID.String()) {
		t.Fatal("hello contained the account id")
	}
	if err := guestConn.WriteMessage(websocket.TextMessage, roomFrame(code, "join")); err != nil {
		t.Fatal(err)
	}
	if op := readUntil(t, guestConn, "hello")["op"]; op != "hello" {
		t.Fatalf("guest hello %v", op)
	}
	move := []byte(`{"v":1,"channel":"room","op":"move-to","x":0.66,"y":0.4,"facing":"left"}`)
	if err := guestConn.WriteMessage(websocket.TextMessage, move); err != nil {
		t.Fatal(err)
	}
	hub.Flush()
	tick := readRoomTick(t, ownerConn, 0.66)
	if tick["channel"] != "room" || tick["code"] != code {
		t.Fatalf("tick %+v", tick)
	}

	status, body = roomJSONCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/leave", guestTok, "")
	if status != http.StatusOK {
		t.Fatalf("leave %d %s", status, body)
	}
	if !socketClosed(t, guestConn) {
		t.Fatal("leave kept the socket open")
	}

	for _, u := range fillers {
		status, body = roomJSONCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/leave", sign(u), "")
		if status != http.StatusOK {
			t.Fatalf("filler leave %d %s", status, body)
		}
	}
	status, body = roomJSONCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/leave", ownerTok, "")
	if status != http.StatusOK {
		t.Fatalf("owner leave %d %s", status, body)
	}
	status, body = roomJSONCall(t, http.MethodGet, base+"/mascot/rooms/"+code, ownerTok, "")
	if status != http.StatusNotFound || roomCodeOf(t, body) != "room_not_found" {
		t.Fatalf("gone room %d %s", status, body)
	}
}

func roomJSONCall(t *testing.T, method, rawURL, access, body string) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, rawURL, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if access != "" {
		req.Header.Set("Authorization", "Bearer "+access)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(raw)
}

func roomData(t *testing.T, body string) map[string]any {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("json %s: %v", body, err)
	}
	return env.Data
}

func roomCodeOf(t *testing.T, body string) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("json %s: %v", body, err)
	}
	return env.Error.Code
}

func roomFrame(code, op string) []byte {
	return []byte(`{"v":1,"channel":"room","op":"` + op + `","code":"` + code + `","nickname":"Mây Hồng","outfit":"outfit_pajamas","hair":"hair_sleep_buns","x":0.4,"y":0.6,"facing":"right"}`)
}

func readWSJSON(t *testing.T, conn *websocket.Conn) map[string]any {
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
	return msg
}

func readRoomTick(t *testing.T, conn *websocket.Conn, x float64) map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		msg := readWSJSON(t, conn)
		if msg["op"] != "tick" {
			continue
		}
		upserts, _ := msg["upserts"].([]any)
		for _, item := range upserts {
			person, _ := item.(map[string]any)
			got, _ := person["x"].(float64)
			if got == x {
				return msg
			}
		}
	}
	t.Fatalf("no tick with x=%v", x)
	return nil
}

func socketClosed(t *testing.T, conn *websocket.Conn) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
		_, _, err := conn.ReadMessage()
		if err != nil {
			return true
		}
	}
	return false
}

func readUntil(t *testing.T, conn *websocket.Conn, op string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		msg := readWSJSON(t, conn)
		if msg["op"] == op {
			return msg
		}
	}
	t.Fatalf("no %s frame", op)
	return nil
}
