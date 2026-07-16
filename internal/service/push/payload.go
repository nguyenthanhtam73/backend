package push

import (
	"math/rand"
	"strings"
	"time"
)

// dailyReminderCopy is one title/body pair for the daily check-in nudge.
// Warm, coach-like VN tone — short enough for the notification shade.
type dailyReminderCopy struct {
	Title string
	Body  string
}

// dailyReminderVariants rotate lightly so the same user doesn't see identical
// copy every evening. Add more lines here anytime — no schema change needed.
var dailyReminderVariants = []dailyReminderCopy{
	{
		Title: "Hôm nay da bạn thế nào?",
		Body:  "Một phút check-in da thôi, streak của bạn đang chờ nè 🔥",
	},
	{
		Title: "DaDiary nhắc bạn",
		Body:  "Dành chút thời gian check-in da nhé ✨ streak đang chờ bạn!",
	},
	{
		Title: "Da ơi, check-in nhẹ thôi",
		Body:  "Chăm da đều là cách giữ da khỏe. Hôm nay bạn check-in chưa?",
	},
}

func pickDailyReminderCopy() dailyReminderCopy {
	if len(dailyReminderVariants) == 0 {
		return dailyReminderCopy{
			Title: "DaDiary nhắc bạn",
			Body:  "Check-in da hôm nay nhé ✨",
		}
	}
	return dailyReminderVariants[rand.Intn(len(dailyReminderVariants))]
}

// streakAtRiskVariants — days_since == 1, streak still alive until end of day.
var streakAtRiskVariants = []dailyReminderCopy{
	{
		Title: "Streak của bạn sắp gãy rồi!",
		Body:  "Bạn đã bỏ lỡ 1 ngày. Check-in hôm nay để giữ chuỗi ngày nhé 🔥",
	},
	{
		Title: "Streak đang lung lay đó!",
		Body:  "Còn hôm nay để cứu chuỗi. Check-in một phát thôi ✨",
	},
	{
		Title: "Đừng để streak đứt nha",
		Body:  "Hôm qua miss rồi — check-in hôm nay là giữ được chuỗi 🔥",
	},
}

func pickStreakAtRiskCopy() dailyReminderCopy {
	if len(streakAtRiskVariants) == 0 {
		return dailyReminderCopy{
			Title: "Streak của bạn sắp gãy rồi!",
			Body:  "Check-in hôm nay để giữ chuỗi ngày nhé 🔥",
		}
	}
	return streakAtRiskVariants[rand.Intn(len(streakAtRiskVariants))]
}

// NotificationType selects copy + defaults for a product notification.
// Add new kinds here as reminders / streak alerts ship.
type NotificationType string

const (
	NotificationTypeTest          NotificationType = "test"
	NotificationTypeDailyReminder NotificationType = "daily_reminder"
	NotificationTypeStreakAtRisk  NotificationType = "streak_at_risk"
)

// Web Push TTL (seconds): how long the push service may retain an undelivered
// message while the device is offline. Short TTLs (e.g. 60s) drop evening
// reminders if the phone was briefly offline — too aggressive for DaDiary.
const (
	// TTLDailyReminder — keep overnight / next-day delivery window.
	TTLDailyReminder = 24 * 60 * 60 // 86400
	// TTLStreakAtRisk — urgent but still useful if the device wakes later tonight.
	TTLStreakAtRisk = 12 * 60 * 60 // 43200
	// TTLTest — short-lived; test pushes are interactive and time-sensitive.
	TTLTest = 1 * 60 * 60 // 3600
	// TTLDefault — safe fallback for unknown types (at least 8h).
	TTLDefault = 8 * 60 * 60 // 28800
)

// TTLForType returns the Web Push TTL for a notification kind.
func TTLForType(nType NotificationType) int {
	switch nType {
	case NotificationTypeDailyReminder:
		return TTLDailyReminder
	case NotificationTypeStreakAtRisk:
		return TTLStreakAtRisk
	case NotificationTypeTest:
		return TTLTest
	default:
		return TTLDefault
	}
}

// TTLForPayload reads data["type"] when present, else TTLDefault.
func TTLForPayload(p NotificationPayload) int {
	if p.Data != nil {
		if raw, ok := p.Data["type"].(string); ok && strings.TrimSpace(raw) != "" {
			return TTLForType(NotificationType(strings.TrimSpace(raw)))
		}
	}
	return TTLDefault
}

