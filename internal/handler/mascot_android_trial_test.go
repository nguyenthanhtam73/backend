package handler

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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

func TestAndroidTrialOpensMascotAndHidesTestShirt(t *testing.T) {
	app, db, _ := newMascotAppFor(t, false)

	status, env := mascotCall(t, app, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusNotFound || mascotCode(t, env) != "not_found" {
		t.Fatalf("no header %d %#v", status, env)
	}
	status, env = mascotCallHeader(t, app, http.MethodGet, "/api/v1/me/mascot", "", map[string]string{
		"X-DaDiary-Client": "web",
	})
	if status != http.StatusNotFound || mascotCode(t, env) != "not_found" {
		t.Fatalf("other client %d %#v", status, env)
	}

	status, env = mascotCallHeader(t, app, http.MethodGet, "/api/v1/me/mascot", "", map[string]string{
		"X-DaDiary-Client": "android-app",
	})
	if status != http.StatusOK {
		t.Fatalf("trial %d %#v", status, env)
	}
	raw := mascotRaw(t, env)
	if strings.Contains(raw, domain.MascotAdminTestShirt) {
		t.Fatalf("trial response showed the test shirt %s", raw)
	}
	var shirts int64
	if err := db.Model(&domain.UserMascotItem{}).Where("item_id = ?", domain.MascotAdminTestShirt).Count(&shirts).Error; err != nil {
		t.Fatal(err)
	}
	if shirts != 0 {
		t.Fatal("trial grant included the test shirt")
	}

	adminApp, _, _ := newMascotApp(t)
	status, env = mascotCall(t, adminApp, http.MethodGet, "/api/v1/me/mascot", "")
	if status != http.StatusOK {
		t.Fatalf("admin %d %#v", status, env)
	}
}

func TestAndroidTrialOpensRooms(t *testing.T) {
	base, memberTok, adminTok := startTrialRoomHTTP(t)

	status, body := roomJSONCall(t, http.MethodPost, base+"/mascot/rooms", memberTok, "")
	if status != http.StatusNotFound || roomCodeOf(t, body) != "not_found" {
		t.Fatalf("no header %d %s", status, body)
	}
	status, body = roomJSONCall(t, http.MethodGet, base+"/mascot/rooms/mine", memberTok, "")
	if status != http.StatusNotFound || roomCodeOf(t, body) != "not_found" {
		t.Fatalf("mine no header %d %s", status, body)
	}

	status, body = roomCall(t, http.MethodPost, base+"/mascot/rooms", memberTok, "", map[string]string{
		"X-DaDiary-Client": "android-app",
	})
	if status != http.StatusCreated {
		t.Fatalf("create %d %s", status, body)
	}
	code, _ := roomData(t, body)["code"].(string)
	if code == "" {
		t.Fatalf("create body %s", body)
	}
	status, body = roomCall(t, http.MethodGet, base+"/mascot/rooms/mine", memberTok, "", map[string]string{
		"X-DaDiary-Client": "android-app",
	})
	if status != http.StatusOK || !strings.Contains(body, code) {
		t.Fatalf("mine %d %s", status, body)
	}
	status, body = roomCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/join", memberTok, "", map[string]string{
		"X-DaDiary-Client": "android-app",
	})
	if status != http.StatusOK {
		t.Fatalf("join %d %s", status, body)
	}

	status, body = roomJSONCall(t, http.MethodPost, base+"/mascot/rooms", adminTok, "")
	if status != http.StatusCreated {
		t.Fatalf("admin %d %s", status, body)
	}
}

