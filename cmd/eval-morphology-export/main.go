// eval-morphology-export turns operator corrections into the labeled eval set.
//
// Reviewing already means judging whether the AI named the right group. This command
// collects those judgements — every admin_skin_reviews row with analysis_corrected_at —
// and writes the photos plus a manifest.json that TestMorphologyEvalLive scores against.
// That closes the loop: you review as usual, and accuracy numbers appear without anyone
// hand-labelling anything.
//
// Usage:
//
//	go run ./cmd/eval-morphology-export
//	go run ./cmd/eval-morphology-export --out internal/service/ai/testdata/morphology --limit 200
//	DADIARY_OPENAI_API_KEY=... go test ./internal/service/ai/ -run TestMorphologyEvalLive -v
//
// PRIVACY: the exported photos are real user faces. The output directory gets a
// .gitignore that excludes everything, so a generated eval set can never be committed.
// Delete the directory when you are done measuring.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/dadiary/backend/internal/storage"
)

// evalCase mirrors the manifest entry TestMorphologyEvalLive reads.
type evalCase struct {
	File        string `json:"file"`
	Region      string `json:"region"`
	WantGroup   string `json:"want_group,omitempty"`
	WantConcern string `json:"want_concern,omitempty"`
	SkinContext string `json:"skin_context,omitempty"`
	Question    string `json:"question,omitempty"`
	// AIGroup / AIConcern record what the model said BEFORE the correction, so the
	// manifest doubles as a record of which groups it tends to get wrong.
	AIGroup   string `json:"ai_group,omitempty"`
	AIConcern string `json:"ai_concern,omitempty"`
	ReviewID  string `json:"review_id"`
	Corrected string `json:"corrected_at,omitempty"`
}

