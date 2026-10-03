package adminretention

import (
	"fmt"
	"strings"

	"github.com/dadiary/backend/internal/config"
	"gorm.io/gorm"
)

// dialect holds SQL fragments for one database. Signup days follow
// streaktime (Asia/Ho_Chi_Minh, UTC+7, no DST). check_date is already that
// civil day — stored as a date, or as UTC midnight of the Y-M-D — so it is
// not shifted again.
type dialect struct {
	name      string
	signupDay string
	checkDay  string
	island    string
	nextDay   string
	isoWeek   string
}

func dialectFor(db *gorm.DB) (dialect, error) {
	if db == nil || db.Dialector == nil {
		return dialect{}, ErrUnavailable
	}
	switch db.Dialector.Name() {
	case "postgres":
		return postgresDialect(), nil
	case "sqlite":
		return sqliteDialect(), nil
	default:
		return dialect{}, fmt.Errorf("%w: unsupported database %q", ErrUnavailable, db.Dialector.Name())
	}
}

func postgresDialect() dialect {
	signup := "((u.created_at AT TIME ZONE 'Asia/Ho_Chi_Minh')::date)"
	return dialect{
		name:      "postgres",
		signupDay: signup,
		checkDay:  "(sc.check_date)::date",
		// date - integer stays constant across a run of consecutive days.
		island:  "(d - CAST(ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY d) AS integer))",
		nextDay: "(dc.first_day + 1)",
		isoWeek: `to_char(e.signup_day, 'IYYY-"W"IW')`,
	}
}

func sqliteDialect() dialect {
	// created_at is a UTC instant (RFC3339). Vietnam is UTC+7 year-round,
	// the same offset streaktime uses when zoneinfo is missing.
	signup := "date(datetime(u.created_at, '+7 hours'))"
	return dialect{
		name:      "sqlite",
		signupDay: signup,
		checkDay:  "date(sc.check_date)",
		// julianday of a date is n.5; truncating keeps consecutive days 1 apart.
		island:  "(CAST(julianday(d) AS integer) - ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY d))",
		nextDay: "date(dc.first_day, '+1 day')",
		isoWeek: sqliteISOWeekSQL("e.signup_day"),
	}
}

// sqliteISOWeekSQL returns an expression for the ISO week of dayExpr
// ("2026-W37"). SQLite strftime has no %V / %G. The Thursday of the week
// decides the ISO year; week 1 is the week of 4 January.
func sqliteISOWeekSQL(dayExpr string) string {
	isoDow := "(((CAST(strftime('%w', " + dayExpr + ") AS integer) + 6) % 7) + 1)"
	// printf('%+d days', n) yields '+3 days' or '-2 days'. Concatenating '+'
	// with a negative number produces '+-2 days', which SQLite rejects.
	thursday := "date(" + dayExpr + ", printf('%+d days', 4 - " + isoDow + "))"
	isoYear := "CAST(strftime('%Y', " + thursday + ") AS integer)"
	jan4 := "printf('%04d-01-04', " + isoYear + ")"
	jan4Dow := "(((CAST(strftime('%w', " + jan4 + ") AS integer) + 6) % 7) + 1)"
	week1Monday := "date(" + jan4 + ", printf('%+d days', 1 - " + jan4Dow + "))"
	week := "CAST(ROUND((julianday(" + thursday + ") - julianday(" + week1Monday + ")) / 7.0) AS integer) + 1"
	return "printf('%04d-W%02d', " + isoYear + ", " + week + ")"
}

func (d dialect) compareDay(expr, op string) string {
	if d.name == "postgres" {
		return expr + " " + op + " CAST(? AS date)"
	}
	return expr + " " + op + " ?"
}

// userWhere is the eligible-user predicate. args begins with is_active.
func (d dialect) userWhere(excludedEmails []string, from, to *string) (string, []any) {
	var b strings.Builder
	args := []any{true}
	b.WriteString("u.deleted_at IS NULL AND u.is_active = ?")

	emails := normalizeEmails(excludedEmails)
	if len(emails) > 0 {
		b.WriteString(" AND lower(u.email) NOT IN (")
		for i, email := range emails {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("?")
			args = append(args, email)
		}
		b.WriteString(")")
	}
	for _, frag := range config.RetentionStatsExcludedEmailSubstrings {
		b.WriteString(` AND lower(u.email) NOT LIKE ? ESCAPE '\'`)
		args = append(args, likeContains(frag))
	}
	if from != nil {
		b.WriteString(" AND ")
		b.WriteString(d.compareDay(d.signupDay, ">="))
		args = append(args, *from)
	}
	if to != nil {
		b.WriteString(" AND ")
		b.WriteString(d.compareDay(d.signupDay, "<="))
		args = append(args, *to)
	}
	return b.String(), args
}