func TestAndroidTrialHidesTestShirtOnLobby(t *testing.T) {
	admin := &domain.User{ID: uuid.New(), Email: "admin@example.com", IsActive: true}
	member := &domain.User{ID: uuid.New(), Email: "member@example.com", IsActive: true}
	wsBase, _, tok, _ := startLobbyServer(t, false, admin, member)
	memberAccess, err := tok.SignAccess(member.ID)
	if err != nil {
		t.Fatal(err)
	}
	adminAccess, err := tok.SignAccess(admin.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, resp, err := websocket.DefaultDialer.Dial(wsBase, http.Header{
		"Authorization": []string{"Bearer " + memberAccess},
	})
	if err == nil {
		t.Fatal("non-admin without the header upgraded")
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("no header status %d err %v", status, err)
	}

	conn := dialLobby(t, wsBase, http.Header{
		"Authorization":    []string{"Bearer " + memberAccess},
		"X-DaDiary-Client": []string{"android-app"},
	})
	defer conn.Close()
	hello := joinOutfit(t, conn, domain.MascotAdminTestShirt)
	if outfit := helloOutfit(t, hello); outfit != domain.MascotDefaultTrialOutfit {
		t.Fatalf("trial wore %q", outfit)
	}
	if strings.Contains(mustJSON(t, hello), domain.MascotAdminTestShirt) {
		t.Fatal("hello still named the test shirt")
	}

	adminConn := dialLobby(t, wsBase, http.Header{
		"Authorization": []string{"Bearer " + adminAccess},
	})
	defer adminConn.Close()
	adminHello := joinOutfit(t, adminConn, domain.MascotAdminTestShirt)
	if outfit := helloOutfit(t, adminHello); outfit != domain.MascotAdminTestShirt {
		t.Fatalf("admin shirt %q", outfit)
	}
}

func mascotCallHeader(t *testing.T, app *fiber.App, method, path, body string, headers map[string]string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return res.StatusCode, env
}

func mascotRaw(t *testing.T, env map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func startTrialRoomHTTP(t *testing.T) (base, memberTok, adminTok string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:room_trial_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.MascotRoom{}, &domain.MascotRoomMember{}, &domain.MascotRoomActiveSeat{}); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	mk := func(email string) *domain.User {
		t.Helper()
		u := &domain.User{Email: email, Username: "trial-" + uuid.NewString(), IsActive: true}
		if err := users.Create(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		return u
	}
	member := mk("trial-member@example.com")
	admin := mk("trial-admin@example.com")
	byID := map[uuid.UUID]*domain.User{member.ID: member, admin.ID: admin}
	tok, err := token.NewService(config.JWTConfig{
		Secret:     "test-secret-for-room-trial-32b!",
		AccessTTL:  time.Hour,
		RefreshTTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	sign := func(u *domain.User) string {
		t.Helper()
		raw, err := tok.SignAccess(u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	cfg := &config.Config{AdminEmails: []string{admin.Email}}
	hub := lobby.NewHub(lobby.Config{})
	hub.Start()
	t.Cleanup(hub.Stop)
	repo := repository.NewMascotRoomRepository(db)
	hub.SetRoomGate(newMascotRoomGate(repo))
	lookup := func(_ context.Context, id uuid.UUID) (*domain.User, error) { return byID[id], nil }
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	jwt := middleware.RequireAccessJWT(tok, lookup)
	NewMascotRoomHandler(repo, cfg, hub, nil).Register(app.Group("/api/v1"), jwt)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	return "http://" + ln.Addr().String() + "/api/v1", sign(member), sign(admin)
}

func roomCall(t *testing.T, method, rawURL, access, body string, headers map[string]string) (int, string) {
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
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(raw)
}

func joinOutfit(t *testing.T, conn *websocket.Conn, outfit string) map[string]any {
	t.Helper()
	body := []byte(`{"v":1,"channel":"lobby","op":"join","nickname":"Mây Hồng","outfit":"` + outfit + `","hair":"hair_sleep_buns","accessory":"accessory_star_clip","x":0.4,"y":0.6,"facing":"right"}`)
	if err := conn.WriteMessage(websocket.TextMessage, body); err != nil {
		t.Fatal(err)
	}
	return readHello(t, conn)
}

func helloOutfit(t *testing.T, msg map[string]any) string {
	t.Helper()
	sid, _ := msg["session_id"].(string)
	people, _ := msg["people"].([]any)
	for _, item := range people {
		row, _ := item.(map[string]any)
		if row["id"] != sid {
			continue
		}
		outfit, _ := row["outfit"].(string)
		return outfit
	}
	t.Fatalf("session %s missing from %#v", sid, msg)
	return ""
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
