package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	resendhookuc "github.com/dadiary/backend/internal/usecase/resendhook"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	testWebhookSecret = "whsec_ERERERERERERERERERERERERERERERER"
	testWebhookSig    = "v1,GiYVOEltBEIK+QoCbvd16cX5FxA8aaCOHSUM6q8UoRo="
)

func testWebhookBody() []byte {
	return []byte(`{"type":"email.opened","created_at":"2023-11-14T22:13:20Z","data":{"email_id":"re_abc","created_at":"2023-11-14T22:13:20Z"}}`)
}

func newWebhookApp(t *testing.T, secret string) (*fiber.App, *repository.EmailEngagementRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:resend_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.EmailSendReceipt{}, &domain.EmailEngagementEvent{}); err != nil {
		t.Fatal(err)
	}
	user := &domain.User{Email: "open@test.com", Username: "open@test.com", IsActive: true}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.EmailSendReceipt{
		UserID:        user.ID,
		Kind:          "d1",
		ResendEmailID: "re_abc",
		CreatedAt:     time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	events := repository.NewEmailEngagementRepository(db)
	receipts := repository.NewEmailSendReceiptRepository(db)
	svc := resendhookuc.NewService(secret, events, receipts, repository.NewUserRepository(db))
	svc.SetClock(func() time.Time { return time.Unix(1700000000, 0) })

	app := fiber.New()
	h := NewResendWebhookHandler(svc)
	app.Post("/api/v1/email/resend/webhook", h.Handle)
	return app, events, db
}

func postWebhook(t *testing.T, app *fiber.App, body []byte, id, ts, sig string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/email/resend/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("svix-id", id)
	req.Header.Set("svix-timestamp", ts)
	req.Header.Set("svix-signature", sig)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestResendWebhook_StoresOpenAndIsIdempotent(t *testing.T) {
	app, events, db := newWebhookApp(t, testWebhookSecret)
	body := testWebhookBody()
	// The golden signature is for a different body (email_id abc). Sign this
	// body with the same key so the handler test uses a real Svix signature.
	sig := signTestWebhook(t, "msg_open_1", "1700000000", body)

	resp := postWebhook(t, app, body, "msg_open_1", "1700000000", sig)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	n, err := events.CountByResendEventID(context.Background(), "msg_open_1")
	if err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	var row domain.EmailEngagementEvent
	if err := db.Where("resend_event_id = ?", "msg_open_1").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.EventType != domain.EmailEventOpened || row.Kind != "d1" || row.UserID == nil {
		t.Fatalf("row=%+v", row)
	}

	again := postWebhook(t, app, body, "msg_open_1", "1700000000", sig)
	if again.StatusCode != http.StatusOK {
		t.Fatalf("duplicate status=%d", again.StatusCode)
	}
	n, err = events.CountByResendEventID(context.Background(), "msg_open_1")
	if err != nil || n != 1 {
		t.Fatalf("duplicate count=%d err=%v", n, err)
	}
	var payload map[string]any
	b, _ := io.ReadAll(again.Body)
	_ = json.Unmarshal(b, &payload)
	if payload["duplicate"] != true {
		t.Fatalf("duplicate payload=%v", payload)
	}
}

func TestResendWebhook_RejectsBadSignatureAndIgnoresOtherEvents(t *testing.T) {
	app, events, db := newWebhookApp(t, testWebhookSecret)
	body := testWebhookBody()
	bad := postWebhook(t, app, body, "msg_bad", "1700000000", "v1,AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if bad.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad sig status=%d", bad.StatusCode)
	}
	n, err := events.CountByResendEventID(context.Background(), "msg_bad")
	if err != nil || n != 0 {
		t.Fatalf("rejected event stored count=%d err=%v", n, err)
	}

	other := []byte(`{"type":"email.delivered","data":{"email_id":"re_abc"}}`)
	sig := signTestWebhook(t, "msg_del", "1700000000", other)
	ok := postWebhook(t, app, other, "msg_del", "1700000000", sig)
	if ok.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(ok.Body)
		t.Fatalf("noop status=%d body=%s", ok.StatusCode, b)
	}
	n, err = events.CountByResendEventID(context.Background(), "msg_del")
	if err != nil || n != 0 {
		t.Fatalf("delivered should not persist count=%d err=%v", n, err)
	}

	clickBody := []byte(`{"type":"email.clicked","data":{"email_id":"re_abc","click":{"link":"https://dadiary.vn/check-in?src=email_d1","timestamp":"2023-11-14T22:13:20Z"}}}`)
	clickSig := signTestWebhook(t, "msg_click", "1700000000", clickBody)
	click := postWebhook(t, app, clickBody, "msg_click", "1700000000", clickSig)
	if click.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(click.Body)
		t.Fatalf("click status=%d body=%s", click.StatusCode, b)
	}
	var row domain.EmailEngagementEvent
	if err := db.Where("resend_event_id = ?", "msg_click").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.EventType != domain.EmailEventClicked || row.LinkURL != "https://dadiary.vn/check-in?src=email_d1" {
		t.Fatalf("click row=%+v", row)
	}
}

func TestResendWebhook_DeletedUserIsIgnored(t *testing.T) {
	app, events, db := newWebhookApp(t, testWebhookSecret)
	if err := db.Exec(`DELETE FROM users`).Error; err != nil {
		t.Fatal(err)
	}
	body := testWebhookBody()
	sig := signTestWebhook(t, "msg_gone", "1700000000", body)
	resp := postWebhook(t, app, body, "msg_gone", "1700000000", sig)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, b)
	}
	b, _ := io.ReadAll(resp.Body)
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ignored"] != true || payload["stored"] == true {
		t.Fatalf("payload=%v", payload)
	}
	n, err := events.CountByResendEventID(context.Background(), "msg_gone")
	if err != nil || n != 0 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	var users int64
	if err := db.Model(&domain.User{}).Count(&users).Error; err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Fatalf("user recreated: %d", users)
	}
}

func TestResendWebhook_MissingSecret(t *testing.T) {
	app, _, _ := newWebhookApp(t, "")
	body := testWebhookBody()
	resp := postWebhook(t, app, body, "msg_x", "1700000000", testWebhookSig)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func signTestWebhook(t *testing.T, id, ts string, body []byte) string {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString("ERERERERERERERERERERERERERERERER")
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + ts + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