// DefaultVibrate is a short double-pulse suitable for mobile reminders.
var DefaultVibrate = []int{100, 50, 100}

// BuildNotificationPayload builds a rich Web Push payload for a given type.
//
// `data` is merged into payload.Data (e.g. override "url"). Special top-level
// overrides recognised in `data`:
//   - "image" (string): hero / big-picture URL
//   - "badge" (string): Android status-bar badge icon URL
//   - "url" (string): deep-link path opened on click (also stored in Data)
//   - "silent" (bool): suppress OS sound/vibration
//   - "requireInteraction" (bool): keep notification until user acts
//
// Web Push cannot play a custom sound file — only Silent=true/false. The OS
// and the user's notification settings decide whether anything is heard.
//
// Example:
//
//	payload := push.BuildNotificationPayload(push.NotificationTypeTest, map[string]any{
//	    "url":   "/check-in",
//	    "image": "/icons/icon-512.png",
//	    "badge": "/icons/icon-192.png",
//	})
func BuildNotificationPayload(nType NotificationType, data map[string]any) NotificationPayload {
	base := NotificationPayload{
		Icon:      DefaultNotificationIcon,
		Badge:     DefaultNotificationBadge,
		Image:     DefaultNotificationImage,
		Renotify:  true,
		Silent:    false, // explicit: allow OS default sound (not a custom file)
		Timestamp: time.Now().UnixMilli(),
		Vibrate:   append([]int(nil), DefaultVibrate...),
		Data: map[string]any{
			"type": string(nType),
			"url":  DefaultNotificationURL,
		},
	}

	switch nType {
	case NotificationTypeTest:
		base.Title = "DaDiary nhắc bạn"
		base.Body = "Đã đến giờ chăm sóc da hôm nay chưa? ✨"
		base.Tag = "dadiary-test"
		base.RequireInteraction = true // stays until dismissed while testing UX
		base.Data["action"] = "check-in"
		base.Actions = []NotificationAction{
			{Action: "check-in", Title: "Check-in ngay"},
			{Action: "later", Title: "Để sau"},
		}

	case NotificationTypeDailyReminder:
		// Warm coach-style nudge; variant picked at send time to reduce fatigue.
		copy := pickDailyReminderCopy()
		base.Title = copy.Title
		base.Body = copy.Body
		base.Tag = "dadiary-daily-reminder"
		base.RequireInteraction = false
		base.Data["action"] = "check-in"
		base.Actions = []NotificationAction{
			{Action: "check-in", Title: "Check-in ngay"},
			{Action: "later", Title: "Để sau"},
		}

	case NotificationTypeStreakAtRisk:
		// Urgent but warm — user missed yesterday and hasn't checked in today.
		copy := pickStreakAtRiskCopy()
		base.Title = copy.Title
		base.Body = copy.Body
		base.Tag = "dadiary-streak-at-risk"
		base.RequireInteraction = true // important — don't auto-dismiss
		base.Data["action"] = "check-in"
		base.Actions = []NotificationAction{
			{Action: "check-in", Title: "Check-in ngay"},
			{Action: "later", Title: "Để sau"},
		}

	default:
		base.Title = "DaDiary"
		base.Body = "Bạn có một cập nhật mới."
		base.Tag = "dadiary-generic"
		base.Data["action"] = "open"
	}

	// Caller overrides for top-level presentation fields + Data merges.
	for k, v := range data {
		if k == "" || v == nil {
			continue
		}
		switch k {
		case "image":
			if img, ok := v.(string); ok {
				if trimmed := strings.TrimSpace(img); trimmed != "" {
					base.Image = trimmed
				}
			}
		case "badge":
			if badge, ok := v.(string); ok {
				if trimmed := strings.TrimSpace(badge); trimmed != "" {
					base.Badge = trimmed
				}
			}
		case "silent":
			if silent, ok := v.(bool); ok {
				base.Silent = silent
			}
		case "requireInteraction":
			if ri, ok := v.(bool); ok {
				base.RequireInteraction = ri
			}
		default:
			base.Data[k] = v
		}
	}
	return base
}
