// reconcile-subscriptions repairs leftover billing state.
//
// Closes overdue open subscription rows and syncs users.plan_tier /
// subscription_status to verified evidence: a covering subscriptions period,
// a still-covering paid SePay order, or an admin lifetime grant.
// Dated Premium with no covering evidence is set back to free/expired.
// Safe to re-run (idempotent). Default is dry-run.
//
// Usage:
//
//	go run ./cmd/reconcile-subscriptions --env .env.prod-eval.local
//	go run ./cmd/reconcile-subscriptions --env .env.prod-eval.local --apply
//
// Railway (backend service already has DADIARY_DATABASE_URL):
//
//	railway run --service backend go run ./cmd/reconcile-subscriptions
//	railway run --service backend go run ./cmd/reconcile-subscriptions --apply
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	subscriptionuc "github.com/dadiary/backend/internal/usecase/subscription"
)

func main() {
	envPath := flag.String("env", ".env", "env file with database credentials (ignored when vars are already set)")
	apply := flag.Bool("apply", false, "write changes (default is dry-run)")
	flag.Parse()

	cfg, err := config.Load(*envPath)
	if err != nil {
		fail("config: %v", err)
	}
	db, err := repository.NewPostgres(cfg)
	if err != nil {
		fail("database: %v", err)
	}

	users := repository.NewUserRepository(db)
	subs := repository.NewSubscriptionRepository(db)
	logs := repository.NewPlanChangeLogRepository(db)
	orders := repository.NewPaymentOrderRepository(db)
	trialDays, graceDays := domain.DefaultTrialDays, domain.DefaultGraceDays
	if cfg != nil {
		trialDays = cfg.Subscription.TrialDays
		graceDays = cfg.Subscription.GraceDays
	}
	svc := subscriptionuc.NewService(db, users, subs, logs, trialDays, graceDays)
	svc.AttachPaymentOrders(orders)
	ctx := context.Background()
	now := time.Now().UTC()
	grace := domain.ClampGraceDays(graceDays)

	overdue, err := subs.ListOpenOverdue(ctx, now, 2000)
	if err != nil {
		fail("list overdue subscriptions: %v", err)
	}
	activeSubs, err := subs.CountActiveInPeriod(ctx, now)
	if err != nil {
		fail("count active subscriptions: %v", err)
	}
	activeUsers, err := users.CountActivePremiumInPeriod(ctx, now)
	if err != nil {
		fail("count active users: %v", err)
	}
	premiumUsers, err := users.CountActivePremiumUsers(ctx)
	if err != nil {
		fail("count premium plan_tier: %v", err)
	}
	activeStatus, err := users.CountUsersBySubscriptionStatus(ctx, domain.SubStatusActive)
	if err != nil {
		fail("count subscription_status=active: %v", err)
	}
	lifetime, err := users.CountLifetimePaidUsers(ctx)
	if err != nil {
		fail("count lifetime grants: %v", err)
	}
	paidLooking, err := users.ListPaidLookingUsers(ctx, 2000)
	if err != nil {
		fail("list paid-looking users: %v", err)
	}
	coveringIDs, err := subs.ListCoveringUserIDs(ctx, now, grace, 2000)
	if err != nil {
		fail("list covering subscriptions: %v", err)
	}

	fmt.Println("=== BILLING RECONCILE ===")
	fmt.Printf("now=%s  grace_days=%d  apply=%v\n", now.Format(time.RFC3339), grace, *apply)
	fmt.Printf("open overdue subscription rows: %d\n", len(overdue))
	fmt.Printf("users.plan_tier premium/premium_plus: %d\n", premiumUsers)
	fmt.Printf("users.subscription_status=active: %d\n", activeStatus)
	fmt.Printf("lifetime grants (paid + NULL plan_expires_at): %d\n", lifetime)
	fmt.Printf("paid-looking users (paid plan or open status): %d\n", len(paidLooking))
	fmt.Printf("users with covering subscription (period+grace): %d\n", len(coveringIDs))
	fmt.Printf("active billed subscriptions (status=active AND period_ends_at > now): %d\n", activeSubs)
	fmt.Printf("active billed users (premium + status=active AND plan_expires_at > now): %d\n", activeUsers)
	fmt.Println()

	limit := 20
	if len(overdue) < limit {
		limit = len(overdue)
	}
	for i := 0; i < limit; i++ {
		row := overdue[i]
		ends := ""
		if row.PeriodEndsAt != nil {
			ends = row.PeriodEndsAt.UTC().Format(time.RFC3339)
		}
		fmt.Printf("  %s  user=%s  status=%s  period_ends_at=%s  event=%s\n",
			row.ID.String()[:8], row.UserID.String()[:8], row.Status, ends, row.EventType)
	}
	if len(overdue) > limit {
		fmt.Printf("  … %d more\n", len(overdue)-limit)
	}
	fmt.Println()

	if !*apply {
		fmt.Println("Dry-run. Re-run with --apply to write (idempotent; no deletes).")
		return
	}

	res, err := svc.ReconcileBillingState(ctx)
	if err != nil {
		fail("reconcile: %v", err)
	}

	activeSubsAfter, _ := subs.CountActiveInPeriod(ctx, now)
	activeUsersAfter, _ := users.CountActivePremiumInPeriod(ctx, now)
	overdueAfter, _ := subs.ListOpenOverdue(ctx, now, 2000)
	premiumAfter, _ := users.CountActivePremiumUsers(ctx)
	activeStatusAfter, _ := users.CountUsersBySubscriptionStatus(ctx, domain.SubStatusActive)
	lifetimeAfter, _ := users.CountLifetimePaidUsers(ctx)

	fmt.Printf("applied: candidates=%d rows_closed=%d users_expired=%d users_past_due=%d users_refreshed=%d history_appended=%d\n",
		res.Candidates, res.SubscriptionRowsClosed, res.UsersExpired, res.UsersMarkedPastDue, res.UsersRefreshed, res.HistoryEventsAppended)
	fmt.Printf("after: open overdue=%d  active billed subs=%d  active billed users=%d\n",
		len(overdueAfter), activeSubsAfter, activeUsersAfter)
	fmt.Printf("after: plan_tier premium=%d  subscription_status=active=%d  lifetime grants=%d\n",
		premiumAfter, activeStatusAfter, lifetimeAfter)
	if activeSubsAfter != activeUsersAfter {
		fmt.Println("NOTE: billed-period counts still differ — leftover rows or clock skew.")
	} else {
		fmt.Println("OK: active billed subscriptions match active billed users.")
	}
	if premiumAfter > lifetimeAfter && activeSubsAfter == 0 {
		fmt.Println("NOTE: remaining premium rows should be lifetime grants (NULL plan_expires_at) or in-grace covering periods.")
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
