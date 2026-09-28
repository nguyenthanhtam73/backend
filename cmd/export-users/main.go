// export-users answers "where exactly do people stop?" and hands back the list
// needed to go ask them.
//
// Registration counts alone hide the drop-off: someone who signed up and left is a
// different problem from someone who finished onboarding, checked in twice, and never
// came back. This command buckets every account by how far it actually got and writes
// one CSV row per person so the follow-up email can be segmented.
//
// Usage:
//
//	go run ./cmd/export-users --env .env.prod-eval.local
//	go run ./cmd/export-users --env .env.prod-eval.local --out C:\tmp\drop-off.csv
//	go run ./cmd/export-users --env .env.prod-eval.local --segment onboarded_no_checkin
//
// PRIVACY: the CSV holds real user emails. It is written to the home directory by
// default (outside the repo and OneDrive); keep it that way, and delete it once the
// emails are sent.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/repository"
)

// segmentOrder is the funnel, in the order people fall out of it.
var segmentOrder = []string{
	"signup_only",
	"onboarded_no_checkin",
	"one_checkin",
	"two_checkins",
	"three_plus",
}

var segmentLabel = map[string]string{
	"signup_only":          "Đăng ký xong, chưa từng làm onboarding",
	"onboarded_no_checkin": "Onboarding xong, chưa check-in lần nào",
	"one_checkin":          "Check-in đúng 1 ngày",
	"two_checkins":         "Check-in 2 ngày",
	"three_plus":           "Check-in từ 3 ngày trở lên",
}

// row is one account plus how far it got. Days counts distinct check-in dates,
// not rows: two check-ins on the same day is still one day of the habit.
type row struct {
	Email         string
	Name          string
	SignedUp      time.Time
	DidOnboarding bool
	Days          int
	LastCheckIn   *time.Time
	Segment       string
}

const query = `
SELECT
  u.email,
  COALESCE(NULLIF(TRIM(u.display_name), ''), u.username) AS name,
  u.created_at                                           AS signed_up,
  (p.user_id IS NOT NULL)                                AS did_onboarding,
  COALESCE(c.days, 0)                                    AS days,
  c.last_check_in                                        AS last_check_in,
  CASE
    WHEN COALESCE(c.days, 0) = 0 AND p.user_id IS NULL THEN 'signup_only'
    WHEN COALESCE(c.days, 0) = 0                       THEN 'onboarded_no_checkin'
    WHEN c.days = 1                                    THEN 'one_checkin'
    WHEN c.days = 2                                    THEN 'two_checkins'
    ELSE 'three_plus'
  END                                                    AS segment
FROM users u
LEFT JOIN skin_profiles p
  ON p.user_id = u.id AND p.deleted_at IS NULL
LEFT JOIN (
  SELECT user_id,
         COUNT(DISTINCT check_date) AS days,
         MAX(check_date)            AS last_check_in
  FROM skin_checks
  WHERE deleted_at IS NULL
  GROUP BY user_id
) c ON c.user_id = u.id
WHERE u.deleted_at IS NULL
  AND u.is_active = TRUE
ORDER BY u.created_at
`

func main() {
	envPath := flag.String("env", ".env", "env file to load (needs DADIARY_DATABASE_URL)")
	out := flag.String("out", defaultOutPath(), "CSV output path")
	segment := flag.String("segment", "", "only export one segment ("+strings.Join(segmentOrder, ", ")+")")
	includeStaff := flag.Bool("include-staff", false, "keep admin / skin-review operator accounts")
	flag.Parse()

	if *segment != "" && segmentLabel[*segment] == "" {
		fail("unknown --segment %q; want one of: %s", *segment, strings.Join(segmentOrder, ", "))
	}

	cfg, err := config.Load(*envPath)
	if err != nil {
		fail("config: %v", err)
	}
	db, err := repository.NewPostgres(cfg)
	if err != nil {
		fail("database: %v", err)
	}

	var rows []row
	if err := db.Raw(query).Scan(&rows).Error; err != nil {
		fail("query: %v", err)
	}

	kept := rows[:0]
	staff := 0
	for _, r := range rows {
		if !*includeStaff && cfg.CanSkinReviewEmail(r.Email) {
			staff++
			continue
		}
		kept = append(kept, r)
	}
	rows = kept

	printFunnel(rows, staff)

	if *segment != "" {
		filtered := make([]row, 0, len(rows))
		for _, r := range rows {
			if r.Segment == *segment {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	if len(rows) == 0 {
		fmt.Println("\nNo accounts matched — nothing written.")
		return
	}
	if err := writeCSV(*out, rows); err != nil {
		fail("write csv: %v", err)
	}

	abs, _ := filepath.Abs(*out)
	fmt.Printf("\nWrote %d rows → %s\n", len(rows), abs)
	fmt.Println("This file contains real user emails. Delete it once the emails are sent.")
}

// printFunnel is the point of the command: where the drop-off actually is.
func printFunnel(rows []row, staff int) {
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Segment]++
	}
	total := len(rows)

	fmt.Printf("%d accounts (excluding %d staff)\n\n", total, staff)
	for _, seg := range segmentOrder {
		n := counts[seg]
		pct := 0.0
		if total > 0 {
			pct = float64(n) / float64(total) * 100
		}
		fmt.Printf("  %-22s %4d  %5.1f%%  %s  %s\n",
			seg, n, pct, bar(pct), segmentLabel[seg])
	}

	reached := total - counts["signup_only"] - counts["onboarded_no_checkin"]
	fmt.Printf("\n  Chạm được vòng lặp (>=1 ngày check-in): %d / %d\n", reached, total)
	fmt.Printf("  Qua được ngày 2:                        %d / %d\n", counts["three_plus"], total)
}

func bar(pct float64) string {
	n := int(pct/5 + 0.5)
	if n > 20 {
		n = 20
	}
	return strings.Repeat("#", n) + strings.Repeat("·", 20-n)
}

func writeCSV(path string, rows []row) error {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Segment != rows[j].Segment {
			return indexOf(rows[i].Segment) < indexOf(rows[j].Segment)
		}
		return rows[i].SignedUp.Before(rows[j].SignedUp)
	})

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{
		"email", "name", "segment", "signed_up", "did_onboarding", "checkin_days", "last_check_in",
	}); err != nil {
		return err
	}
	for _, r := range rows {
		last := ""
		if r.LastCheckIn != nil {
			last = r.LastCheckIn.Format("2006-01-02")
		}
		if err := w.Write([]string{
			r.Email,
			r.Name,
			r.Segment,
			r.SignedUp.Format("2006-01-02"),
			strconv.FormatBool(r.DidOnboarding),
			strconv.Itoa(r.Days),
			last,
		}); err != nil {
			return err
		}
	}
	return w.Error()
}

// defaultOutPath is the home directory rather than a path relative to the repo:
// the checkout lives under a cloud-synced folder, which would upload the emails.
func defaultOutPath() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "dadiary-users.csv")
	}
	return filepath.Join(os.TempDir(), "dadiary-users.csv")
}

func indexOf(seg string) int {
	for i, s := range segmentOrder {
		if s == seg {
			return i
		}
	}
	return len(segmentOrder)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "export-users: "+format+"\n", args...)
	os.Exit(1)
}
