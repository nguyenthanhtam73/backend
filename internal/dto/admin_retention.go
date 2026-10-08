package dto

// AdminRetentionStatsResponse is GET /api/v1/admin/retention-stats.
//
// Counts are all-time (or limited to users whose Vietnam registration day
// falls in from/to). A "day used" is one distinct skin_checks.check_date —
// the Vietnam civil day of a check-in (streaktime / Asia/Ho_Chi_Minh).
// Test accounts, admin and skin-review addresses, inactive users, and
// soft-deleted users are omitted. The handler only runs SELECT queries.
type AdminRetentionStatsResponse struct {
	RegisteredUsers  int64                      `json:"registered_users"`
	UsersWithCheckin int64                      `json:"users_with_checkin"`
	DaysUsed         AdminRetentionDaysUsed     `json:"days_used"`
	Consecutive      AdminRetentionConsecutive  `json:"consecutive"`
	ReturnNextDay    int64                      `json:"return_next_day"`
	BySignupWeek     []AdminRetentionSignupWeek `json:"by_signup_week"`
	From             *string                    `json:"from"`
	To               *string                    `json:"to"`
	Calendar         string                     `json:"calendar"`
	AsOf             string                     `json:"as_of"`
}

// AdminRetentionDaysUsed counts users by how many distinct Vietnam check-in
// days they have. AtLeast1 equals UsersWithCheckin. MaxDays is the largest
// distinct-day count among included users (0 when nobody has checked in).
type AdminRetentionDaysUsed struct {
	AtLeast1  int64 `json:"at_least_1"`
	AtLeast2  int64 `json:"at_least_2"`
	AtLeast7  int64 `json:"at_least_7"`
	AtLeast14 int64 `json:"at_least_14"`
	MaxDays   int64 `json:"max_days"`
}

// AdminRetentionConsecutive counts users whose longest run of consecutive
// Vietnam check-in days reaches the threshold. A single skipped day starts
// a new run. AtLeast1 equals UsersWithCheckin.
type AdminRetentionConsecutive struct {
	AtLeast1  int64 `json:"at_least_1"`
	AtLeast2  int64 `json:"at_least_2"`
	AtLeast7  int64 `json:"at_least_7"`
	AtLeast14 int64 `json:"at_least_14"`
}

// AdminRetentionSignupWeek is one ISO week of registration in Vietnam time
// (week label "2026-W37"). CheckedInOnce is users with at least one check-in
// day, not exactly one day.
type AdminRetentionSignupWeek struct {
	Week            string `json:"week"`
	Registered      int64  `json:"registered"`
	CheckedInOnce   int64  `json:"checked_in_once"`
	AtLeast2Days    int64  `json:"at_least_2_days"`
	ReturnedNextDay int64  `json:"returned_next_day"`
}
