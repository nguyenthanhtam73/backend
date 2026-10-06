package repository

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestDeleteAccount_PostgresFKAndSchema runs against a real Postgres.
// SQLite does not enforce foreign keys, so a wrong delete order passes there.
//
// Skip: both DADIARY_DATABASE_URL and DATABASE_URL empty.
// CI: .github/workflows/test.yml starts Postgres 16 and sets DADIARY_DATABASE_URL,
// so this test runs on pull requests and on pushes to main. The Railway deploy
// workflow does not start Postgres and does not run tests.
func TestDeleteAccount_PostgresFKAndSchema(t *testing.T) {
	db := openAccountDeletePostgres(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`ALTER TABLE payment_orders ALTER COLUMN user_id SET NOT NULL`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`ALTER TABLE plan_change_logs ALTER COLUMN user_id SET NOT NULL`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`ALTER TABLE plan_change_logs ALTER COLUMN actor_user_id SET NOT NULL`).Error; err != nil {
		t.Fatal(err)
	}
	for _, fk := range []struct{ table, column, name, ref string }{
		{"payment_orders", "user_id", "acctdel_fk_payment_orders_user", "users(id)"},
		{"plan_change_logs", "user_id", "acctdel_fk_plan_logs_user", "users(id)"},
		{"plan_change_logs", "actor_user_id", "acctdel_fk_plan_logs_actor", "users(id)"},
		{"skin_checks", "user_id", "acctdel_fk_skin_checks_user", "users(id)"},
		{"skin_analyses", "skin_check_id", "acctdel_fk_skin_analyses_check", "skin_checks(id)"},
		{"streaks", "user_id", "acctdel_fk_streaks_user", "users(id)"},
		{"subscriptions", "user_id", "acctdel_fk_subscriptions_user", "users(id)"},
		{"refresh_sessions", "user_id", "acctdel_fk_refresh_sessions_user", "users(id)"},
		{"push_click_events", "user_id", "acctdel_fk_push_clicks_user", "users(id)"},
	} {
		if err := addRestrictFK(db, fk.table, fk.column, fk.name, fk.ref); err != nil {
			t.Fatal(err)
		}
	}

	user := &domain.User{Email: "pg-delete@dadiary.test", Username: "pgdelete", PasswordHash: "x", IsActive: true}
	other := &domain.User{Email: "pg-keep@dadiary.test", Username: "pgkeep", PasswordHash: "x", IsActive: true}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(other).Error; err != nil {
		t.Fatal(err)
	}

	invoice := "DD-" + strings.ToUpper(strings.ReplaceAll(user.ID.String(), "-", ""))[:8] + "-1710000000-PGTEST1"
	order := &domain.PaymentOrder{
		UserID:          user.ID,
		InvoiceNumber:   invoice,
		PlanTier:        domain.PlanPremium,
		BillingInterval: domain.BillingMonthly,
		AmountVND:       99000,
		Status:          domain.PaymentPaid,
		CustomData:      `{"email":"pg-delete@dadiary.test"}`,
		RawWebhook:      `{"customer":"pg-delete@dadiary.test"}`,
	}
	if err := db.Create(order).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.PaymentOpsEvent{
		ID:            uuid.New(),
		Kind:          domain.OpsKindPaymentSuccess,
		InvoiceNumber: invoice,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.PlanChangeLog{
		UserID:      user.ID,
		ActorUserID: user.ID,
		ActorEmail:  user.Email,
		FromPlan:    domain.PlanFree,
		ToPlan:      domain.PlanPremium,
		Reason:      "grant " + user.Email,
	}).Error; err != nil {
		t.Fatal(err)
	}
	check := &domain.SkinCheck{
		UserID:     user.ID,
		ImageURLs:  []byte(`[]`),
		CheckDate:  time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
		Visibility: domain.CheckVisibilityPrivate,
	}
	if err := db.Create(check).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.SkinAnalysis{
		SkinCheckID: check.ID,
		Status:      domain.AnalysisStatusCompleted,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.Streak{UserID: user.ID, CurrentStreak: 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.Subscription{
		UserID:    user.ID,
		PlanTier:  domain.PlanPremium,
		Status:    domain.SubStatusActive,
		EventType: domain.SubEventRenewed,
		Provider:  domain.SubProviderSePay,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.RefreshSession{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: strings.Repeat("ab", 32),
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.PushClickEvent{
		UserID:         user.ID,
		IdempotencyKey: "pg-click",
		ClickedAt:      time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.EmailEngagementEvent{
		ResendEventID: "pg-evt",
		ResendEmailID: "re_pg",
		UserID:        &user.ID,
		Kind:          "d1",
		EventType:     domain.EmailEventClicked,
		LinkURL:       "https://dadiary.vn/check-in?who=" + user.Email,
		OccurredAt:    time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.User{}).Where("id = ?", user.ID).Update(
		"push_opt_in_skipped_at", time.Now().UTC(),
	).Error; err != nil {
		t.Fatal(err)
	}
	otherCheck := &domain.SkinCheck{
		UserID:     other.ID,
		ImageURLs:  []byte(`[]`),
		CheckDate:  time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC),
		Visibility: domain.CheckVisibilityPrivate,
	}
	if err := db.Create(otherCheck).Error; err != nil {
		t.Fatal(err)
	}

	repo := NewUserDataRepository(db)
	if _, err := repo.DeleteAccount(t.Context(), user.ID); !errors.Is(err, ErrAccountDeletionSchema) {
		t.Fatalf("schema not ready: %v", err)
	}
	var still domain.User
	if err := db.First(&still, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("user removed before schema was ready: %v", err)
	}
	var pending domain.PaymentOrder
	if err := db.First(&pending, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if pending.UserID != user.ID || pending.InvoiceNumber != invoice || pending.CustomData == "" {
		t.Fatalf("order changed before schema was ready: %+v", pending)
	}

	if err := ApplyAccountDeletionSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := ApplyAccountDeletionSchema(db); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	if _, err := repo.DeleteAccount(t.Context(), user.ID); err != nil {
		t.Fatal(err)
	}
	var gone int64
	if err := db.Model(&domain.User{}).Where("id = ?", user.ID).Count(&gone).Error; err != nil {
		t.Fatal(err)
	}
	if gone != 0 {
		t.Fatalf("user still present")
	}
	for _, count := range []struct {
		name  string
		model any
		where string
		arg   any
	}{
		{"skin_checks", &domain.SkinCheck{}, "user_id = ?", user.ID},
		{"skin_analyses", &domain.SkinAnalysis{}, "skin_check_id = ?", check.ID},
		{"streaks", &domain.Streak{}, "user_id = ?", user.ID},
		{"subscriptions", &domain.Subscription{}, "user_id = ?", user.ID},
		{"refresh_sessions", &domain.RefreshSession{}, "user_id = ?", user.ID},
		{"push_click_events", &domain.PushClickEvent{}, "user_id = ?", user.ID},
	} {
		var n int64
		if err := db.Unscoped().Model(count.model).Where(count.where, count.arg).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%s left: %d", count.name, n)
		}
	}
	var saved domain.PaymentOrder
	if err := db.First(&saved, "id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.UserID != uuid.Nil {
		t.Fatalf("payment user_id=%s", saved.UserID)
	}
	if saved.InvoiceNumber != invoice || saved.AmountVND != 99000 {
		t.Fatalf("invoice=%s amount=%d", saved.InvoiceNumber, saved.AmountVND)
	}
	if saved.CustomData != "" || saved.RawWebhook != "" {
		t.Fatalf("payload left custom=%q webhook=%q", saved.CustomData, saved.RawWebhook)
	}
	var ops domain.PaymentOpsEvent
	if err := db.Where("invoice_number = ?", invoice).First(&ops).Error; err != nil {
		t.Fatal(err)
	}
	if ops.InvoiceNumber != invoice {
		t.Fatalf("ops invoice=%s", ops.InvoiceNumber)
	}
	var engagement domain.EmailEngagementEvent
	if err := db.Where("resend_event_id = ?", "pg-evt").First(&engagement).Error; err != nil {
		t.Fatal(err)
	}
	if engagement.UserID != nil || strings.Contains(engagement.LinkURL, user.Email) {
		t.Fatalf("engagement still linked: %+v", engagement)
	}
	var kept domain.User
	if err := db.First(&kept, "id = ?", other.ID).Error; err != nil {
		t.Fatal(err)
	}
	var keptChecks int64
	if err := db.Model(&domain.SkinCheck{}).Where("id = ?", otherCheck.ID).Count(&keptChecks).Error; err != nil {
		t.Fatal(err)
	}
	if keptChecks != 1 {
		t.Fatalf("other account's check count=%d", keptChecks)
	}
}

func TestApplyAccountDeletionSchema_NoopWhenNullable(t *testing.T) {
	db := openAccountDeletePostgres(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	forceAccountDeletionNotNull(t, db)
	if err := ApplyAccountDeletionSchema(db); err != nil {
		t.Fatal(err)
	}
	schema := currentSchema(t, db)
	lockDB := openSchemaSession(t, schema)
	tx := lockDB.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if err := tx.Exec("LOCK TABLE payment_orders, plan_change_logs IN ACCESS EXCLUSIVE MODE").Error; err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- ApplyAccountDeletionSchema(db) }()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ApplyAccountDeletionSchema blocked on an already-nullable column")
	}
}

func TestApplyAccountDeletionSchema_LockTimeoutDoesNotHang(t *testing.T) {
	db := openAccountDeletePostgres(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	forceAccountDeletionNotNull(t, db)
	schema := currentSchema(t, db)
	lockDB := openSchemaSession(t, schema)
	tx := lockDB.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if err := tx.Exec("LOCK TABLE payment_orders IN ACCESS EXCLUSIVE MODE").Error; err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	errCh := make(chan error, 1)
	go func() { errCh <- ApplyAccountDeletionSchema(db) }()
	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "lock timeout") {
			t.Fatalf("elapsed=%s err=%v", time.Since(start), err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("ApplyAccountDeletionSchema hung past lock_timeout")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("lock wait took %s", time.Since(start))
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	created := &domain.User{
		Email: "still-here@dadiary.test", Username: "stillhere", PasswordHash: "x", IsActive: true,
	}
	if err := db.Create(created).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewUserDataRepository(db)
	if _, err := repo.DeleteAccount(t.Context(), created.ID); !errors.Is(err, ErrAccountDeletionSchema) {
		t.Fatalf("delete after lock timeout: %v", err)
	}
	var still domain.User
	if err := db.First(&still, "id = ?", created.ID).Error; err != nil {
		t.Fatalf("user removed while schema is not ready: %v", err)
	}
}

func forceAccountDeletionNotNull(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, q := range []string{
		`ALTER TABLE payment_orders ALTER COLUMN user_id SET NOT NULL`,
		`ALTER TABLE plan_change_logs ALTER COLUMN user_id SET NOT NULL`,
		`ALTER TABLE plan_change_logs ALTER COLUMN actor_user_id SET NOT NULL`,
	} {
		if err := db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func currentSchema(t *testing.T, db *gorm.DB) string {
	t.Helper()
	var current struct {
		Schema string `gorm:"column:current_schema"`
	}
	if err := db.Raw("SELECT current_schema() AS current_schema").Scan(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current.Schema == "" {
		t.Fatal("empty schema")
	}
	return current.Schema
}

func openSchemaSession(t *testing.T, schema string) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("DADIARY_DATABASE_URL"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if dsn == "" {
		t.Skip("Postgres account-deletion test skipped: set DADIARY_DATABASE_URL or DATABASE_URL")
	}
	scoped, err := withSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(scoped), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("SET search_path TO " + quoteIdent(schema)).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func openAccountDeletePostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("DADIARY_DATABASE_URL"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if dsn == "" {
		t.Skip("Postgres account-deletion test skipped: set DADIARY_DATABASE_URL or DATABASE_URL")
	}
	schema := "acctdel_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if !safeSchema(schema) {
		t.Fatalf("unsafe schema %q", schema)
	}

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if err := admin.Exec("CREATE SCHEMA " + quoteIdent(schema)).Error; err != nil {
		sqlDB, _ := admin.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		t.Fatal(err)
	}

	scopedDSN, err := withSearchPath(dsn, schema)
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgres.Open(scopedDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("SET search_path TO " + quoteIdent(schema)).Error; err != nil {
		t.Fatal(err)
	}
	var current struct {
		Schema string `gorm:"column:current_schema"`
	}
	if err := db.Raw("SELECT current_schema() AS current_schema").Scan(&current).Error; err != nil {
		t.Fatal(err)
	}
	if current.Schema != schema {
		t.Fatalf("search_path is %q, want %q", current.Schema, schema)
	}

	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA " + quoteIdent(schema) + " CASCADE").Error
		adminSQL, _ := admin.DB()
		if adminSQL != nil {
			_ = adminSQL.Close()
		}
	})
	return db
}

func addRestrictFK(db *gorm.DB, table, column, name, ref string) error {
	if !safeIdent(table) || !safeIdent(column) || !safeIdent(name) || !safeRef(ref) {
		return fmt.Errorf("unsafe identifier")
	}
	q := fmt.Sprintf(
		`ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s ON DELETE RESTRICT`,
		table, name, column, ref,
	)
	if err := db.Exec(q).Error; err != nil {
		msg := err.Error()
		if strings.Contains(msg, "42710") || strings.Contains(msg, "already exists") {
			return nil
		}
		return err
	}
	return nil
}

func withSearchPath(dsn, schema string) (string, error) {
	opt := "-csearch_path=" + schema
	if strings.Contains(dsn, "://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		q := u.Query()
		if prev := q.Get("options"); prev != "" {
			opt = prev + " " + opt
		}
		q.Set("options", opt)
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	return strings.TrimSpace(dsn) + " options=" + opt, nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func safeSchema(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func safeRef(ref string) bool {
	switch ref {
	case "users(id)", "skin_checks(id)":
		return true
	default:
		return false
	}
}
