package repository

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestDailyQuestClaimOnceAndBonus_Postgres runs the chest claim against
// Postgres, the database production boots. Parallel claims of one session
// credit once. Parallel claims of both sessions add the bonus once.
func TestDailyQuestClaimOnceAndBonus_Postgres(t *testing.T) {
	db := openDailyQuestPostgres(t)
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.RoutineEntry{},
		&domain.DailyQuestClaim{},
		&domain.UserCoinWallet{},
		&domain.UserCoinLedger{},
	); err != nil {
		t.Fatal(err)
	}
	assertDailyQuestPostgresSchema(t, db)

	user := dailyQuestUser(t, db)
	repo := NewDailyQuestRepository(db)
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, streaktime.Location)
	saveQuestRoutine(t, db, user.ID, "2026-11-05", true, true)

	const n = 12
	var wg sync.WaitGroup
	errCh := make(chan error, n*2)
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := repo.Claim(context.Background(), user.ID, domain.DailyQuestSessionMorning, now); err != nil {
				errCh <- err
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := repo.Claim(context.Background(), user.ID, domain.DailyQuestSessionEvening, now); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	var balance int64
	if err := db.Model(&domain.UserCoinWallet{}).Where("user_id = ?", user.ID).Pluck("balance", &balance).Error; err != nil {
		t.Fatal(err)
	}
	if balance != int64(domain.DailyQuestMorningCoins+domain.DailyQuestEveningCoins+domain.DailyQuestBonusCoins) {
		t.Fatalf("balance %d", balance)
	}
	type tally struct {
		Reason string
		N      int64
		Sum    int64
	}
	var rows []tally
	if err := db.Model(&domain.UserCoinLedger{}).
		Select("reason, COUNT(*) AS n, COALESCE(SUM(amount), 0) AS sum").
		Where("user_id = ?", user.ID).
		Group("reason").
		Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	got := map[string]tally{}
	for _, row := range rows {
		got[row.Reason] = row
	}
	for _, reason := range []string{domain.CoinReasonDailyQuestMorning, domain.CoinReasonDailyQuestEvening, domain.CoinReasonDailyQuestBonus} {
		row, ok := got[reason]
		if !ok || row.N != 1 {
			t.Fatalf("ledger %s %#v", reason, got)
		}
	}
	if got[domain.CoinReasonDailyQuestMorning].Sum != int64(domain.DailyQuestMorningCoins) ||
		got[domain.CoinReasonDailyQuestEvening].Sum != int64(domain.DailyQuestEveningCoins) ||
		got[domain.CoinReasonDailyQuestBonus].Sum != int64(domain.DailyQuestBonusCoins) {
		t.Fatalf("amounts %#v", got)
	}
	var claims int64
	if err := db.Model(&domain.DailyQuestClaim{}).Where("user_id = ?", user.ID).Count(&claims).Error; err != nil {
		t.Fatal(err)
	}
	if claims != 2 {
		t.Fatalf("claim rows %d", claims)
	}
}

func assertDailyQuestPostgresSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	type constraint struct {
		Name string
		Type string
		Def  string
	}
	var rows []constraint
	if err := db.Raw(`
		SELECT conname AS name, contype AS type, pg_get_constraintdef(oid) AS def
		FROM pg_constraint
		WHERE conrelid = 'daily_quest_claims'::regclass
	`).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	defs := map[string]string{}
	var pk string
	for _, row := range rows {
		compact := strings.ToLower(strings.ReplaceAll(row.Def, " ", ""))
		defs[row.Name] = compact
		if row.Type == "p" {
			pk = compact
		}
	}
	if !strings.Contains(pk, "primarykey(user_id,day,session)") {
		t.Fatalf("primary key %q in %#v", pk, defs)
	}
	fk, ok := defs["daily_quest_claims_user_id_fkey"]
	if !ok || !strings.Contains(fk, "foreignkey(user_id)referencesusers(id)") || !strings.Contains(fk, "ondeletecascade") {
		t.Fatalf("foreign key %#v", defs)
	}
	for _, name := range []string{
		"daily_quest_claims_coins_check",
		"daily_quest_claims_bonus_check",
		"daily_quest_claims_session_check",
	} {
		if _, ok := defs[name]; !ok {
			t.Fatalf("missing %s in %#v", name, defs)
		}
	}
	if !strings.Contains(defs["daily_quest_claims_coins_check"], "coins>=0") ||
		!strings.Contains(defs["daily_quest_claims_bonus_check"], "bonus>=0") ||
		!strings.Contains(defs["daily_quest_claims_session_check"], "morning") ||
		!strings.Contains(defs["daily_quest_claims_session_check"], "evening") {
		t.Fatalf("checks %#v", defs)
	}

	user := dailyQuestUser(t, db)
	if err := db.Exec(
		`INSERT INTO daily_quest_claims (user_id, day, session, coins, bonus, created_at) VALUES (?, '2026-11-01', 'morning', -1, 0, NOW())`,
		user.ID,
	).Error; err == nil {
		t.Fatal("negative coins accepted")
	}
	if err := db.Exec(
		`INSERT INTO daily_quest_claims (user_id, day, session, coins, bonus, created_at) VALUES (?, '2026-11-01', 'noon', 1, 0, NOW())`,
		user.ID,
	).Error; err == nil {
		t.Fatal("unknown session accepted")
	}
	if err := db.Exec(
		`INSERT INTO daily_quest_claims (user_id, day, session, coins, bonus, created_at) VALUES (?, '2026-11-01', 'morning', 1, 0, NOW())`,
		uuid.New(),
	).Error; err == nil {
		t.Fatal("missing user accepted")
	}
}

func openDailyQuestPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("DADIARY_DATABASE_URL"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if dsn == "" {
		t.Skip("DADIARY_DATABASE_URL not set")
	}
	schemaName := "dq_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if !safeSchema(schemaName) {
		t.Fatalf("unsafe schema %q", schemaName)
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	if err := admin.Exec("CREATE SCHEMA " + quoteIdent(schemaName)).Error; err != nil {
		sqlDB, _ := admin.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		t.Fatal(err)
	}
	scoped, err := withSearchPath(dsn, schemaName)
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
	sqlDB.SetMaxOpenConns(8)
	if err := db.Exec("SET search_path TO " + quoteIdent(schemaName)).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA " + quoteIdent(schemaName) + " CASCADE").Error
		adminSQL, _ := admin.DB()
		if adminSQL != nil {
			_ = adminSQL.Close()
		}
	})
	return db
}
