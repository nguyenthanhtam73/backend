// replay-ai-outage re-runs skin-check analyses and onboarding starter routines
// that were saved while Anthropic and OpenAI were out of credit.
//
// Selection (default since 2026-11-06T13:00:00Z):
//   - skin checks with a stored photo whose analysis is not completed
//     (failed, missing, or pending/processing older than 5 minutes)
//   - onboarding profiles still on the local starter scaffold
//     (not user-edited, not already starter_routine_source=ai)
//
// Completed analyses and generated starters are skipped, so the command is
// safe to repeat. Default is a dry-run that only counts. A real run calls
// providers oldest first, two at a time, with a pause between starts, and
// stops after 5 consecutive provider errors.
//
// Logs and stdout contain counts only: no emails, photo URLs, or user ids.
//
// Usage:
//
//	go run ./cmd/replay-ai-outage --env .env
//	go run ./cmd/replay-ai-outage --env .env --apply
//
// Railway (backend service already has DADIARY_DATABASE_URL and the AI keys):
//
//	railway run --service backend go run ./cmd/replay-ai-outage
//	railway run --service backend go run ./cmd/replay-ai-outage --apply
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
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/dadiary/backend/internal/service/analysis"
	"github.com/dadiary/backend/internal/storage"
	airecovery "github.com/dadiary/backend/internal/usecase/airecovery"
	profileuc "github.com/dadiary/backend/internal/usecase/profile"
)

func main() {
	envPath := flag.String("env", ".env", "env file (ignored when vars are already set)")
	apply := flag.Bool("apply", false, "call providers (default is dry-run)")
	sinceRaw := flag.String("since", domain.AIAnalysisOutageSince.Format(time.RFC3339), "inclusive RFC3339 lower bound")
	concurrency := flag.Int("concurrency", 2, "provider calls in flight (1–3)")
	pause := flag.Duration("pause", airecovery.DefaultPause, "pause between starting calls when --apply")
	stopAfter := flag.Int("stop-after", 5, "stop after this many consecutive provider errors")
	limit := flag.Int("limit", 0, "max checks and max starter routines (0 = no limit)")
	flag.Parse()

	since, err := time.Parse(time.RFC3339, *sinceRaw)
	if err != nil {
		fail("since: %v", err)
	}

	cfg, err := config.Load(*envPath)
	if err != nil {
		fail("config: %v", err)
	}
	db, err := repository.NewPostgres(cfg)
	if err != nil {
		fail("database: %v", err)
	}

	checks := repository.NewSkinCheckRepository(db)
	profiles := repository.NewSkinProfileRepository(db)
	var processor *analysis.Service
	var starters *profileuc.Service
	if *apply {
		store, err := storage.New(cfg)
		if err != nil {
			fail("storage: %v", err)
		}
		feedback := repository.NewAIFeedbackRepository(db)
		routines := repository.NewRoutineEntryRepository(db)
		wardrobe := repository.NewSkincareProductRepository(db)
		cache := ai.NewMemoryCache()
		processor = analysis.New(cfg, checks, profiles, feedback, routines, wardrobe, cache, store)
		starters = profileuc.NewService(cfg, profiles, checks, feedback, routines, wardrobe, cache)
	}

	svc := airecovery.NewService(checks, profiles, processor, starters)
	res, err := svc.Run(context.Background(), airecovery.Options{
		Since:       since,
		Apply:       *apply,
		Concurrency: *concurrency,
		Pause:       *pause,
		StopAfter:   *stopAfter,
		Limit:       *limit,
	})
	if err != nil {
		fail("replay: %v", err)
	}

	fmt.Println("=== AI OUTAGE REPLAY ===")
	fmt.Printf("since=%s  apply=%v  concurrency=%d  pause=%s  stop_after=%d  limit=%d\n",
		res.Since, *apply, *concurrency, pause.String(), *stopAfter, *limit)
	fmt.Printf("found=%d  done=%d  still_failed=%d  skipped=%d  stopped=%v\n",
		res.Found, res.Done, res.StillFailed, res.Skipped, res.Stopped)
	fmt.Printf("checks: found=%d done=%d still_failed=%d skipped=%d\n",
		res.Checks.Found, res.Checks.Done, res.Checks.StillFailed, res.Checks.Skipped)
	fmt.Printf("starters: found=%d done=%d still_failed=%d skipped=%d\n",
		res.Starters.Found, res.Starters.Done, res.Starters.StillFailed, res.Starters.Skipped)
	if !*apply {
		fmt.Println()
		fmt.Println("Dry-run. Re-run with --apply to call providers.")
		fmt.Println("Safe to repeat: completed analyses and existing AI starters are skipped.")
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
