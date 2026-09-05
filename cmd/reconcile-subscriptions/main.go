// reconcile-subscriptions repairs leftover billing state.
//
// The daily PlanExpiryJob updates users when grace ends but used to leave the
// original subscriptions.status='active' row in place. This command closes
// overdue open subscription rows and syncs users.plan_tier / subscription_status.
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
	trialDays, graceDays := domain.DefaultTrialDays, domain.DefaultGraceDays
	if cfg != nil {
		trialDays = cfg.Subscription.TrialDays
		graceDays = cfg.Subscription.GraceDays
	}
	svc := subscriptionuc.NewService(db, users, subs, logs, trialDays, graceDays)
	ctx := context.Background()
	now := time.Now().UTC()

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

	fmt.Println("=== BILLING RECONCILE ===")
	fmt.Printf("now=%s  grace_days=%d  apply=%v\n", now.Format(time.RFC3339), domain.ClampGraceDays(graceDays), *apply)
	fmt.Printf("open overdue subscription rows: %d\n", len(overdue))
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

	fmt.Printf("applied: candidates=%d rows_closed=%d users_expired=%d users_past_due=%d history_appended=%d\n",
		res.Candidates, res.SubscriptionRowsClosed, res.UsersExpired, res.UsersMarkedPastDue, res.HistoryEventsAppended)
	fmt.Printf("after: open overdue=%d  active billed subs=%d  active billed users=%d\n",
		len(overdueAfter), activeSubsAfter, activeUsersAfter)
	if activeSubsAfter != activeUsersAfter {
		fmt.Println("NOTE: counts still differ — check lifetime grants (NULL plan_expires_at) or leftover rows.")
	} else {
		fmt.Println("OK: active billed subscriptions match active billed users.")
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
