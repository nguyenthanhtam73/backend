// expire-pending-orders marks leftover SePay checkout rows that stayed pending.
//
// This is local hygiene only (payment_orders.status). It does not call SePay.
// A later ORDER_PAID IPN still fulfills. Safe to re-run (idempotent). Default is dry-run.
//
// Usage:
//
//	go run ./cmd/expire-pending-orders --env .env
//	go run ./cmd/expire-pending-orders --env .env --apply
//
// Railway (backend service already has DADIARY_DATABASE_URL):
//
//	railway run --service backend go run ./cmd/expire-pending-orders
//	railway run --service backend go run ./cmd/expire-pending-orders --apply
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/repository"
	paymentuc "github.com/dadiary/backend/internal/usecase/payment"
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
	orders := repository.NewPaymentOrderRepository(db)
	logs := repository.NewPlanChangeLogRepository(db)
	svc := paymentuc.NewService(db, cfg, orders, users, logs)
	ctx := context.Background()
	now := time.Now().UTC()

	preview, err := svc.PreviewStalePending(ctx, now)
	if err != nil {
		fail("preview: %v", err)
	}

	fmt.Println("=== EXPIRE PENDING SEPAY ORDERS ===")
	fmt.Printf("now=%s  ttl_hours=%d  cutoff=%s  apply=%v\n",
		now.Format(time.RFC3339), preview.TTLHours, preview.Cutoff.Format(time.RFC3339), *apply)
	fmt.Printf("pending older than TTL: %d\n", preview.PendingStale)
	fmt.Printf("pending still inside TTL: %d\n", preview.PendingFresh)
	fmt.Println()
	fmt.Println("TTL is a local hygiene window (not a SePay-documented session lifetime).")
	fmt.Println("Only payment_orders.status is updated. Late ORDER_PAID IPN still fulfills.")
	fmt.Println()

	stale, err := orders.ListPendingCreatedBefore(ctx, preview.Cutoff, 20)
	if err != nil {
		fail("list stale: %v", err)
	}
	for _, row := range stale {
		fmt.Printf("  %s  user=%s  invoice=%s  created_at=%s\n",
			row.ID.String()[:8], row.UserID.String()[:8], row.InvoiceNumber, row.CreatedAt.UTC().Format(time.RFC3339))
	}
	if preview.PendingStale > int64(len(stale)) {
		fmt.Printf("  … %d more\n", preview.PendingStale-int64(len(stale)))
	}
	fmt.Println()

	if !*apply {
		fmt.Println("Dry-run. Re-run with --apply to write (idempotent; no deletes).")
		return
	}

	res, err := svc.ExpireStalePending(ctx, now)
	if err != nil {
		fail("expire: %v", err)
	}
	fmt.Printf("applied: expired=%d  pending_fresh=%d  pending_stale_before=%d\n",
		res.Expired, res.PendingFresh, res.PendingStale)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
