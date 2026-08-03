package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/service/ai"
)

func main() {
	mode := "photo"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	cfg, err := config.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	if !cfg.HasOpenAIKey() {
		fmt.Fprintln(os.Stderr, "missing OpenAI key")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 5 * time.Minute}

	switch mode {
	case "photo":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: onboarding_smoke photo image.jpg")
			os.Exit(2)
		}
		raw, err := os.ReadFile(os.Args[2])
		if err != nil {
			fmt.Fprintf(os.Stderr, "read: %v\n", err)
			os.Exit(1)
		}
		out, err := ai.OnboardingSkinAnalyze(ctx, cfg, client, []ai.ImageBytes{{Data: raw}, {Data: raw}}, "vi")
		if err != nil {
			fmt.Fprintf(os.Stderr, "analyze: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("=== ANALYZE (photo) ===")
		fmt.Println("model:", out.ModelUsed)
		fmt.Println("skin_type_guess:", out.SkinTypeGuess)
		fmt.Println("concerns:", strings.Join(out.Concerns, ", "))
		fmt.Println("main_concerns:", strings.Join(out.MainConcerns, ", "))
		fmt.Println("suggested_goal:", out.SuggestedGoal)
		fmt.Println("barrier_signal:", out.BarrierSignal)
		fmt.Println("\n--- detailed_observations ---")
		fmt.Println(out.DetailedObservations)
		fmt.Println("\n--- coaching_notes ---")
		fmt.Println(out.CoachingNotes)
		fmt.Println("\n--- visual_observations (first 6) ---")
		for i, v := range out.VisualObservations {
			if i >= 6 { break }
			fmt.Println("-", v)
		}
		hits := jargonHits(out.DetailedObservations, out.CoachingNotes, strings.Join(out.MainConcerns, " "), strings.Join(out.VisualObservations, " "))
		printJargon(hits)
		payload := map[string]any{
			"skin_type": out.SkinTypeGuess,
			"concerns": out.Concerns,
			"goal": out.SuggestedGoal,
			"skill_level": "beginner",
			"locale": "vi",
			"skin_analysis": out,
		}
		b, _ := json.Marshal(payload)
		routine, err := ai.GenerateStarterRoutine(ctx, cfg, b, "vi", "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "starter: %v\n", err)
			os.Exit(1)
		}
		printRoutine("photo-grounded", routine)
	case "goal":
		payload := map[string]any{
			"skin_type": "combo",
			"concerns": []string{"acne", "weak_barrier"},
			"goal": "barrier",
			"skill_level": "beginner",
			"locale": "vi",
		}
		b, _ := json.Marshal(payload)
		routine, err := ai.GenerateStarterRoutine(ctx, cfg, b, "vi", "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "starter: %v\n", err)
			os.Exit(1)
		}
		printRoutine("goal-only", routine)
	default:
		fmt.Fprintln(os.Stderr, "usage: onboarding_smoke photo image.jpg | goal")
		os.Exit(2)
	}
}

func printRoutine(label string, r ai.StarterRoutine) {
	fmt.Printf("\n=== STARTER ROUTINE (%s) ===\n", label)
	fmt.Println("encouragement:", r.Encouragement)
	fmt.Println("skin_readback:", r.SkinReadback)
	fmt.Println("morning:")
	for i, s := range r.Morning { fmt.Printf("  %d. %s\n", i+1, s) }
	fmt.Println("evening:")
	for i, s := range r.Evening { fmt.Printf("  %d. %s\n", i+1, s) }
	fmt.Println("safety_notes:", r.SafetyNotes)
	fmt.Println("closing_reminder:", r.ClosingReminder)
	hits := jargonHits(r.Encouragement, r.SkinReadback, r.SafetyNotes, r.ClosingReminder, strings.Join(r.Morning, " "), strings.Join(r.Evening, " "))
	printJargon(hits)
}

func jargonHits(parts ...string) []string {
	text := strings.Join(parts, "\n")
	pats := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bbarrier\b`),
		regexp.MustCompile(`(?i)\berythema\b`),
		regexp.MustCompile(`(?i)\bsebum\b`),
		regexp.MustCompile(`(?i)\bpapules?\b`),
		regexp.MustCompile(`(?i)\bcomedones?\b`),
		regexp.MustCompile(`(?i)\bhyperpigmentation\b`),
		regexp.MustCompile(`(?i)\binflammat\w*\b`),
		regexp.MustCompile(`(?i)\btexture\b`),
		regexp.MustCompile(`hang rao da`),
		regexp.MustCompile(`(?i)\bT-zone\b`),
	}
	var hits []string
	seen := map[string]bool{}
	for _, re := range pats {
		if m := re.FindString(text); m != "" && !seen[strings.ToLower(m)] {
			seen[strings.ToLower(m)] = true
			hits = append(hits, m)
		}
	}
	return hits
}

func printJargon(hits []string) {
	if len(hits) == 0 {
		fmt.Println("\nJARGON CHECK: OK")
		return
	}
	fmt.Println("\nJARGON CHECK: FAIL")
	for _, h := range hits { fmt.Println(" -", h) }
}