package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/storage"
	"github.com/dadiary/backend/internal/token"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestDeleteAccount_Contract(t *testing.T) {
	app, db, store := newAccountApp(t)
	const email = "delete-me@dadiary.test"
	const password = "password1"

	access, refresh, userID := registerAccount(t, app, email, password)
	photoKey := "2026/10/03/check-in/delete-me__" + userID.String() + "/face.jpg"
	missingKey := "2026/10/03/check-in/delete-me__" + userID.String() + "/gone.jpg"
	avatarKey := "2026/10/03/check-in/delete-me__" + userID.String() + "/avatar.jpg"
	signedKey := "2026/10/03/check-in/delete-me__" + userID.String() + "/signed.jpg"
	if err := store.Save(t.Context(), photoKey, []byte("face-bytes"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(t.Context(), avatarKey, []byte("avatar-bytes"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(t.Context(), signedKey, []byte("signed-bytes"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}

	images, _ := json.Marshal([]string{
		photoKey,
		missingKey,
		"/uploads/" + signedKey + "?exp=1999999999&sig=abc",
	})
	check := &domain.SkinCheck{
		UserID:       userID,
		ImageURLs:    images,
		CheckDate:    time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
		Visibility:   domain.CheckVisibilityPrivate,
		PhotoContext: json.RawMessage(`{"images":[{"index":0,"kind":"closeup","zone":"chin"}],"skin_context":{"firmness":"firm","duration":"months","pain":"none"}}`),
	}
	if err := db.Create(check).Error; err != nil {
		t.Fatal(err)
	}
	var storedCtx string
	if err := db.Raw(`SELECT photo_context FROM skin_checks WHERE id = ?`, check.ID).Scan(&storedCtx).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(storedCtx, "chin") || !strings.Contains(storedCtx, "firm") {
		t.Fatalf("photo_context not stored with the check-in: %s", storedCtx)
	}
	if err := db.Create(&domain.Streak{UserID: userID, CurrentStreak: 3}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.User{}).Where("id = ?", userID).Updates(map[string]any{
		"plan_tier":           domain.PlanPremium,
		"subscription_status": domain.SubStatusActive,
		"avatar_url":          avatarKey,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.Subscription{
		UserID:    userID,
		PlanTier:  domain.PlanPremium,
		Status:    domain.SubStatusActive,
		EventType: domain.SubEventRenewed,
		Provider:  domain.SubProviderSePay,
	}).Error; err != nil {
		t.Fatal(err)
	}

	props, _ := json.Marshal(map[string]string{"note": email, "who": userID.String()})
	funnel := &domain.FunnelEvent{
		UserID:    &userID,
		SessionID: "acct-del",
		Event:     domain.FunnelCheckinPageView,
		Path:      "/check-in",
		Props:     props,
		ClientTS:  time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC),
		ServerTS:  time.Date(2026, 10, 3, 1, 0, 1, 0, time.UTC),
		UserAgent: "test",
	}
	if err := db.Create(funnel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.PaywallView{
		UserID:  &userID,
		Surface: domain.PaywallSurfacePricing,
		Feature: "export",
	}).Error; err != nil {
		t.Fatal(err)
	}

	short := strings.ToUpper(strings.ReplaceAll(userID.String(), "-", ""))[:8]
	invoice := "DD-" + short + "-1710000000-ABCDEF12"
	order := &domain.PaymentOrder{
		UserID:          userID,
		InvoiceNumber:   invoice,
		PlanTier:        domain.PlanPremium,
		BillingInterval: domain.BillingMonthly,
		AmountVND:       99000,
		Status:          domain.PaymentPaid,
		CustomData:      `{"user_id":"` + userID.String() + `","email":"` + email + `"}`,
		RawWebhook:      `{"customer":{"id":"` + userID.String() + `","email":"` + email + `"}}`,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatal(err)
	}
	paidAt := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Model(order).Update("paid_at", paidAt).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.PaymentOpsEvent{
		Kind:          domain.OpsKindPaymentSuccess,
		InvoiceNumber: invoice,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.EmailSendReceipt{
		UserID:        userID,
		Kind:          "d0",
		ResendEmailID: "re_delete_me",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.EmailEngagementEvent{
		ResendEventID: "evt-delete-me",
		ResendEmailID: "re_delete_me",
		UserID:        &userID,
		Kind:          "d0",
		EventType:     domain.EmailEventClicked,
		LinkURL:       "https://dadiary.vn/check-in?who=" + email + "&uid=" + userID.String(),
		OccurredAt:    time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.PushClickEvent{
		UserID:         userID,
		IdempotencyKey: "click-delete-me",
		ClickedAt:      time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.User{}).Where("id = ?", userID).Updates(map[string]any{
		"push_opt_in_skipped_at": time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.CheckInReminderFlag{
		UserID:     userID,
		Kind:       "d1",
		SignupDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		ComputedOn: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.UsageEvent{
		UserID:  userID,
		Feature: domain.UsageRoutineSuggest,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.PlanChangeLog{
		UserID:      userID,
		ActorUserID: userID,
		ActorEmail:  email,
		FromPlan:    domain.PlanFree,
		ToPlan:      domain.PlanPremium,
		Reason:      "grant " + email,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.BetaSignup{Email: email, Source: "landing_page"}).Error; err != nil {
		t.Fatal(err)
	}
	reviewKey := "2026/10/03/admin-skin-review/delete-me__" + userID.String() + "/review.jpg"
	if err := store.Save(t.Context(), reviewKey, []byte("review-bytes"), "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	reviewImages, _ := json.Marshal([]string{reviewKey})
	if err := db.Create(&domain.AdminSkinReview{
		AdminUserID: userID,
		Title:       "review",
		ImagePaths:  reviewImages,
		Analysis:    json.RawMessage(`{"summary":"ok"}`),
	}).Error; err != nil {
		t.Fatal(err)
	}

	status, raw := callJSON(t, app, http.MethodDelete, "/api/v1/me", "", `{"password":"`+password+`"}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("missing token status=%d body=%s", status, raw)
	}
	if code, _ := errorFields(t, raw); code != "unauthorized" {
		t.Fatalf("missing token code=%s", code)
	}

	status, raw = callJSON(t, app, http.MethodDelete, "/api/v1/me", access, `{}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("missing password status=%d body=%s", status, raw)
	}
	if code, _ := errorFields(t, raw); code != "invalid_password" {
		t.Fatalf("missing password code=%s", code)
	}

	status, raw = callJSON(t, app, http.MethodDelete, "/api/v1/me", access, `{"password":"wrong-password"}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("wrong password status=%d body=%s", status, raw)
	}
	if code, _ := errorFields(t, raw); code != "invalid_password" {
		t.Fatalf("wrong password code=%s", code)
	}
	if _, err := store.Read(t.Context(), photoKey); err != nil {
		t.Fatalf("photo removed after rejected password: %v", err)
	}
	var still domain.User
	if err := db.First(&still, "id = ?", userID).Error; err != nil {
		t.Fatalf("user removed after rejected password: %v", err)
	}
	if still.PlanTier != domain.PlanPremium {
		t.Fatalf("plan changed before delete: %s", still.PlanTier)
	}

	status, raw = callJSON(t, app, http.MethodDelete, "/api/v1/me", access, `{"password":"`+password+`"}`)
	if status != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", status, raw)
	}
	if len(raw) != 0 {
		t.Fatalf("delete body=%q, want empty 204", raw)
	}

	waitObjectGone(t, store, photoKey)
	waitObjectGone(t, store, avatarKey)
	waitObjectGone(t, store, reviewKey)
	waitObjectGone(t, store, signedKey)

	status, raw = callJSON(t, app, http.MethodGet, "/api/v1/me", access, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("old access status=%d body=%s", status, raw)
	}
	if code, _ := errorFields(t, raw); code != "invalid_token" {
		t.Fatalf("old access code=%s body=%s", code, raw)
	}
	status, raw = callJSON(t, app, http.MethodPost, "/api/v1/auth/refresh", "", `{"refresh_token":"`+refresh+`"}`)
	if status != http.StatusUnauthorized {
		t.Fatalf("old refresh status=%d body=%s", status, raw)
	}

	var users int64
	if err := db.Unscoped().Model(&domain.User{}).Where("id = ? OR LOWER(email) = ?", userID, email).Count(&users).Error; err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Fatalf("user row still present: %d", users)
	}
	var checks int64
	if err := db.Unscoped().Model(&domain.SkinCheck{}).Where("user_id = ?", userID).Count(&checks).Error; err != nil {
		t.Fatal(err)
	}
	if checks != 0 {
		t.Fatalf("skin checks left: %d", checks)
	}
	var ctxLeft int64
	if err := db.Raw(`SELECT COUNT(*) FROM skin_checks WHERE user_id = ? AND photo_context IS NOT NULL`, userID).Scan(&ctxLeft).Error; err != nil {
		t.Fatal(err)
	}
	if ctxLeft != 0 {
		t.Fatalf("photo_context rows left after account deletion: %d", ctxLeft)
	}
	var streaks int64
	if err := db.Model(&domain.Streak{}).Where("user_id = ?", userID).Count(&streaks).Error; err != nil {
		t.Fatal(err)
	}
	if streaks != 0 {
		t.Fatalf("streak left: %d", streaks)
	}
	var subs int64
	if err := db.Unscoped().Model(&domain.Subscription{}).Where("user_id = ?", userID).Count(&subs).Error; err != nil {
		t.Fatal(err)
	}
	if subs != 0 {
		t.Fatalf("subscriptions left: %d", subs)
	}
	var reviews int64
	if err := db.Model(&domain.AdminSkinReview{}).Where("admin_user_id = ?", userID).Count(&reviews).Error; err != nil {
		t.Fatal(err)
	}
	if reviews != 0 {
		t.Fatalf("admin reviews left: %d", reviews)
	}
	var betas int64
	if err := db.Model(&domain.BetaSignup{}).Where("LOWER(email) = ?", email).Count(&betas).Error; err != nil {
		t.Fatal(err)
	}
	if betas != 0 {
		t.Fatalf("beta signup left: %d", betas)
	}

	var ev domain.FunnelEvent
	if err := db.First(&ev, "id = ?", funnel.ID).Error; err != nil {
		t.Fatal(err)
	}
	if ev.UserID != nil {
		t.Fatalf("funnel user_id=%v", ev.UserID)
	}
	if strings.Contains(strings.ToLower(string(ev.Props)), email) || strings.Contains(string(ev.Props), userID.String()) {
		t.Fatalf("funnel props still identify the person: %s", ev.Props)
	}
	var view domain.PaywallView
	if err := db.Where("surface = ?", domain.PaywallSurfacePricing).First(&view).Error; err != nil {
		t.Fatal(err)
	}
	if view.UserID != nil {
		t.Fatalf("paywall user_id=%v", view.UserID)
	}

	var saved domain.PaymentOrder
	if err := db.First(&saved, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.UserID != uuid.Nil {
		t.Fatalf("payment user_id=%s", saved.UserID)
	}
	if saved.AmountVND != 99000 {
		t.Fatalf("amount=%d", saved.AmountVND)
	}
	if saved.PaidAt == nil || !saved.PaidAt.Equal(paidAt) {
		t.Fatalf("paid_at=%v", saved.PaidAt)
	}
	if saved.CustomData != "" || saved.RawWebhook != "" {
		t.Fatalf("payment payload left custom=%q webhook=%q", saved.CustomData, saved.RawWebhook)
	}
	if saved.InvoiceNumber != invoice {
		t.Fatalf("invoice changed: got %s want %s", saved.InvoiceNumber, invoice)
	}
	var ops domain.PaymentOpsEvent
	if err := db.Where("kind = ?", domain.OpsKindPaymentSuccess).First(&ops).Error; err != nil {
		t.Fatal(err)
	}
	if ops.InvoiceNumber != invoice {
		t.Fatalf("ops invoice=%s want %s", ops.InvoiceNumber, invoice)
	}

	var receipt domain.EmailSendReceipt
	if err := db.Where("kind = ?", "d0").First(&receipt).Error; err != nil {
		t.Fatal(err)
	}
	if receipt.UserID == uuid.Nil || receipt.UserID == userID {
		t.Fatalf("email receipt user_id=%s", receipt.UserID)
	}
	var engagement domain.EmailEngagementEvent
	if err := db.Where("resend_event_id = ?", "evt-delete-me").First(&engagement).Error; err != nil {
		t.Fatal(err)
	}
	if engagement.UserID != nil {
		t.Fatalf("engagement user_id=%v", engagement.UserID)
	}
	if strings.Contains(strings.ToLower(engagement.LinkURL), email) || strings.Contains(engagement.LinkURL, userID.String()) {
		t.Fatalf("engagement link still identifies the person: %s", engagement.LinkURL)
	}
	var clicks int64
	if err := db.Model(&domain.PushClickEvent{}).Where("user_id = ?", userID).Count(&clicks).Error; err != nil {
		t.Fatal(err)
	}
	if clicks != 0 {
		t.Fatalf("push clicks left: %d", clicks)
	}
	var flags int64
	if err := db.Unscoped().Model(&domain.CheckInReminderFlag{}).Where("user_id = ?", userID).Count(&flags).Error; err != nil {
		t.Fatal(err)
	}
	if flags != 0 {
		t.Fatalf("reminder flags left: %d", flags)
	}
	var usage domain.UsageEvent
	if err := db.First(&usage).Error; err != nil {
		t.Fatal(err)
	}
	if usage.UserID == uuid.Nil || usage.UserID == userID {
		t.Fatalf("usage user_id=%s", usage.UserID)
	}
	var planLog domain.PlanChangeLog
	if err := db.First(&planLog).Error; err != nil {
		t.Fatal(err)
	}
	if planLog.UserID != uuid.Nil || planLog.ActorUserID != uuid.Nil {
		t.Fatalf("plan log ids user=%s actor=%s", planLog.UserID, planLog.ActorUserID)
	}
	if planLog.ActorEmail != "" || strings.Contains(strings.ToLower(planLog.Reason), email) {
		t.Fatalf("plan log still names the person email=%q reason=%q", planLog.ActorEmail, planLog.Reason)
	}
	if planLog.ToPlan != domain.PlanPremium {
		t.Fatalf("plan log dropped the change: %+v", planLog)
	}

	status, raw = callJSON(t, app, http.MethodPost, "/api/v1/auth/register", "", `{"email":"`+email+`","password":"`+password+`"}`)
	if status != http.StatusCreated {
		t.Fatalf("re-register status=%d body=%s", status, raw)
	}
	status, raw = callJSON(t, app, http.MethodGet, "/api/v1/me", access, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("old token after re-register status=%d body=%s", status, raw)
	}
}

func TestDeleteAccount_RateLimitsPasswordGuesses(t *testing.T) {
	app, db, _ := newAccountApp(t)
	access, _, userID := registerAccount(t, app, "limit-me@dadiary.test", "password1")
	for i := 0; i < 5; i++ {
		status, raw := callJSON(t, app, http.MethodDelete, "/api/v1/me", access, `{"password":"wrong-password"}`)
		if status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status=%d body=%s", i+1, status, raw)
		}
		if code, _ := errorFields(t, raw); code != "invalid_password" {
			t.Fatalf("attempt %d code=%s", i+1, code)
		}
	}
	status, raw := callJSON(t, app, http.MethodDelete, "/api/v1/me", access, `{"password":"wrong-password"}`)
	if status != http.StatusTooManyRequests {
		t.Fatalf("rate limit status=%d body=%s", status, raw)
	}
	code, msg := errorFields(t, raw)
	if code != "rate_limited" {
		t.Fatalf("rate limit code=%s body=%s", code, raw)
	}
	if !strings.Contains(msg, "15 minutes") || !strings.Contains(msg, "15 phút") {
		t.Fatalf("rate limit message=%q", msg)
	}
	if strings.Contains(msg, "a minute") || strings.Contains(msg, "1 phút") {
		t.Fatalf("rate limit message still says one minute: %q", msg)
	}
	var still domain.User
	if err := db.First(&still, "id = ?", userID).Error; err != nil {
		t.Fatal(err)
	}
}

func newAccountApp(t *testing.T) (*fiber.App, *gorm.DB, storage.Storage) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:account_delete_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.RefreshSession{},
		&domain.SkinCheck{},
		&domain.SkinAnalysis{},
		&domain.SkinProfile{},
		&domain.RoutineEntry{},
		&domain.SkincareProduct{},
		&domain.AIUserFeedback{},
		&domain.Feedback{},
		&domain.AffiliateClick{},
		&domain.ProgressLog{},
		&domain.PushSubscription{},
		&domain.PushSendReceipt{},
		&domain.CaptureReminderClaim{},
		&domain.CheckInReminderFlag{},
		&domain.RoutineSuggestJob{},
		&domain.Streak{},
		&domain.UserUsage{},
		&domain.Subscription{},
		&domain.AdminSkinReview{},
		&domain.BetaSignup{},
		&domain.FunnelEvent{},
		&domain.PaywallView{},
		&domain.EmailSendReceipt{},
		&domain.EmailEngagementEvent{},
		&domain.PushClickEvent{},
		&domain.UsageEvent{},
		&domain.PaymentOrder{},
		&domain.PaymentOpsEvent{},
		&domain.PlanChangeLog{},
	); err != nil {
		t.Fatal(err)
	}
	tok, err := token.NewService(config.JWTConfig{
		Secret:     "test-secret-for-account-delete-32",
		AccessTTL:  24 * time.Hour,
		RefreshTTL: 48 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Upload: config.UploadConfig{Dir: t.TempDir()}}
	store, err := storage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	Router(app, cfg, db, tok, store)
	return app, db, store
}

func registerAccount(t *testing.T, app *fiber.App, email, password string) (access, refresh string, userID uuid.UUID) {
	t.Helper()
	status, raw := callJSON(t, app, http.MethodPost, "/api/v1/auth/register", "", `{"email":"`+email+`","password":"`+password+`","display_name":"Ada"}`)
	if status != http.StatusCreated {
		t.Fatalf("register status=%d body=%s", status, raw)
	}
	var env struct {
		Data struct {
			Tokens struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
			} `json:"tokens"`
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	id, err := uuid.Parse(env.Data.User.ID)
	if err != nil || id == uuid.Nil {
		t.Fatalf("user id %q: %v", env.Data.User.ID, err)
	}
	if env.Data.Tokens.AccessToken == "" || env.Data.Tokens.RefreshToken == "" {
		t.Fatalf("tokens missing: %s", raw)
	}
	return env.Data.Tokens.AccessToken, env.Data.Tokens.RefreshToken, id
}

func waitObjectGone(t *testing.T, store storage.Storage, key string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, err := store.Read(t.Context(), key)
		if err != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s still on disk", key)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func callJSON(t *testing.T, app *fiber.App, method, path, access, body string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if access != "" {
		req.Header.Set("Authorization", "Bearer "+access)
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
	return res.StatusCode, raw
}
