// send-outage-apology emails users whose skin check in the 6–7 Nov 2026
// credit outage (2026-11-06T13:30Z through 2026-11-07T18:15Z) failed or
// never completed. A later replay keeps them via outage_affected.
//
// Default is a dry-run that prints only the recipient count. --test sends ONE
// email, the "result is ready" variant, to the first address in
// DADIARY_ADMIN_EMAILS. --apply sends for real, at most once per user
// (marker ai_outage_apology_20261106), 200ms apart, and stops after 5 send errors.
//
// Stdout and logs never include an email address.
//
//	go run ./cmd/send-outage-apology --env .env
//	go run ./cmd/send-outage-apology --env .env --test
//	go run ./cmd/send-outage-apology --env .env --apply
//
//	railway run --service backend go run ./cmd/send-outage-apology
//	railway run --service backend go run ./cmd/send-outage-apology --test
//	railway run --service backend go run ./cmd/send-outage-apology --apply
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/email"
	"github.com/dadiary/backend/internal/usecase/outageapology"
)

func main() {
	envPath := flag.String("env", ".env", "env file (ignored when vars are already set)")
	apply := flag.Bool("apply", false, "send to affected users (default is dry-run)")
	testMode := flag.Bool("test", false, "send one has-result email to the first DADIARY_ADMIN_EMAILS address")
	pause := flag.Duration("pause", outageapology.DefaultPause, "pause between sends when --apply")
	stopAfter := flag.Int("stop-after", outageapology.DefaultStopAfter, "stop after this many send errors")
	flag.Parse()

	if *apply && *testMode {
		fail("pass only one of --apply or --test")
	}

	cfg, err := config.Load(*envPath)
	if err != nil {
		fail("config: %v", err)
	}
	db, err := repository.NewPostgres(cfg)
	if err != nil {
		fail("database: %v", err)
	}
	if err := repository.PrepareOutageApologySchema(db); err != nil {
		fail("schema: %v", err)
	}

	mode := outageapology.ModeDryRun
	switch {
	case *testMode:
		mode = outageapology.ModeTest
	case *apply:
		mode = outageapology.ModeApply
	}
	origin := "https://dadiary.vn"
	var admins []string
	if cfg != nil {
		origin = cfg.PublicWebOrigin()
		admins = cfg.AdminEmails
	}
	svc := outageapology.NewService(
		repository.NewSkinCheckRepository(db),
		repository.NewEmailSendReceiptRepository(db),
		email.NewResendClient(cfg.Email.ResendAPIKey, cfg.Email.From),
		origin,
		admins,
	)
	res, err := svc.Run(context.Background(), mode, *pause, *stopAfter)
	if err != nil {
		fail("send: %v", err)
	}
	if mode == outageapology.ModeDryRun {
		fmt.Printf("recipients=%d\n", res.Recipients)
		return
	}
	fmt.Printf("recipients=%d sent=%d skipped=%d failed=%d stopped=%v test=%v\n",
		res.Recipients, res.Sent, res.Skipped, res.Failed, res.Stopped, res.Test)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