func normalizeEmails(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		email := strings.ToLower(strings.TrimSpace(raw))
		if email == "" {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	return out
}

func likeContains(fragment string) string {
	frag := strings.ToLower(strings.TrimSpace(fragment))
	frag = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(frag)
	return "%" + frag + "%"
}

func (d dialect) withEligible(where string) string {
	return `
WITH eligible AS (
  SELECT u.id AS id, ` + d.signupDay + ` AS signup_day
  FROM users AS u
  WHERE ` + where + `
),
days AS (
  SELECT DISTINCT sc.user_id AS user_id, ` + d.checkDay + ` AS d
  FROM skin_checks AS sc
  INNER JOIN eligible AS e ON e.id = sc.user_id
  WHERE sc.deleted_at IS NULL
),
day_counts AS (
  SELECT user_id, COUNT(*) AS n, MIN(d) AS first_day
  FROM days
  WHERE d IS NOT NULL
  GROUP BY user_id
),
numbered AS (
  SELECT user_id, d, ` + d.island + ` AS grp
  FROM days
  WHERE d IS NOT NULL
),
runs AS (
  SELECT user_id, COUNT(*) AS run_len
  FROM numbered
  GROUP BY user_id, grp
),
longest AS (
  SELECT user_id, MAX(run_len) AS longest
  FROM runs
  GROUP BY user_id
),
returned AS (
  SELECT DISTINCT dc.user_id AS user_id
  FROM day_counts AS dc
  INNER JOIN days AS chk ON chk.user_id = dc.user_id AND chk.d = ` + d.nextDay + `
)`
}

func (d dialect) summarySQL(where string) string {
	return d.withEligible(where) + `
SELECT
  (SELECT COUNT(*) FROM eligible) AS registered_users,
  (SELECT COUNT(*) FROM day_counts) AS users_with_checkin,
  (SELECT COUNT(*) FROM day_counts WHERE n >= 1) AS days_at_least_1,
  (SELECT COUNT(*) FROM day_counts WHERE n >= 2) AS days_at_least_2,
  (SELECT COUNT(*) FROM day_counts WHERE n >= 7) AS days_at_least_7,
  (SELECT COUNT(*) FROM day_counts WHERE n >= 14) AS days_at_least_14,
  (SELECT COALESCE(MAX(n), 0) FROM day_counts) AS max_days,
  (SELECT COUNT(*) FROM longest WHERE longest >= 1) AS consec_at_least_1,
  (SELECT COUNT(*) FROM longest WHERE longest >= 2) AS consec_at_least_2,
  (SELECT COUNT(*) FROM longest WHERE longest >= 7) AS consec_at_least_7,
  (SELECT COUNT(*) FROM longest WHERE longest >= 14) AS consec_at_least_14,
  (SELECT COUNT(*) FROM returned) AS return_next_day`
}

func (d dialect) cohortSQL(where string) string {
	return d.withEligible(where) + `
SELECT
  iso_week AS week,
  COUNT(*) AS registered,
  SUM(CASE WHEN n >= 1 THEN 1 ELSE 0 END) AS checked_in_once,
  SUM(CASE WHEN n >= 2 THEN 1 ELSE 0 END) AS at_least_2_days,
  SUM(came_back) AS returned_next_day
FROM (
  SELECT
    ` + d.isoWeek + ` AS iso_week,
    COALESCE(dc.n, 0) AS n,
    CASE WHEN r.user_id IS NOT NULL THEN 1 ELSE 0 END AS came_back
  FROM eligible AS e
  LEFT JOIN day_counts AS dc ON dc.user_id = e.id
  LEFT JOIN returned AS r ON r.user_id = e.id
) AS per_user
GROUP BY iso_week
ORDER BY iso_week`
}
