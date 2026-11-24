package repository

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/streaktime"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func TestDailyQuestClaimPaysOnce(t *testing.T) {
	db := openDailyQuestDB(t)
	user := dailyQuestUser(t, db)
	repo := NewDailyQuestRepository(db)
	now := time.Date(2026, 11, 5, 8, 0, 0, 0, streaktime.Location) // 08:00 VN, care day Nov 5
	saveQuestRoutine(t, db, user.ID, "2026-11-05", true, false)

	got, err := repo.Claim(context.Background(), user.ID, "morning", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Already || got.Granted != 8 || got.Bonus != 0 || got.Balance != 8 || got.Session != "morning" {
		t.Fatalf("first %+v", got)
	}
	again, err := repo.Claim(context.Background(), user.ID, " Morning ", now)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Already || again.Granted != 8 || again.Bonus != 0 || again.Balance != 8 {
		t.Fatalf("repeat %+v", again)
	}
	var ledger int64
	if err := db.Model(&domain.UserCoinLedger{}).Where("user_id = ?", user.ID).Count(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	if ledger != 1 {
		t.Fatalf("ledger rows %d", ledger)
	}
	var claims int64
	if err := db.Model(&domain.DailyQuestClaim{}).Where("user_id = ?", user.ID).Count(&claims).Error; err != nil {
		t.Fatal(err)
	}
	if claims != 1 {
		t.Fatalf("claim rows %d", claims)
	}
}

func TestDailyQuestClaimRejectsUnfinishedSession(t *testing.T) {
	db := openDailyQuestDB(t)
	user := dailyQuestUser(t, db)
	repo := NewDailyQuestRepository(db)
	now := time.Date(2026, 11, 5, 9, 0, 0, 0, streaktime.Location)
	if _, err := repo.Claim(context.Background(), user.ID, "morning", now); !errors.Is(err, ErrDailyQuestSteps) {
		t.Fatalf("missing routine %v", err)
	}
	saveQuestRoutine(t, db, user.ID, "2026-11-05", false, false)
	if _, err := repo.Claim(context.Background(), user.ID, "evening", now); !errors.Is(err, ErrDailyQuestSteps) {
		t.Fatalf("unticked evening %v", err)
	}
	half := dailyQuestUser(t, db)
	if err := db.Create(&domain.RoutineEntry{
		UserID:      half.ID,
		RoutineDate: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC),
		Morning:     json.RawMessage(`[{"id":"wash","title":"Rửa mặt","completed":true},{"id":"spf","title":"Chống nắng","completed":false}]`),
		Evening:     json.RawMessage(`[]`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Claim(context.Background(), half.ID, "morning", now); !errors.Is(err, ErrDailyQuestSteps) {
		t.Fatalf("half-ticked morning %v", err)
	}
	if _, err := repo.Claim(context.Background(), half.ID, "evening", now); !errors.Is(err, ErrDailyQuestSteps) {
		t.Fatalf("empty evening %v", err)
	}
	if _, err := repo.Claim(context.Background(), user.ID, "noon", now); !errors.Is(err, ErrDailyQuestSession) {
		t.Fatalf("bad session %v", err)
	}
	var balance int64
	if err := db.Model(&domain.UserCoinWallet{}).Where("user_id = ?", user.ID).Count(&balance).Error; err != nil {
		t.Fatal(err)
	}
	if balance != 0 {
		t.Fatal("unfinished claim created a wallet")
	}
}

func TestDailyQuestEveningClaimAfterMidnightUsesCareDay(t *testing.T) {
	db := openDailyQuestDB(t)
	user := dailyQuestUser(t, db)
	repo := NewDailyQuestRepository(db)
	// 01:30 on 5 Nov Vietnam is still the 4 Nov evening session.
	late := time.Date(2026, 11, 5, 1, 30, 0, 0, streaktime.Location)
	if streaktime.DateOf(late).Format("2006-01-02") != "2026-11-05" {
		t.Fatal("check-in day must be 5 Nov")
	}
	if streaktime.RoutineCareDay(late).Format("2006-01-02") != "2026-11-04" {
		t.Fatal("care day must be 4 Nov")
	}
	saveQuestRoutine(t, db, user.ID, "2026-11-04", true, true)
	// A finished routine on the civil morning must not be the chest that opens.
	saveQuestRoutine(t, db, user.ID, "2026-11-05", true, true)

	got, err := repo.Claim(context.Background(), user.ID, "evening", late)
	if err != nil {
		t.Fatal(err)
	}
	if got.Already || got.Granted != 8 || got.Bonus != 0 || got.Balance != 8 || got.Session != "evening" {
		t.Fatalf("late evening %+v", got)
	}
	var row domain.DailyQuestClaim
	if err := db.Where("user_id = ?", user.ID).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Day != "2026-11-04" || row.Session != "evening" {
		t.Fatalf("stored claim %+v", row)
	}
	// 04:00 rolls to 5 Nov. The evening chest of 4 Nov is no longer the open day.
	rolled := time.Date(2026, 11, 5, 4, 0, 0, 0, streaktime.Location)
	next, err := repo.Claim(context.Background(), user.ID, "evening", rolled)
	if err != nil {
		t.Fatal(err)
	}
	if next.Already || next.Balance != 16 || next.Bonus != 0 {
		t.Fatalf("rolled evening %+v", next)
	}
	var days []string
	if err := db.Model(&domain.DailyQuestClaim{}).Where("user_id = ?", user.ID).Order("day").Pluck("day", &days).Error; err != nil {
		t.Fatal(err)
	}
	if len(days) != 2 || days[0] != "2026-11-04" || days[1] != "2026-11-05" {
		t.Fatalf("days %v", days)
	}
}

func TestDailyQuestBonusOnceOnSecondSession(t *testing.T) {
	db := openDailyQuestDB(t)
	user := dailyQuestUser(t, db)
	repo := NewDailyQuestRepository(db)
	now := time.Date(2026, 11, 5, 21, 0, 0, 0, streaktime.Location)
	saveQuestRoutine(t, db, user.ID, "2026-11-05", true, true)

	first, err := repo.Claim(context.Background(), user.ID, "evening", now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Granted != 8 || first.Bonus != 0 || first.Balance != 8 {
		t.Fatalf("first session %+v", first)
	}
	second, err := repo.Claim(context.Background(), user.ID, "morning", now)
	if err != nil {
		t.Fatal(err)
	}
	if second.Already || second.Granted != 8 || second.Bonus != 2 || second.Balance != 18 {
		t.Fatalf("second session %+v", second)
	}
	replay, err := repo.Claim(context.Background(), user.ID, "morning", now)
	if err != nil || !replay.Already || replay.Granted != 8 || replay.Bonus != 2 || replay.Balance != 18 {
		t.Fatalf("replay %+v %v", replay, err)
	}
	other, err := repo.Claim(context.Background(), user.ID, "evening", now)
	if err != nil || !other.Already || other.Bonus != 0 || other.Balance != 18 {
		t.Fatalf("evening replay %+v %v", other, err)
	}
	var rows []domain.UserCoinLedger
	if err := db.Where("user_id = ?", user.ID).Order("reason").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("ledger %d", len(rows))
	}
	got := map[string]int64{}
	for _, row := range rows {
		got[row.Reason] += row.Amount
	}
	if got[domain.CoinReasonDailyQuestMorning] != 8 || got[domain.CoinReasonDailyQuestEvening] != 8 || got[domain.CoinReasonDailyQuestBonus] != 2 {
		t.Fatalf("reasons %#v", got)
	}
	var bonusRows int64
	if err := db.Model(&domain.UserCoinLedger{}).Where("reason = ?", domain.CoinReasonDailyQuestBonus).Count(&bonusRows).Error; err != nil {
		t.Fatal(err)
	}
	if bonusRows != 1 {
		t.Fatalf("bonus rows %d", bonusRows)
	}
}

func TestDailyQuestClaimPaysOnceUnderConcurrency(t *testing.T) {
	db := openConcurrentDailyQuestDB(t)
	user := dailyQuestUser(t, db)
	repo := NewDailyQuestRepository(db)
	now := time.Date(2026, 11, 5, 10, 0, 0, 0, streaktime.Location)
	saveQuestRoutine(t, db, user.ID, "2026-11-05", true, false)

	const n = 12
	var wg sync.WaitGroup
	type result struct {
		already bool
		balance int64
		granted int
		bonus   int
		err     error
	}
	out := make([]result, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := repo.Claim(context.Background(), user.ID, "morning", now)
			out[i] = result{already: got.Already, balance: got.Balance, granted: got.Granted, bonus: got.Bonus, err: err}
		}(i)
	}
	wg.Wait()
	paid := 0
	for _, row := range out {
		if row.err != nil {
			t.Fatalf("claim: %v", row.err)
		}
		if row.granted != 8 || row.bonus != 0 || row.balance != 8 {
			t.Fatalf("result %+v", row)
		}
		if !row.already {
			paid++
		}
	}
	if paid != 1 {
		t.Fatalf("paid %d times", paid)
	}
	var claims int64
	if err := db.Model(&domain.DailyQuestClaim{}).Where("user_id = ?", user.ID).Count(&claims).Error; err != nil {
		t.Fatal(err)
	}
	if claims != 1 {
		t.Fatalf("claim rows %d", claims)
	}
	var ledger int64
	if err := db.Model(&domain.UserCoinLedger{}).Where("user_id = ?", user.ID).Count(&ledger).Error; err != nil {
		t.Fatal(err)
	}
	if ledger != 1 {
		t.Fatalf("ledger rows %d", ledger)
	}
}

func TestDailyQuestBonusOnceUnderConcurrency(t *testing.T) {
	db := openConcurrentDailyQuestDB(t)
	user := dailyQuestUser(t, db)
	repo := NewDailyQuestRepository(db)
	now := time.Date(2026, 11, 5, 22, 0, 0, 0, streaktime.Location)
	saveQuestRoutine(t, db, user.ID, "2026-11-05", true, true)

	const n = 8
	var wg sync.WaitGroup
	errCh := make(chan error, n*2)
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := repo.Claim(context.Background(), user.ID, "morning", now); err != nil {
				errCh <- err
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := repo.Claim(context.Background(), user.ID, "evening", now); err != nil {
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
	if balance != 18 {
		t.Fatalf("balance %d", balance)
	}
	var bonus int64
	if err := db.Model(&domain.UserCoinLedger{}).
		Where("user_id = ? AND reason = ?", user.ID, domain.CoinReasonDailyQuestBonus).
		Count(&bonus).Error; err != nil {
		t.Fatal(err)
	}
	if bonus != 1 {
		t.Fatalf("bonus ledger rows %d", bonus)
	}
	var sum int64
	if err := db.Model(&domain.UserCoinLedger{}).Where("user_id = ?", user.ID).Select("COALESCE(SUM(amount),0)").Scan(&sum).Error; err != nil {
		t.Fatal(err)
	}
	if sum != 18 {
		t.Fatalf("ledger sum %d", sum)
	}
	var claims int64
	if err := db.Model(&domain.DailyQuestClaim{}).Where("user_id = ?", user.ID).Count(&claims).Error; err != nil {
		t.Fatal(err)
	}
	if claims != 2 {
		t.Fatalf("claim rows %d", claims)
	}
}

func TestDailyQuestStatusUsesCareDay(t *testing.T) {
	db := openDailyQuestDB(t)
	user := dailyQuestUser(t, db)
	repo := NewDailyQuestRepository(db)
	late := time.Date(2026, 11, 5, 2, 0, 0, 0, streaktime.Location)
	saveQuestRoutine(t, db, user.ID, "2026-11-04", true, false)
	if _, err := repo.Claim(context.Background(), user.ID, "morning", late); err != nil {
		t.Fatal(err)
	}
	status, err := repo.Status(context.Background(), user.ID, late)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Morning.Done || !status.Morning.Claimed || status.Evening.Done || status.Evening.Claimed {
		t.Fatalf("status %+v", status)
	}
}

func TestDailyQuestClaimModelMatchesMigration047(t *testing.T) {
	parsed, err := schema.Parse(&domain.DailyQuestClaim{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	var pk []string
	for _, field := range parsed.PrimaryFields {
		pk = append(pk, field.DBName)
	}
	if len(pk) != 3 || pk[0] != "user_id" || pk[1] != "day" || pk[2] != "session" {
		t.Fatalf("primary key %v", pk)
	}
	checks := parsed.ParseCheckConstraints()
	wantChecks := map[string]string{
		"daily_quest_claims_coins_check":   "coins >= 0",
		"daily_quest_claims_bonus_check":   "bonus >= 0",
		"daily_quest_claims_session_check": "session IN ('morning', 'evening')",
	}
	for name, expr := range wantChecks {
		got, ok := checks[name]
		if !ok || got.Constraint != expr {
			t.Fatalf("check %s = %#v", name, got)
		}
	}
	var fk *schema.Constraint
	for _, rel := range parsed.Relationships.Relations {
		if rel.Name != "User" {
			continue
		}
		fk = rel.ParseConstraint()
	}
	if fk == nil || fk.Name != "daily_quest_claims_user_id_fkey" || fk.OnDelete != "CASCADE" {
		t.Fatalf("fk %#v", fk)
	}
	if len(fk.ForeignKeys) != 1 || fk.ForeignKeys[0].DBName != "user_id" {
		t.Fatalf("fk columns %#v", fk.ForeignKeys)
	}
}

func openDailyQuestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:dailyquest_" + uuid.NewString() + "?mode=memory&cache=shared&_pragma=busy_timeout(5000)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := migrateDailyQuest(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func openConcurrentDailyQuestDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "daily-quest.db")
	// immediate takes the write lock at BEGIN so two transactions cannot
	// both miss the claim row and credit twice.
	dsn := "file:" + path + "?_txlock=immediate&_pragma=busy_timeout(8000)&_pragma=journal_mode(WAL)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(4)
	if err := migrateDailyQuest(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func migrateDailyQuest(db *gorm.DB) error {
	return db.AutoMigrate(
		&domain.User{},
		&domain.RoutineEntry{},
		&domain.DailyQuestClaim{},
		&domain.UserCoinWallet{},
		&domain.UserCoinLedger{},
	)
}

func dailyQuestUser(t *testing.T, db *gorm.DB) *domain.User {
	t.Helper()
	user := &domain.User{
		Email:       "quest-" + uuid.NewString() + "@example.com",
		Username:    "dq-" + uuid.NewString(),
		DisplayName: "Lan",
		IsActive:    true,
	}
	if err := NewUserRepository(db).Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	return user
}

func saveQuestRoutine(t *testing.T, db *gorm.DB, userID uuid.UUID, day string, morningDone, eveningDone bool) {
	t.Helper()
	at, err := time.Parse("2006-01-02", day)
	if err != nil {
		t.Fatal(err)
	}
	row := domain.RoutineEntry{
		UserID:      userID,
		RoutineDate: time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC),
		Morning:     json.RawMessage(questSteps(morningDone)),
		Evening:     json.RawMessage(questSteps(eveningDone)),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
}

func questSteps(done bool) string {
	flag := "false"
	if done {
		flag = "true"
	}
	return `[{"id":"wash","title":"Rửa mặt","completed":` + flag + `},{"id":"moist","title":"Dưỡng","completed":` + flag + `}]`
}
