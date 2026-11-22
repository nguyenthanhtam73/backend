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
	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/dadiary/backend/internal/token"
	"github.com/dadiary/backend/internal/usecase/roomquest"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type countingPush struct{ calls int }

func (p *countingPush) SendToUser(context.Context, uuid.UUID, pushsvc.NotificationPayload) error {
	p.calls++
	return nil
}

func TestRoomSharedGateQuestBoardNudge(t *testing.T) {
	env := startSharedRoom(t)
	base := env.base
	adminTok := env.tokens[env.admin.Email]
	memberTok := env.tokens[env.member.Email]
	webTok := env.tokens[env.web.Email]

	for _, path := range []string{"/quest", "/today"} {
		status, body := sharedCall(t, http.MethodGet, base+"/mascot/rooms/AB23CD"+path, webTok, "", nil)
		if status != http.StatusNotFound || roomCodeOf(t, body) != "not_found" {
			t.Fatalf("web %s %d %s", path, status, body)
		}
	}
	status, body := sharedCall(t, http.MethodPost, base+"/mascot/rooms/AB23CD/quest/claim", webTok, `{"member_days":5}`, nil)
	if status != http.StatusNotFound || roomCodeOf(t, body) != "not_found" {
		t.Fatalf("web claim %d %s", status, body)
	}
	status, body = sharedCall(t, http.MethodPost, base+"/mascot/rooms/AB23CD/nudge", webTok, `{"user_id":"`+env.member.ID.String()+`"}`, nil)
	if status != http.StatusNotFound || roomCodeOf(t, body) != "not_found" {
		t.Fatalf("web nudge %d %s", status, body)
	}
	status, body = sharedCall(t, http.MethodGet, base+lobby.Route, webTok, "", nil)
	if status != http.StatusNotFound || roomCodeOf(t, body) != "not_found" {
		t.Fatalf("web gesture socket %d %s", status, body)
	}
	status, body = sharedCall(t, http.MethodGet, base+lobby.Route, webTok, "", map[string]string{"X-DaDiary-Client": "web"})
	if status != http.StatusNotFound || roomCodeOf(t, body) != "not_found" {
		t.Fatalf("web client header %d %s", status, body)
	}

	android := map[string]string{"X-DaDiary-Client": "android-app"}
	status, body = sharedCall(t, http.MethodPost, base+"/mascot/rooms", memberTok, "", android)
	if status != http.StatusCreated {
		t.Fatalf("android create %d %s", status, body)
	}
	code, _ := roomData(t, body)["code"].(string)

	status, body = sharedCall(t, http.MethodGet, base+"/mascot/rooms/"+code+"/quest", adminTok, "", nil)
	if status != http.StatusForbidden || roomCodeOf(t, body) != "not_a_member" {
		t.Fatalf("non-member quest %d %s", status, body)
	}
	status, body = sharedCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/join", adminTok, "", nil)
	if status != http.StatusOK {
		t.Fatalf("admin join %d %s", status, body)
	}

	start := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	end := start.Add(8 * 24 * time.Hour)
	if err := env.db.Model(&domain.MascotRoom{}).Where("code = ?", code).Updates(map[string]any{
		"created_at": start,
		"ends_at":    end,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := env.db.Model(&domain.MascotRoomMember{}).Where("room_code = ?", code).Update("joined_at", start).Error; err != nil {
		t.Fatal(err)
	}
	if err := env.db.Model(&domain.MascotRoomActiveSeat{}).Where("room_code = ?", code).Update("ends_at", end).Error; err != nil {
		t.Fatal(err)
	}

	lateUTC := time.Date(2026, 11, 2, 16, 30, 0, 0, time.UTC)
	earlyNext := time.Date(2026, 11, 2, 17, 30, 0, 0, time.UTC)
	days := []time.Time{
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		streaktime.DateOf(lateUTC),
		streaktime.DateOf(earlyNext),
		time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC),
	}
	for _, day := range days {
		if err := env.db.Create(&domain.SkinCheck{
			UserID:     env.member.ID,
			ImageURLs:  json.RawMessage(`["https://cdn.example/secret-face.jpg"]`),
			UserNote:   "SECRET_NOTE_LEAK",
			Conditions: json.RawMessage(`["SECRET_CONDITION"]`),
			CheckDate:  day,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := env.db.Create(&domain.RoutineEntry{
		UserID:      env.member.ID,
		RoutineDate: streaktime.DateOf(lateUTC),
		Morning:     json.RawMessage(`[{"id":"wash","title":"Rửa mặt","completed":true}]`),
		Notes:       "SECRET_ROUTINE_NOTE",
	}).Error; err != nil {
		t.Fatal(err)
	}
	look, _ := json.Marshal(domain.MascotLoadout{
		Body: "body_chibi", Hair: "hair_test_look", Outfit: "outfit_cozy_pajamas",
		Accessory: "accessory_star_clip", Accessories: []string{"accessory_star_clip"},
		RoomItems: []string{"room_plant"},
	})
	if err := env.db.Create(&domain.UserMascot{
		UserID: env.member.ID, Loadout: look, Gender: domain.MascotGenderMale,
		GlowLevel: 4, GlowLevelSeen: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}

	env.quest.SetNow(func() time.Time { return earlyNext })
	status, body = sharedCall(t, http.MethodGet, base+"/mascot/rooms/"+code+"/today", memberTok, "", android)
	if status != http.StatusOK {
		t.Fatalf("today %d %s", status, body)
	}
	assertNoSkinLeak(t, body)
	if !strings.Contains(body, "hair_test_look") || !strings.Contains(body, `"display_name":"Lan"`) {
		t.Fatalf("board missing look or name %s", body)
	}
	if !strings.Contains(body, `"gender":"male"`) {
		t.Fatalf("gender %s", body)
	}
	memberRow := todayMember(t, body, env.member.ID.String())
	if memberRow["cared_today"] != false || memberRow["checked_today"] != true {
		t.Fatalf("vn 3 Nov flags %#v", memberRow)
	}
	for _, key := range []string{"image_urls", "user_note", "conditions", "score", "analysis", "avatar_url", "email", "notes"} {
		if _, ok := memberRow[key]; ok {
			t.Fatalf("leaked key %s", key)
		}
	}

	env.quest.SetNow(func() time.Time { return lateUTC })
	status, body = sharedCall(t, http.MethodGet, base+"/mascot/rooms/"+code+"/today", memberTok, "", android)
	if status != http.StatusOK {
		t.Fatalf("today earlier %d %s", status, body)
	}
	memberRow = todayMember(t, body, env.member.ID.String())
	if memberRow["cared_today"] != true || memberRow["checked_today"] != true {
		t.Fatalf("vn 2 Nov flags %#v", memberRow)
	}

	env.quest.SetNow(func() time.Time { return earlyNext })
	status, body = sharedCall(t, http.MethodGet, base+"/mascot/rooms/"+code+"/quest", memberTok, "", android)
	if status != http.StatusOK {
		t.Fatalf("quest %d %s", status, body)
	}
	quest := roomData(t, body)
	if quest["progress"] != float64(5) || quest["vn_today"] != "2026-11-03" {
		t.Fatalf("quest %#v", quest)
	}
	status, body = sharedCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/quest/claim", memberTok, `{"member_days":5,"message":"give me coins"}`, android)
	if status != http.StatusBadRequest || roomCodeOf(t, body) != "invalid_body" {
		t.Fatalf("free text claim %d %s", status, body)
	}
	status, body = sharedCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/quest/claim", memberTok, `{"member_days":5}`, android)
	if status != http.StatusOK {
		t.Fatalf("claim %d %s", status, body)
	}
	claimed := roomData(t, body)
	if claimed["already_claimed"] != false || claimed["coins"] != float64(40) || claimed["balance"] != float64(40) || claimed["tier"] != "small" {
		t.Fatalf("claim body %#v", claimed)
	}
	status, body = sharedCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/quest/claim", memberTok, `{"member_days":5}`, android)
	if status != http.StatusOK {
		t.Fatalf("reclaim %d %s", status, body)
	}
	again := roomData(t, body)
	if again["already_claimed"] != true || again["balance"] != float64(40) || again["coins"] != float64(40) {
		t.Fatalf("reclaim %#v", again)
	}

	status, body = sharedCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/nudge", memberTok, `{"user_id":"`+env.admin.ID.String()+`","message":"đi chăm da"}`, android)
	if status != http.StatusBadRequest || roomCodeOf(t, body) != "invalid_body" {
		t.Fatalf("free text nudge %d %s", status, body)
	}
	status, body = sharedCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/nudge", memberTok, `{"user_id":"`+env.member.ID.String()+`"}`, android)
	if status != http.StatusBadRequest || roomCodeOf(t, body) != "nudge_self" {
		t.Fatalf("self nudge %d %s", status, body)
	}
	status, body = sharedCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/nudge", memberTok, `{"user_id":"`+env.admin.ID.String()+`"}`, android)
	if status != http.StatusOK {
		t.Fatalf("nudge %d %s", status, body)
	}
	nudge := roomData(t, body)
	msg, _ := nudge["message"].(string)
	if !strings.Contains(msg, "Lan") || strings.Contains(msg, "đi chăm da") || nudge["push"] != "sent" {
		t.Fatalf("nudge %#v", nudge)
	}
	if env.push.calls != 1 {
		t.Fatalf("push calls %d", env.push.calls)
	}
	status, body = sharedCall(t, http.MethodPost, base+"/mascot/rooms/"+code+"/nudge", memberTok, `{"user_id":"`+env.admin.ID.String()+`"}`, android)
	if status != http.StatusConflict || roomCodeOf(t, body) != "already_nudged" {
		t.Fatalf("second nudge %d %s", status, body)
	}
	if env.push.calls != 1 {
		t.Fatalf("push calls after limit %d", env.push.calls)
	}
}

type sharedEnv struct {
	db     *gorm.DB
	base   string
	quest  *roomquest.Service
	push   *countingPush
	admin  *domain.User
	member *domain.User
	web    *domain.User
	tokens map[string]string
}

func startSharedRoom(t *testing.T) sharedEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:shared_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.MascotRoom{},
		&domain.MascotRoomMember{},
		&domain.MascotRoomActiveSeat{},
		&domain.MascotRoomQuestClaim{},
		&domain.MascotRoomNudge{},
		&domain.UserCoinWallet{},
		&domain.UserCoinLedger{},
		&domain.RoutineEntry{},
		&domain.SkinCheck{},
		&domain.UserMascot{},
	); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	mk := func(email, name string) *domain.User {
		t.Helper()
		u := &domain.User{
			Email: email, Username: "s-" + uuid.NewString(), DisplayName: name, IsActive: true,
			AvatarURL: "https://cdn.example/avatar-secret.jpg",
		}
		if err := users.Create(context.Background(), u); err != nil {
			t.Fatal(err)
		}
		return u
	}
	admin := mk("admin-shared@example.com", "Admin")
	member := mk("lan-secret@example.com", "Lan")
	web := mk("web-shared@example.com", "Web")
	byID := map[uuid.UUID]*domain.User{admin.ID: admin, member.ID: member, web.ID: web}
	tok, err := token.NewService(config.JWTConfig{
		Secret: "test-secret-for-shared-room-32b!", AccessTTL: time.Hour, RefreshTTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AdminEmails: []string{admin.Email}}
	hub := lobby.NewHub(lobby.Config{})
	hub.Start()
	t.Cleanup(hub.Stop)
	repo := repository.NewMascotRoomRepository(db)
	hub.SetRoomGate(newMascotRoomGate(repo))
	quest := roomquest.NewService(repository.NewRoomQuestRepository(db), hub)
	push := &countingPush{}
	quest.SetPush(push)
	lookup := func(_ context.Context, id uuid.UUID) (*domain.User, error) { return byID[id], nil }
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	jwt := middleware.RequireAccessJWT(tok, lookup)
	h := NewMascotRoomHandler(repo, cfg, hub, nil)
	h.AttachQuest(quest)
	h.Register(app.Group("/api/v1"), jwt)
	NewLobbyHandler(hub, tok, cfg, lookup).Register(app.Group("/api/v1"))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(ln) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	tokens := map[string]string{}
	for _, u := range []*domain.User{admin, member, web} {
		raw, err := tok.SignAccess(u.ID)
		if err != nil {
			t.Fatal(err)
		}
		tokens[u.Email] = raw
	}
	return sharedEnv{
		db: db, base: "http://" + ln.Addr().String() + "/api/v1",
		quest: quest, push: push, admin: admin, member: member, web: web, tokens: tokens,
	}
}

func sharedCall(t *testing.T, method, rawURL, access, body string, headers map[string]string) (int, string) {
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

func todayMember(t *testing.T, body, userID string) map[string]any {
	t.Helper()
	raw, _ := roomData(t, body)["members"].([]any)
	for _, item := range raw {
		row, _ := item.(map[string]any)
		if row["user_id"] == userID {
			return row
		}
	}
	t.Fatalf("member %s missing in %s", userID, body)
	return nil
}

func assertNoSkinLeak(t *testing.T, body string) {
	t.Helper()
	for _, secret := range []string{
		"SECRET_NOTE_LEAK", "secret-face", "avatar-secret", "SECRET_CONDITION",
		"SECRET_ROUTINE_NOTE", "lan-secret@example.com",
	} {
		if strings.Contains(body, secret) {
			t.Fatalf("board leaked %s in %s", secret, body)
		}
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(v any) {
		switch node := v.(type) {
		case map[string]any:
			for key, child := range node {
				switch key {
				case "image", "images", "image_urls", "photo", "photos", "avatar", "avatar_url",
					"condition", "conditions", "score", "scores", "note", "notes", "user_note",
					"analysis", "symptom", "symptoms", "email":
					t.Fatalf("board key %s", key)
				}
				walk(child)
			}
		case []any:
			for _, child := range node {
				walk(child)
			}
		}
	}
	var decoded any
	if err := json.Unmarshal(env.Data, &decoded); err != nil {
		t.Fatal(err)
	}
	walk(decoded)
}
