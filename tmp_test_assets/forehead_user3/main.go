// Temporary: run AdminSkinReviewAnalyze on 3 user forehead crops.
// Usage (from backend/): go run ./tmp_test_assets/forehead_user3/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/service/ai"
)

func main() {
	assets := filepath.Join("tmp_test_assets", "forehead_user3")
	imgs := []string{
		filepath.Join(assets, "forehead_user_1.jpg"),
		filepath.Join(assets, "forehead_user_2.jpg"),
		filepath.Join(assets, "forehead_user_3.jpg"),
	}
	for _, p := range imgs {
		if _, err := os.Stat(p); err != nil {
			fmt.Fprintf(os.Stderr, "missing %s — copy user forehead photos first\n", p)
			os.Exit(1)
		}
	}

	cfg, err := config.Load("")
	if err != nil || !cfg.HasOpenAIKey() {
		fmt.Fprintln(os.Stderr, "config / openai key required")
		os.Exit(1)
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	fail := false

	for i, path := range imgs {
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n######## IMAGE %d: %s ########\n", i+1, filepath.Base(path))
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		analysis, model, err := ai.AdminSkinReviewAnalyze(ctx, cfg, client, []ai.ImageBytes{{Data: raw}}, "vi")
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "analyze: %v\n", err)
			os.Exit(1)
		}
		b, _ := json.MarshalIndent(map[string]any{
			"image": path, "model": model, "analysis": analysis,
		}, "", "  ")
		fmt.Println(string(b))

		fh := find(analysis, "forehead")
		nose := find(analysis, "nose")
		cheeks := find(analysis, "cheeks")
		chin := find(analysis, "chin")

		fmt.Println("\n--- LENGTH COMPARE ---")
		printArea("forehead", fh)
		printArea("nose", nose)
		printArea("cheeks", cheeks)
		printArea("chin", chin)
		fmt.Printf("overview: %d câu / %d chars\n", countSent(analysis.Overview), utf8.RuneCountInString(analysis.Overview))

		ok := true
		if fh == nil || strings.EqualFold(fh.Concern, "not_visible") {
			fmt.Println("FAIL: forehead not primary visible")
			ok = false
		} else if n := countSent(fh.Note); n < 4 {
			fmt.Printf("FAIL: forehead note too short (%d câu, want 4–6)\n", n)
			ok = false
		} else {
			fmt.Printf("PASS: forehead note %d câu (thick)\n", countSent(fh.Note))
		}
		for _, a := range []*dto.AdminSkinAttentionArea{nose, cheeks, chin} {
			name := "?"
			if a != nil {
				name = a.Region
			}
			if a == nil || !strings.EqualFold(a.Concern, "not_visible") {
				fmt.Printf("FAIL: %s should be not_visible\n", name)
				ok = false
				continue
			}
			n := countSent(a.Note)
			if n != 1 {
				fmt.Printf("FAIL: %s not_visible note = %d câu (want exactly 1)\n", name, n)
				ok = false
			} else {
				fmt.Printf("PASS: %s not_visible = 1 câu\n", name)
			}
		}
		ov := countSent(analysis.Overview)
		if ov < 3 || ov > 5 {
			fmt.Printf("WARN: overview %d câu (target 3–5 for single-region)\n", ov)
		} else {
			fmt.Printf("PASS: overview %d câu\n", ov)
		}
		if !ok {
			fail = true
		}
	}
	if fail {
		os.Exit(1)
	}
	fmt.Println("\nOVERALL: PASS")
}

func find(a *dto.AdminSkinReviewAnalysis, region string) *dto.AdminSkinAttentionArea {
	for i := range a.AttentionAreas {
		if strings.EqualFold(a.AttentionAreas[i].Region, region) {
			return &a.AttentionAreas[i]
		}
	}
	return nil
}

func printArea(label string, a *dto.AdminSkinAttentionArea) {
	if a == nil {
		fmt.Printf("%s: MISSING\n", label)
		return
	}
	fmt.Printf("%s [%s] %d câu / %d chars:\n  %s\n",
		label, a.Concern, countSent(a.Note), utf8.RuneCountInString(a.Note), a.Note)
}

func countSent(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	re := regexp.MustCompile(`[.!?…]+|\n+`)
	parts := re.Split(s, -1)
	n := 0
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return n
}
