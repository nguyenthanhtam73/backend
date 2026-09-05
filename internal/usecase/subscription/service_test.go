package subscription

import (
	"context"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupSubDB(t *testing.T) (*gorm.DB, *repository.GormUserRepository, *repository.SubscriptionRepository, *repository.PlanChangeLogRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:sub_lc_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.Subscription{}, &domain.PlanChangeLog{}); err != nil {
		t.Fatal(err)
	}
	users := repository.NewUserRepository(db)
	subs := repository.NewSubscriptionRepository(db)
	logs := repository.NewPlanChangeLogRepository(db)
	return db, users, subs, logs
}

func createFreeUser(t *testing.T, users *repository.GormUserRepository) *domain.User {
	t.Helper()
	u := &domain.User{
		Email:              uuid.New().String() + "@test.com",
		Username:           "u_" + uuid.New().String()[:8],
		PlanTier:           domain.PlanFree,
		SubscriptionStatus: domain.SubStatusNone,
		IsActive:           true,
	}
	if err := users.Create(context.Background(), u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestStartTrial_Cancel_Renewal_CheckActive(t *testing.T) {
	db, users, subs, logs := setupSubDB(t)
	svc := NewService(db, users, subs, logs, 7, 3)
	ctx := context.Background()

	u := createFreeUser(t, users)

	// --- StartTrial ---
	plan, err := svc.StartTrial(ctx, u.ID, domain.PlanPremium)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Active || plan.Status != domain.SubStatusTrialing {
		t.Fatalf("trial plan: %+v", plan)
	}
	// DaysLeft counts until access ends (trial + grace).
	if plan.DaysLeft < 9 || plan.DaysLeft > 10 {
		t.Fatalf("trial days_left=%d want ~10 (7 trial + 3 grace)", plan.DaysLeft)
	}
	if plan.EligibleForTrial {
		t.Fatal("should not be eligible after starting trial")
	}
	if plan.TrialEndsAt == nil {
		t.Fatal("trial_ends_at required")
	}

	// Second trial rejected.
	if _, err := svc.StartTrial(ctx, u.ID, domain.PlanPremium); err != ErrNotEligible {
		t.Fatalf("second trial: %v", err)
	}

	// --- Cancel ---
	canceled, err := svc.CancelSubscription(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if canceled.Status != domain.SubStatusCanceled || !canceled.CancelAtPeriodEnd {
		t.Fatalf("canceled: %+v", canceled)
	}
	if !canceled.Active {
		t.Fatal("canceled user keeps access until grace ends")
	}
	if _, err := svc.CancelSubscription(ctx, u.ID); err != ErrAlreadyCanceled {
		t.Fatalf("double cancel: %v", err)
	}

	// --- Renew (SePay) clears cancel + extends ---
	beforeExpire := *canceled.PlanExpiresAt
	renewed, err := svc.HandleRenewal(ctx, RenewalInput{
		UserID:          u.ID,
		PlanTier:        domain.PlanPremium,
		BillingInterval: domain.BillingMonthly,
		ExternalRef:     "DD-TEST-INV-1",
		Provider:        domain.SubProviderSePay,
	})
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Status != domain.SubStatusActive || renewed.CanceledAt != nil {
		t.Fatalf("renewed: %+v", renewed)
	}
	if renewed.PlanExpiresAt == nil || !renewed.PlanExpiresAt.After(beforeExpire) {
		t.Fatalf("expiry not extended: before=%s after=%v", beforeExpire, renewed.PlanExpiresAt)
	}

	// Idempotent renew with same external_ref.
	again, err := svc.HandleRenewal(ctx, RenewalInput{
		UserID:          u.ID,
		PlanTier:        domain.PlanPremium,
		BillingInterval: domain.BillingMonthly,
		ExternalRef:     "DD-TEST-INV-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !again.PlanExpiresAt.Equal(*renewed.PlanExpiresAt) {
		t.Fatalf("idempotent renew changed expiry: %v vs %v", again.PlanExpiresAt, renewed.PlanExpiresAt)
	}

	snap, err := svc.CheckActivePlan(ctx, u.ID)
	if err != nil || snap == nil || !snap.Active {
		t.Fatalf("CheckActivePlan: %+v err=%v", snap, err)
	}
	if snap.GraceDaysConfigured != 3 || snap.TrialDaysConfigured != 7 {
		t.Fatalf("config echo: %+v", snap)
	}

	hist, err := subs.ListForUser(ctx, u.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) < 3 {
		t.Fatalf("history rows=%d want >= 3", len(hist))
	}
	active := 0
	for i := range hist {
		if hist[i].Status == domain.SubStatusActive {
			active++
			if hist[i].PeriodEndsAt == nil || !hist[i].PeriodEndsAt.After(time.Now().UTC()) {
				t.Fatalf("active row has ended period: %+v", hist[i])
			}
		}
	}
	if active != 1 {
		t.Fatalf("active history rows=%d want 1 after renewal", active)
	}
}

func TestDowngradePastGrace(t *testing.T) {
	db, users, subs, logs := setupSubDB(t)
	svc := NewService(db, users, subs, logs, 7, 3)
	ctx := context.Background()

	pastGrace := time.Now().UTC().Add(-4 * 24 * time.Hour)
	inGrace := time.Now().UTC().Add(-time.Hour)

	expired := &domain.User{
		Email:              "e1@test.com",
		Username:           "e1",
		PlanTier:           domain.PlanPremium,
		PlanExpiresAt:      &pastGrace,
		SubscriptionStatus: domain.SubStatusPastDue,
		IsActive:           true,
	}
	graceUser := &domain.User{
		Email:              "e2@test.com",
		Username:           "e2",
		PlanTier:           domain.PlanPremium,
		PlanExpiresAt:      &inGrace,
		SubscriptionStatus: domain.SubStatusPastDue,
		IsActive:           true,
	}
	for _, u := range []*domain.User{expired, graceUser} {
		if err := users.Create(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	if err := subs.Create(ctx, &domain.Subscription{
		UserID:       expired.ID,
		PlanTier:     domain.PlanPremium,
		Status:       domain.SubStatusActive,
		EventType:    domain.SubEventRenewed,
		Provider:     domain.SubProviderSePay,
		PeriodEndsAt: &pastGrace,
	}); err != nil {
		t.Fatal(err)
	}
	// Grace is a property of a billed period, not a floating users.plan_expires_at.
	if err := subs.Create(ctx, &domain.Subscription{
		UserID:       graceUser.ID,
		PlanTier:     domain.PlanPremium,
		Status:       domain.SubStatusPastDue,
		EventType:    domain.SubEventRenewed,
		Provider:     domain.SubProviderSePay,
		PeriodEndsAt: &inGrace,
	}); err != nil {
		t.Fatal(err)
	}

	n, err := svc.DowngradePastGrace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("downgraded=%d want 1", n)
	}

	got, _ := users.GetByID(ctx, expired.ID)
	if got.PlanTier != domain.PlanFree || got.SubscriptionStatus != domain.SubStatusExpired {
		t.Fatalf("expired not downgraded: %+v", got)
	}
	still, _ := users.GetByID(ctx, graceUser.ID)
	if still.PlanTier != domain.PlanPremium {
		t.Fatalf("grace user downgraded early: %+v", still)
	}

	// In-grace user still CheckActivePlan=true + InGrace.
	snap, err := svc.CheckActivePlan(ctx, graceUser.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Active || !snap.InGrace || snap.Status != domain.SubStatusPastDue {
		t.Fatalf("grace snapshot: %+v", snap)
	}

	hist, err := subs.ListForUser(ctx, expired.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	openActive := 0
	for i := range hist {
		if hist[i].Status == domain.SubStatusActive {
			openActive++
		}
	}
	if openActive != 0 {
		t.Fatalf("expired user still has active subscription rows: %+v", hist)
	}
}

func TestReconcileBillingState_ExpiresOverdueAndSyncsUsers(t *testing.T) {
	db, users, subs, logs := setupSubDB(t)
	svc := NewService(db, users, subs, logs, 7, 3)
	ctx := context.Background()
	now := time.Now().UTC()

	// Production shape 1: user already free/expired, leftover active subscription.
	staleEnded := now.Add(-10 * 24 * time.Hour)
	alreadyFree := &domain.User{
		Email:              "stale-free@test.com",
		Username:           "stale_free",
		PlanTier:           domain.PlanFree,
		SubscriptionStatus: domain.SubStatusExpired,
		IsActive:           true,
	}
	if err := users.Create(ctx, alreadyFree); err != nil {
		t.Fatal(err)
	}
	if err := subs.Create(ctx, &domain.Subscription{
		UserID:       alreadyFree.ID,
		PlanTier:     domain.PlanPremium,
		Status:       domain.SubStatusActive,
		EventType:    domain.SubEventRenewed,
		Provider:     domain.SubProviderSePay,
		PeriodEndsAt: &staleEnded,
	}); err != nil {
		t.Fatal(err)
	}

	// Production shape 2: still premium/active on users, period ended yesterday (in grace).
	endedYesterday := now.Add(-24 * time.Hour)
	inGrace := &domain.User{
		Email:              "in-grace@test.com",
		Username:           "in_grace",
		PlanTier:           domain.PlanPremium,
		PlanExpiresAt:      &endedYesterday,
		SubscriptionStatus: domain.SubStatusActive,
		IsActive:           true,
	}
	if err := users.Create(ctx, inGrace); err != nil {
		t.Fatal(err)
	}
	if err := subs.Create(ctx, &domain.Subscription{
		UserID:       inGrace.ID,
		PlanTier:     domain.PlanPremium,
		Status:       domain.SubStatusActive,
		EventType:    domain.SubEventRenewed,
		Provider:     domain.SubProviderSePay,
		PeriodEndsAt: &endedYesterday,
	}); err != nil {
		t.Fatal(err)
	}

	// Production shape 3: lifetime-looking grant + overdue billed row.
	endedWeeksAgo := now.Add(-15 * 24 * time.Hour)
	lifetime := &domain.User{
		Email:              "lifetime@test.com",
		Username:           "lifetime",
		PlanTier:           domain.PlanPremium,
		PlanExpiresAt:      nil,
		SubscriptionStatus: domain.SubStatusActive,
		IsActive:           true,
	}
	if err := users.Create(ctx, lifetime); err != nil {
		t.Fatal(err)
	}
	if err := subs.Create(ctx, &domain.Subscription{
		UserID:       lifetime.ID,
		PlanTier:     domain.PlanPremium,
		Status:       domain.SubStatusActive,
		EventType:    domain.SubEventRenewed,
		Provider:     domain.SubProviderSePay,
		PeriodEndsAt: &endedWeeksAgo,
	}); err != nil {
		t.Fatal(err)
	}

	// Current paid period — must not be touched.
	future := now.Add(20 * 24 * time.Hour)
	current := &domain.User{
		Email:              "current@test.com",
		Username:           "current",
		PlanTier:           domain.PlanPremium,
		PlanExpiresAt:      &future,
		SubscriptionStatus: domain.SubStatusActive,
		IsActive:           true,
	}
	if err := users.Create(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err := subs.Create(ctx, &domain.Subscription{
		UserID:       current.ID,
		PlanTier:     domain.PlanPremium,
		Status:       domain.SubStatusActive,
		EventType:    domain.SubEventRenewed,
		Provider:     domain.SubProviderSePay,
		PeriodEndsAt: &future,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.ReconcileBillingState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.SubscriptionRowsClosed < 3 {
		t.Fatalf("closed=%d want >= 3 overdue rows", res.SubscriptionRowsClosed)
	}
	if res.UsersExpired != 0 {
		t.Fatalf("users_expired=%d want 0 (already-free + in-grace + lifetime)", res.UsersExpired)
	}
	if res.UsersMarkedPastDue != 1 {
		t.Fatalf("users_marked_past_due=%d want 1", res.UsersMarkedPastDue)
	}

	assertSubStatus := func(userID uuid.UUID, want domain.SubscriptionStatus) {
		t.Helper()
		rows, err := subs.ListForUser(ctx, userID, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			t.Fatalf("no subscription rows for %s", userID)
		}
		if rows[0].Status != want && !hasStatus(rows, want) {
			t.Fatalf("user %s subscription statuses=%v want %s", userID, statusesOf(rows), want)
		}
		for i := range rows {
			if rows[i].Status == domain.SubStatusActive && rows[i].PeriodEndsAt != nil && !rows[i].PeriodEndsAt.After(now) {
				t.Fatalf("active row past period_ends_at: %+v", rows[i])
			}
		}
	}
	assertSubStatus(alreadyFree.ID, domain.SubStatusExpired)
	assertSubStatus(inGrace.ID, domain.SubStatusPastDue)
	assertSubStatus(lifetime.ID, domain.SubStatusExpired)
	assertSubStatus(current.ID, domain.SubStatusActive)

	gotFree, _ := users.GetByID(ctx, alreadyFree.ID)
	if gotFree.PlanTier != domain.PlanFree || gotFree.SubscriptionStatus != domain.SubStatusExpired {
		t.Fatalf("already-free user changed: %+v", gotFree)
	}
	gotGrace, _ := users.GetByID(ctx, inGrace.ID)
	if gotGrace.PlanTier != domain.PlanPremium || gotGrace.SubscriptionStatus != domain.SubStatusPastDue {
		t.Fatalf("in-grace user: %+v", gotGrace)
	}
	if gotGrace.PlanExpiresAt == nil || !gotGrace.PlanExpiresAt.Equal(endedYesterday) {
		t.Fatalf("in-grace expiry mutated: %v", gotGrace.PlanExpiresAt)
	}
	gotLife, _ := users.GetByID(ctx, lifetime.ID)
	if gotLife.PlanTier != domain.PlanPremium || gotLife.PlanExpiresAt != nil {
		t.Fatalf("lifetime grant revoked: %+v", gotLife)
	}
	gotCur, _ := users.GetByID(ctx, current.ID)
	if gotCur.PlanTier != domain.PlanPremium || gotCur.SubscriptionStatus != domain.SubStatusActive {
		t.Fatalf("current paid user mutated: %+v", gotCur)
	}

	activeSubs, err := subs.CountActiveInPeriod(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	activeUsers, err := users.CountActivePremiumInPeriod(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if activeSubs != activeUsers || activeSubs != 1 {
		t.Fatalf("active-in-period mismatch: subs=%d users=%d want 1", activeSubs, activeUsers)
	}

	again, err := svc.ReconcileBillingState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.SubscriptionRowsClosed != 0 || again.UsersExpired != 0 || again.UsersMarkedPastDue != 0 {
		t.Fatalf("second reconcile not idempotent: %+v", again)
	}
}

func TestReconcileBillingState_DowngradesPastGracePaidUser(t *testing.T) {
	db, users, subs, logs := setupSubDB(t)
	svc := NewService(db, users, subs, logs, 7, 3)
	ctx := context.Background()

	pastGrace := time.Now().UTC().Add(-5 * 24 * time.Hour)
	u := &domain.User{
		Email:              "past-grace@test.com",
		Username:           "past_grace",
		PlanTier:           domain.PlanPremium,
		PlanExpiresAt:      &pastGrace,
		SubscriptionStatus: domain.SubStatusActive,
		IsActive:           true,
	}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := subs.Create(ctx, &domain.Subscription{
		UserID:       u.ID,
		PlanTier:     domain.PlanPremium,
		Status:       domain.SubStatusActive,
		EventType:    domain.SubEventRenewed,
		Provider:     domain.SubProviderSePay,
		PeriodEndsAt: &pastGrace,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.ReconcileBillingState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.UsersExpired != 1 {
		t.Fatalf("users_expired=%d want 1", res.UsersExpired)
	}
	if res.SubscriptionRowsClosed != 1 {
		t.Fatalf("rows_closed=%d want 1", res.SubscriptionRowsClosed)
	}

	got, _ := users.GetByID(ctx, u.ID)
	if got.PlanTier != domain.PlanFree || got.SubscriptionStatus != domain.SubStatusExpired {
		t.Fatalf("not downgraded: %+v", got)
	}
	rows, _ := subs.ListForUser(ctx, u.ID, 10)
	if hasStatus(rows, domain.SubStatusActive) {
		t.Fatalf("active row remains: %+v", rows)
	}

	again, err := svc.ReconcileBillingState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.UsersExpired != 0 || again.HistoryEventsAppended != 0 {
		t.Fatalf("repeat downgrade: %+v", again)
	}
}

func TestReconcileBillingState_ExpiresOrphanPremiumWithoutEvidence(t *testing.T) {
	db, users, subs, logs := setupSubDB(t)
	svc := NewService(db, users, subs, logs, 7, 3)
	ctx := context.Background()
	now := time.Now().UTC()
	future := now.Add(20 * 24 * time.Hour)

	orphan := &domain.User{
		Email:              "orphan-premium@test.com",
		Username:           "orphan_prem",
		PlanTier:           domain.PlanPremium,
		PlanExpiresAt:      &future,
		SubscriptionStatus: domain.SubStatusActive,
		IsActive:           true,
	}
	if err := users.Create(ctx, orphan); err != nil {
		t.Fatal(err)
	}

	res, err := svc.ReconcileBillingState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.UsersExpired != 1 {
		t.Fatalf("users_expired=%d want 1 (dated premium, no covering sub/order)", res.UsersExpired)
	}

	got, _ := users.GetByID(ctx, orphan.ID)
	if got.PlanTier != domain.PlanFree || got.SubscriptionStatus != domain.SubStatusExpired {
		t.Fatalf("orphan not demoted: %+v", got)
	}
	if got.PlanExpiresAt != nil {
		t.Fatalf("orphan expiry not cleared: %v", got.PlanExpiresAt)
	}

	again, err := svc.ReconcileBillingState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.UsersExpired != 0 {
		t.Fatalf("second expire: %+v", again)
	}
}

func TestReconcileBillingState_KeepsUserWhenPaidOrderCovers(t *testing.T) {
	db, users, subs, logs := setupSubDB(t)
	if err := db.AutoMigrate(&domain.PaymentOrder{}); err != nil {
		t.Fatal(err)
	}
	orders := repository.NewPaymentOrderRepository(db)
	svc := NewService(db, users, subs, logs, 7, 3)
	svc.AttachPaymentOrders(orders)
	ctx := context.Background()
	now := time.Now().UTC()
	future := now.Add(20 * 24 * time.Hour)
	paidAt := now.Add(-5 * 24 * time.Hour)

	u := &domain.User{
		Email:              "paid-order@test.com",
		Username:           "paid_order",
		PlanTier:           domain.PlanPremium,
		PlanExpiresAt:      &future,
		SubscriptionStatus: domain.SubStatusActive,
		IsActive:           true,
	}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := orders.Create(ctx, &domain.PaymentOrder{
		UserID:          u.ID,
		InvoiceNumber:   "DD-TEST-COVER-1",
		PlanTier:        domain.PlanPremium,
		BillingInterval: domain.BillingMonthly,
		AmountVND:       79000,
		Status:          domain.PaymentPaid,
		PaidAt:          &paidAt,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.ReconcileBillingState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.UsersExpired != 0 {
		t.Fatalf("paid-order user expired: %+v", res)
	}

	got, _ := users.GetByID(ctx, u.ID)
	if got.PlanTier != domain.PlanPremium || got.SubscriptionStatus != domain.SubStatusActive {
		t.Fatalf("paid-order user not kept: %+v", got)
	}
	if got.PlanExpiresAt == nil {
		t.Fatal("paid-order user lost expiry")
	}
	wantExpiry := domain.ComputePlanExpiry(domain.BillingMonthly, paidAt, nil)
	if !got.PlanExpiresAt.Equal(wantExpiry) {
		t.Fatalf("expiry=%s want stacked order expiry %s", got.PlanExpiresAt.Format(time.RFC3339), wantExpiry.Format(time.RFC3339))
	}
}

func TestReconcileBillingState_RefreshesFreeUserFromCoveringSub(t *testing.T) {
	db, users, subs, logs := setupSubDB(t)
	svc := NewService(db, users, subs, logs, 7, 3)
	ctx := context.Background()
	now := time.Now().UTC()
	future := now.Add(15 * 24 * time.Hour)

	u := &domain.User{
		Email:              "free-with-sub@test.com",
		Username:           "free_sub",
		PlanTier:           domain.PlanFree,
		SubscriptionStatus: domain.SubStatusNone,
		IsActive:           true,
	}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	if err := subs.Create(ctx, &domain.Subscription{
		UserID:       u.ID,
		PlanTier:     domain.PlanPremium,
		Status:       domain.SubStatusActive,
		EventType:    domain.SubEventRenewed,
		Provider:     domain.SubProviderSePay,
		PeriodEndsAt: &future,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := svc.ReconcileBillingState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.UsersRefreshed != 1 {
		t.Fatalf("users_refreshed=%d want 1", res.UsersRefreshed)
	}

	got, _ := users.GetByID(ctx, u.ID)
	if got.PlanTier != domain.PlanPremium || got.SubscriptionStatus != domain.SubStatusActive {
		t.Fatalf("not refreshed from sub: %+v", got)
	}
	if got.PlanExpiresAt == nil || !got.PlanExpiresAt.Equal(future) {
		t.Fatalf("expiry=%v want %s", got.PlanExpiresAt, future.Format(time.RFC3339))
	}

	again, err := svc.ReconcileBillingState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if again.UsersRefreshed != 0 || again.UsersExpired != 0 {
		t.Fatalf("second refresh not idempotent: %+v", again)
	}
}

func TestReconcileBillingState_ExpiresFreeUserWithStaleActiveStatus(t *testing.T) {
	db, users, subs, logs := setupSubDB(t)
	svc := NewService(db, users, subs, logs, 7, 3)
	ctx := context.Background()

	u := &domain.User{
		Email:              "stale-active@test.com",
		Username:           "stale_act",
		PlanTier:           domain.PlanFree,
		SubscriptionStatus: domain.SubStatusActive,
		IsActive:           true,
	}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}

	res, err := svc.ReconcileBillingState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.UsersExpired != 1 {
		t.Fatalf("users_expired=%d want 1", res.UsersExpired)
	}
	got, _ := users.GetByID(ctx, u.ID)
	if got.PlanTier != domain.PlanFree || got.SubscriptionStatus != domain.SubStatusExpired {
		t.Fatalf("stale active status not cleared: %+v", got)
	}
}

func hasStatus(rows []domain.Subscription, want domain.SubscriptionStatus) bool {
	for i := range rows {
		if rows[i].Status == want {
			return true
		}
	}
	return false
}

func statusesOf(rows []domain.Subscription) []domain.SubscriptionStatus {
	out := make([]domain.SubscriptionStatus, len(rows))
	for i := range rows {
		out[i] = rows[i].Status
	}
	return out
}