func main() {
	outDir := flag.String("out", filepath.Join("internal", "service", "ai", "testdata", "morphology"), "output directory for photos + manifest.json")
	limit := flag.Int("limit", 500, "maximum corrected reviews to export")
	envPath := flag.String("env", ".env", "env file to load")
	flag.Parse()

	cfg, err := config.Load(*envPath)
	if err != nil {
		fail("config: %v", err)
	}
	db, err := repository.NewPostgres(cfg)
	if err != nil {
		fail("database: %v", err)
	}
	store, err := storage.New(cfg)
	if err != nil {
		fail("storage: %v", err)
	}

	var rows []domain.AdminSkinReview
	if err := db.
		Where("analysis_corrected_at IS NOT NULL").
		Order("analysis_corrected_at DESC").
		Limit(*limit).
		Find(&rows).Error; err != nil {
		fail("query corrected reviews: %v", err)
	}
	if len(rows) == 0 {
		fmt.Println("No corrected reviews yet.")
		fmt.Println("Correct a wrong group in /admin/skin-review first — that is what creates a label.")
		return
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fail("create %s: %v", *outDir, err)
	}
	// Real faces must never reach git, no matter how this directory is used later.
	gitignore := filepath.Join(*outDir, ".gitignore")
	if err := os.WriteFile(gitignore, []byte("# Exported eval set: real user photos. Never commit.\n*\n!.gitignore\n"), 0o644); err != nil {
		fail("write %s: %v", gitignore, err)
	}

	ctx := context.Background()
	cases := make([]evalCase, 0, len(rows))
	var skipped, aiWrong int

	for _, row := range rows {
		corrected := dto.AdminSkinReviewAnalysis{}
		if len(row.Analysis) > 0 {
			_ = json.Unmarshal(row.Analysis, &corrected)
		}
		area, ok := primaryProblemArea(corrected)
		if !ok {
			skipped++
			continue
		}

		wantGroup := ""
		if g, exact := ai.GroupFromCorrectedArea(area.Concern, area.Region, area.Note); exact {
			wantGroup = string(g)
		}

		rels, derr := dto.DecodeStringSlice(row.ImagePaths)
		if derr != nil || len(rels) == 0 {
			skipped++
			continue
		}
		// The first photo is the one the review is really about.
		data, rerr := store.Read(ctx, rels[0])
		if rerr != nil || len(data) == 0 {
			fmt.Fprintf(os.Stderr, "skip %s: read photo: %v\n", row.ID, rerr)
			skipped++
			continue
		}
		name := fmt.Sprintf("%s%s", row.ID.String(), extOrJPG(rels[0]))
		if werr := os.WriteFile(filepath.Join(*outDir, name), data, 0o644); werr != nil {
			fail("write photo %s: %v", name, werr)
		}

		original := dto.AdminSkinReviewAnalysis{}
		aiGroup, aiConcern := "", ""
		if len(row.AnalysisOriginal) > 0 {
			if json.Unmarshal(row.AnalysisOriginal, &original) == nil {
				aiGroup = original.MorphologyGroup
				if oa, ook := primaryProblemArea(original); ook {
					aiConcern = oa.Concern
				}
			}
		}
		if aiConcern != "" && !strings.EqualFold(aiConcern, area.Concern) {
			aiWrong++
		}

		c := evalCase{
			File:        name,
			Region:      area.Region,
			WantGroup:   wantGroup,
			SkinContext: row.SkinContext,
			Question:    row.UserQuestion,
			AIGroup:     aiGroup,
			AIConcern:   aiConcern,
			ReviewID:    row.ID.String(),
		}
		if wantGroup == "" {
			// No single group maps to this enum — score the concern instead of guessing.
			c.WantConcern = strings.ToLower(strings.TrimSpace(area.Concern))
		}
		if row.AnalysisCorrectedAt != nil {
			c.Corrected = row.AnalysisCorrectedAt.UTC().Format(time.RFC3339)
		}
		cases = append(cases, c)
	}

	if len(cases) == 0 {
		fmt.Printf("Found %d corrected reviews but none were exportable (missing photos or no visible problem area).\n", len(rows))
		return
	}

	manifest, err := json.MarshalIndent(cases, "", "  ")
	if err != nil {
		fail("marshal manifest: %v", err)
	}
	manifestPath := filepath.Join(*outDir, "manifest.json")
	if err := os.WriteFile(manifestPath, append(manifest, '\n'), 0o644); err != nil {
		fail("write manifest: %v", err)
	}

	byGroup := map[string]int{}
	for _, c := range cases {
		key := c.WantGroup
		if key == "" {
			key = "concern:" + c.WantConcern
		}
		byGroup[key]++
	}

	fmt.Printf("Exported %d labeled cases → %s\n", len(cases), manifestPath)
	if skipped > 0 {
		fmt.Printf("Skipped %d (no visible problem area or unreadable photo)\n", skipped)
	}
	if aiWrong > 0 {
		fmt.Printf("The AI's first answer differed from your correction in %d of them — those are the informative ones.\n", aiWrong)
	}
	fmt.Println("\nLabels per group:")
	for k, n := range byGroup {
		fmt.Printf("  %-28s %d\n", k, n)
	}
	fmt.Println("\nNow measure:")
	fmt.Println("  DADIARY_OPENAI_API_KEY=... go test ./internal/service/ai/ -run TestMorphologyEvalLive -v")
	fmt.Println("\nThese are real user photos — delete this directory when you are done.")
}

// primaryProblemArea picks the area the review is about: cheeks win (that is where the
// look-alike cases live), otherwise the first area with a real concern.
func primaryProblemArea(a dto.AdminSkinReviewAnalysis) (dto.AdminSkinAttentionArea, bool) {
	var first dto.AdminSkinAttentionArea
	found := false
	for _, ar := range a.AttentionAreas {
		switch strings.ToLower(strings.TrimSpace(ar.Concern)) {
		case "", "none", "not_visible":
			continue
		}
		if !found {
			first, found = ar, true
		}
		if strings.EqualFold(strings.TrimSpace(ar.Region), "cheeks") {
			return ar, true
		}
	}
	return first, found
}

func extOrJPG(rel string) string {
	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
		return ext
	default:
		return ".jpg"
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
