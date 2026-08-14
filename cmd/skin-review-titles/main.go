// skin-review-titles writes a clean page title onto published skin reviews that have none.
//
// Why: with no stored title the share page derives one from the user's raw question, which
// ships chat shorthand ("kb có hqua thật k") into <title> and Facebook previews. This asks
// the text model to spell the same question properly and stores the result, which also
// bumps updated_at so the sitemap tells Google the page changed.
//
// Dry run by default — nothing is written without --apply.
//
//	go run ./cmd/skin-review-titles --env .env.prod-eval.local --limit 5
//	go run ./cmd/skin-review-titles --env .env.prod-eval.local --apply
//
// Operator-written titles are never overwritten.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"gorm.io/gorm"
)

func main() {
	envPath := flag.String("env", ".env.prod-eval.local", "env file with prod DB + AI credentials")
	limit := flag.Int("limit", 200, "maximum reviews to process")
	apply := flag.Bool("apply", false, "actually write the titles (default: dry run)")
	scan := flag.Bool("scan", false, "list public titles that still look like chat shorthand")
	flag.Parse()

	cfg, err := config.Load(*envPath)
	if err != nil {
		fail("config: %v", err)
	}
	db, err := repository.NewPostgres(cfg)
	if err != nil {
		fail("database: %v", err)
	}
	if *scan {
		scanDirtyTitles(db, *limit)
		return
	}
	if !cfg.HasAnthropicKey() && !cfg.HasOpenAIKey() {
		fail("no text model configured: set DADIARY_ANTHROPIC_API_KEY and/or DADIARY_OPENAI_API_KEY in %s", *envPath)
	}
	repo := repository.NewAdminSkinReviewRepository(db)
	httpClient := &http.Client{Timeout: 2 * time.Minute}

	var rows []domain.AdminSkinReview
	if err := db.
		Where("is_public = ? AND (title IS NULL OR title = '') AND user_question <> ''", true).
		Order("published_at DESC").
		Limit(*limit).
		Find(&rows).Error; err != nil {
		fail("query: %v", err)
	}
	if len(rows) == 0 {
		fmt.Println("Nothing to do: every public review already has a title (or has no question).")
		return
	}

	mode := "DRY RUN (nothing written)"
	if *apply {
		mode = "APPLYING"
	}
	fmt.Printf("%d review(s) without a title · %s\n\n", len(rows), mode)

	written, fellBack, failed := 0, 0, 0
	for i, row := range rows {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		title, fromModel, err := ai.CleanShareTitle(ctx, cfg, httpClient, row.UserQuestion, row.Locale)
		cancel()

		switch {
		case title == "":
			failed++
			fmt.Printf("%3d. SKIP  %s  (no usable title: %v)\n", i+1, row.ID.String()[:8], err)
			continue
		case !fromModel:
			fellBack++
			fmt.Printf("%3d. FALLBACK %s  %v\n", i+1, row.ID.String()[:8], err)
		}

		fmt.Printf("%3d. %s\n     hỏi : %s\n     title: %s\n", i+1, row.ID.String()[:8],
			clip(row.UserQuestion, 90), title)

		if !*apply {
			continue
		}
		if _, err := repo.UpdateMeta(context.Background(), row.ID, &title, nil, nil, nil, nil); err != nil {
			failed++
			fmt.Printf("     !! write failed: %v\n", err)
			continue
		}
		written++
	}

	fmt.Printf("\nprocessed=%d written=%d fallback=%d failed=%d\n", len(rows), written, fellBack, failed)
	if !*apply {
		fmt.Println("Dry run only. Re-run with --apply to write these titles.")
	}
}

func scanDirtyTitles(db *gorm.DB, limit int) {
	var rows []domain.AdminSkinReview
	if err := db.Where("is_public = ?", true).Order("updated_at DESC").Limit(limit).Find(&rows).Error; err != nil {
		fail("scan: %v", err)
	}
	empty, dirty := 0, 0
	for _, row := range rows {
		title := strings.TrimSpace(row.Title)
		if title == "" {
			empty++
			fmt.Printf("EMPTY  %s  %s\n", row.PublicSlug, clip(row.UserQuestion, 70))
			continue
		}
		if titleLooksDirty(title) {
			dirty++
			fmt.Printf("DIRTY  %s  %s\n", row.PublicSlug, title)
		}
	}
	fmt.Printf("\npublic=%d empty=%d dirty=%d\n", len(rows), empty, dirty)
}

func titleLooksDirty(title string) bool {
	for _, w := range strings.Fields(strings.ToLower(title)) {
		core := strings.TrimRight(w, "?!.,:;…")
		switch core {
		case "kb", "hqua", "đtri", "dtri", "ntn", "ko", "kg":
			return true
		}
	}
	return false
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
