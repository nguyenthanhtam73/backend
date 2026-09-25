// refresh-cabinet-insights rewrites cabinet cards whose free text still talks
// about buying. Cabinet rows are products the user already owns. The machine
// token buy.advice stays "nên mua" / "chưa nên" (the app maps those to
// "Nên dùng tiếp" / "Chưa nên dùng tiếp"). This command only selects rows
// whose reason, what-it-does, or ingredient name/gloss contains the word "mua".
//
// Dry run is the default: it lists affected rows, does not call the model,
// and does not write. --apply regenerates each card with one text-model call
// and updates only insight and insight_at on that row (not updated_at, name, or notes).
//
// Safe to re-run. A row drops out once its free text no longer contains "mua".
// The advice token "nên mua" does not count. Other columns and other tables
// are not updated. A failed model call leaves that row unchanged.
//
//	go run ./cmd/refresh-cabinet-insights --env .env.prod-eval.local
//	go run ./cmd/refresh-cabinet-insights --env .env.prod-eval.local --limit 20
//	go run ./cmd/refresh-cabinet-insights --env .env.prod-eval.local --apply --limit 20 --sleep 2s
//
// Run one copy at a time. Each --apply row is one OpenAI text call. The
// default pause between calls is 2s. HTTP 429 and 5xx still go through the
// shared retry helper. Do not point this at production unless you mean to
// spend model calls on the rows the dry run listed.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func main() {
	envPath := flag.String("env", ".env.prod-eval.local", "env file with DB credentials; --apply also needs the OpenAI key")
	limit := flag.Int("limit", 20, "maximum matching cards to list or regenerate this run")
	apply := flag.Bool("apply", false, "regenerate and write insight + insight_at (default: dry run, no model calls)")
	sleep := flag.Duration("sleep", 2*time.Second, "pause between model calls when --apply is set")
	userRaw := flag.String("user", "", "optional user UUID; limit the scan to one cabinet")
	flag.Parse()

	if *limit <= 0 {
		fail("--limit must be > 0")
	}
	if *sleep < 0 {
		fail("--sleep must be >= 0")
	}
	var onlyUser uuid.UUID
	if strings.TrimSpace(*userRaw) != "" {
		id, err := uuid.Parse(strings.TrimSpace(*userRaw))
		if err != nil {
			fail("--user must be a UUID: %v", err)
		}
		onlyUser = id
	}

	cfg, err := config.Load(*envPath)
	if err != nil {
		fail("config: %v", err)
	}
	db, err := repository.NewPostgres(cfg)
	if err != nil {
		fail("database: %v", err)
	}

	rows, err := loadInsightRows(db, onlyUser)
	if err != nil {
		fail("query: %v", err)
	}

	type match struct {
		row  domain.SkincareProduct
		hits []dto.WardrobeInsightMuaHit
	}
	var matches []match
	adviceOnly := 0
	for i := range rows {
		hits := dto.WardrobeInsightMuaHits(rows[i].Insight)
		if len(hits) == 0 {
			adviceOnly++
			continue
		}
		matches = append(matches, match{row: rows[i], hits: hits})
	}

	mode := "DRY RUN (nothing written, no model calls)"
	if *apply {
		mode = "APPLY"
	}
	fmt.Printf("%s\n", mode)
	fmt.Printf("scanned=%d advice-token-only=%d free-text-mua=%d showing=%d\n\n",
		len(rows), adviceOnly, len(matches), min(len(matches), *limit))
	fmt.Println(`buy.advice "nên mua" is ignored. Only what_it_does, fit.reason, buy.why, and actives name/gloss count.`)
	fmt.Println()

	if len(matches) == 0 {
		fmt.Println("Nothing to do: no cabinet card has free text containing \"mua\".")
		return
	}

	shown := matches
	if len(shown) > *limit {
		shown = shown[:*limit]
	}
	for i, m := range shown {
		fmt.Printf("%3d. product=%s user=%s name=%s\n", i+1, m.row.ID, m.row.UserID, clip(m.row.Name, 80))
		for _, h := range m.hits {
			fmt.Printf("     %s: %s\n", h.Field, clip(h.Text, 180))
		}
	}
	if len(matches) > len(shown) {
		fmt.Printf("\n%d more match(es) not shown. Raise --limit or re-run after this batch.\n", len(matches)-len(shown))
	}
	if !*apply {
		fmt.Println("\nDry run only. Re-run with --apply to regenerate these cards.")
		fmt.Println("Each card is one text-model call. --sleep (default 2s) pauses between calls.")
		fmt.Println("Writes change insight and insight_at only.")
		return
	}
	if !cfg.HasOpenAIKey() {
		fail("no OpenAI key: set DADIARY_OPENAI_API_KEY in %s", *envPath)
	}

	products := repository.NewSkincareProductRepository(db)
	profiles := repository.NewSkinProfileRepository(db)
	checks := repository.NewSkinCheckRepository(db)
	httpClient := &http.Client{Timeout: 2 * time.Minute}
	skinCache := map[uuid.UUID]skinBundle{}

	written, failed := 0, 0
	for i, m := range shown {
		if i > 0 && *sleep > 0 {
			time.Sleep(*sleep)
		}
		bundle := loadSkin(skinCache, profiles, checks, m.row.UserID)
		if bundle.err != nil {
			failed++
			fmt.Printf("     !! skin context %s: %v\n", m.row.ID, bundle.err)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		card, err := ai.GenerateWardrobeProductInsight(ctx, cfg, httpClient, ai.WardrobeProductInsightRequest{
			Name:     m.row.Name,
			Brand:    m.row.Brand,
			Category: m.row.Category,
			Notes:    m.row.Notes,
			Profile:  bundle.profile,
			Recent:   bundle.recent,
		})
		cancel()
		if err != nil {
			failed++
			fmt.Printf("     !! model %s: %v (row left unchanged)\n", m.row.ID, err)
			continue
		}
		raw, err := json.Marshal(card)
		if err != nil {
			failed++
			fmt.Printf("     !! encode %s: %v\n", m.row.ID, err)
			continue
		}
		if hits := dto.WardrobeInsightMuaHits(raw); len(hits) > 0 {
			failed++
			fmt.Printf("     !! %s still contains mua after the guard (%s); row left unchanged\n", m.row.ID, hits[0].Field)
			continue
		}
		ok, err := products.ReplaceInsightCopy(context.Background(), m.row.UserID, m.row.ID, raw, time.Now().UTC())
		if err != nil || !ok {
			failed++
			fmt.Printf("     !! write %s: ok=%v err=%v (row left unchanged)\n", m.row.ID, ok, err)
			continue
		}
		written++
		fmt.Printf("     wrote %s\n", m.row.ID)
		fmt.Printf("     why: %s\n", clip(card.Buy.Why, 180))
	}
	fmt.Printf("\nmatched=%d attempted=%d written=%d failed=%d\n", len(matches), len(shown), written, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

type skinBundle struct {
	profile *domain.SkinProfile
	recent  []domain.SkinCheck
	err     error
}

func loadSkin(
	cache map[uuid.UUID]skinBundle,
	profiles *repository.GormSkinProfileRepository,
	checks *repository.GormSkinCheckRepository,
	userID uuid.UUID,
) skinBundle {
	if b, ok := cache[userID]; ok {
		return b
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var b skinBundle
	b.profile, b.err = profiles.GetByUserID(ctx, userID)
	if b.err == nil {
		b.recent, b.err = checks.ListRecentForCoach(ctx, userID, uuid.Nil, 5)
	}
	cache[userID] = b
	return b
}

func loadInsightRows(db *gorm.DB, onlyUser uuid.UUID) ([]domain.SkincareProduct, error) {
	q := db.Model(&domain.SkincareProduct{}).
		Select("id", "user_id", "name", "brand", "category", "notes", "insight").
		Where("insight IS NOT NULL AND CAST(insight AS TEXT) ILIKE ?", "%mua%").
		Order("insight_at ASC NULLS FIRST")
	if onlyUser != uuid.Nil {
		q = q.Where("user_id = ?", onlyUser)
	}
	var rows []domain.SkincareProduct
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
