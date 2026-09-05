// refresh-checkin-reminders recomputes D0/D1 first-check-in flags.
//
// Marks users who signed up today or yesterday (Vietnam civil day) and have
// not checked in today. Safe to re-run. Default is dry-run (prints who would
// be due without writing).
//
// Usage:
//
//	go run ./cmd/refresh-checkin-reminders --env .env
//	go run ./cmd/refresh-checkin-reminders --env .env --apply
//
// Railway:
//
//	railway run --service backend go run ./cmd/refresh-checkin-reminders
//	railway run --service backend go run ./cmd/refresh-checkin-reminders --apply
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/streaktime"
	checkinreminderuc "github.com/dadiary/backend/internal/usecase/checkinreminder"
)

func main() {
	envPath := flag.String("env", ".env", "env file with database credentials (ignored when vars are already set)")
	apply := flag.Bool("apply", false, "write flags (default is dry-run)")
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
	checks := repository.NewSkinCheckRepository(db)
	flags := repository.NewCheckInReminderRepository(db)
	svc := checkinreminderuc.NewService(users, checks, flags, cfg.HasVAPIDKeys())
	ctx := context.Background()
	now := streaktime.Now()
	from, to := checkinreminderuc.SignupWindow(now)

	recent, err := users.ListCreatedBetween(ctx, from, to, 200)
	if err != nil {
		fail("list signups: %v", err)
	}

	fmt.Println("=== D0/D1 CHECK-IN REMINDERS ===")
	fmt.Printf("now_vn=%s  window=[%s, %s)  apply=%v\n",
		now.Format(time.RFC3339), from.Format(time.RFC3339), to.Format(time.RFC3339), *apply)
	fmt.Printf("active users created in window: %d\n", len(recent))
	fmt.Println()
	fmt.Println("No outbound email (no ESP). Evening push is the existing daily_reminder job.")
	fmt.Println("App path: GET /api/v1/me/check-in-reminder")
	fmt.Println()

	dueD0, dueD1 := 0, 0
	limit := 20
	shown := 0
	for i := range recent {
		u := recent[i]
		checkedIn, err := checks.HasCheckedInToday(ctx, u.ID)
		if err != nil {
			fail("has checked in: %v", err)
		}
		state := checkinreminderuc.Select(checkinreminderuc.Input{
			SignupAt:       u.CreatedAt,
			Now:            now,
			CheckedInToday: checkedIn,
			AccountActive:  u.IsActive,
		})
		if !state.Due {
			continue
		}
		if state.Kind == checkinreminderuc.KindD0 {
			dueD0++
		} else {
			dueD1++
		}
		if shown < limit {
			fmt.Printf("  %s  user=%s  kind=%s  signup=%s  checked_in_today=%v\n",
				u.ID.String()[:8], u.Email, state.Kind, state.SignupDate, state.CheckedInToday)
			shown++
		}
	}
	if dueD0+dueD1 > shown {
		fmt.Printf("  … %d more due\n", dueD0+dueD1-shown)
	}
	fmt.Printf("\ndue D0=%d  due D1=%d\n\n", dueD0, dueD1)

	if !*apply {
		fmt.Println("Dry-run. Re-run with --apply to upsert checkin_reminder_flags.")
		return
	}

	res, err := svc.RefreshWindow(ctx)
	if err != nil {
		fail("refresh: %v", err)
	}
	fmt.Printf("applied: scanned=%d upserted=%d due_d0=%d due_d1=%d cleared=%d  at=%s\n",
		res.Scanned, res.Upserted, res.DueD0, res.DueD1, res.Cleared, time.Now().UTC().Format(time.RFC3339))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
