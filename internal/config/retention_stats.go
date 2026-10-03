package config

import "strings"

// RetentionStatsExcludedEmailSubstrings are plus-address fragments for eval
// and fixture accounts. GET /api/v1/admin/retention-stats drops any user whose
// email contains one of these (case-insensitive).
//
// Exact addresses in DADIARY_ADMIN_EMAILS and DADIARY_SKIN_REVIEW_EMAILS are
// excluded as well — see RetentionStatsExcludedEmails. Inactive and
// soft-deleted users are excluded in the query itself.
var RetentionStatsExcludedEmailSubstrings = []string{
	"+goalacne1003",
	"+nolabel1003",
	"+fe53test1003",
	"+dadiarytest0925",
}

// RetentionStatsExcludedEmails returns the exact addresses omitted from admin
// retention stats: full admins plus skin-review operators. Matching is
// case-insensitive. A nil config yields no exact-address exclusions (the
// substring list still applies).
func RetentionStatsExcludedEmails(cfg *Config) []string {
	if cfg == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(cfg.AdminEmails)+len(cfg.SkinReviewEmails))
	out := make([]string, 0, len(cfg.AdminEmails)+len(cfg.SkinReviewEmails))
	for _, list := range [][]string{cfg.AdminEmails, cfg.SkinReviewEmails} {
		for _, raw := range list {
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
	}
	return out
}
