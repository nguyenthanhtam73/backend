package push

import "testing"

func TestTTLForType(t *testing.T) {
	if TTLForType(NotificationTypeDailyReminder) != TTLDailyReminder {
		t.Fatalf("daily TTL: got %d", TTLForType(NotificationTypeDailyReminder))
	}
	if TTLForType(NotificationTypeStreakAtRisk) != TTLStreakAtRisk {
		t.Fatalf("streak TTL: got %d", TTLForType(NotificationTypeStreakAtRisk))
	}
	if TTLForType(NotificationTypeTest) != TTLTest {
		t.Fatalf("test TTL: got %d", TTLForType(NotificationTypeTest))
	}
	p := BuildNotificationPayload(NotificationTypeDailyReminder, nil)
	if TTLForPayload(p) != TTLDailyReminder {
		t.Fatalf("TTLForPayload daily: got %d", TTLForPayload(p))
	}
	if TTLDailyReminder < 8*60*60 || TTLStreakAtRisk < 8*60*60 {
		t.Fatal("reminder TTLs must be at least 8 hours")
	}
}

func TestBuildNotificationPayload_TestType(t *testing.T) {
	p := BuildNotificationPayload(NotificationTypeTest, nil)
	if p.Title == "" || p.Body == "" {
		t.Fatalf("expected title/body, got %+v", p)
	}
	if p.Tag != "dadiary-test" {
		t.Fatalf("tag: got %q", p.Tag)
	}
	if !p.Renotify {
		t.Fatal("expected renotify")
	}
	if p.Image != DefaultNotificationImage {
		t.Fatalf("image: got %q", p.Image)
	}
	if p.Timestamp <= 0 {
		t.Fatalf("timestamp: got %d", p.Timestamp)
	}
	if len(p.Vibrate) != 3 || p.Vibrate[0] != 100 {
		t.Fatalf("vibrate: got %v", p.Vibrate)
	}
	if p.Data["action"] != "check-in" {
		t.Fatalf("data.action: got %v", p.Data["action"])
	}
	if p.Data["type"] != string(NotificationTypeTest) {
		t.Fatalf("data.type: got %v", p.Data["type"])
	}
	if p.Data["url"] != DefaultNotificationURL {
		t.Fatalf("data.url: got %v", p.Data["url"])
	}
	if len(p.Actions) != 2 {
		t.Fatalf("actions: got %d", len(p.Actions))
	}
	if p.Silent {
		t.Fatal("test payload should allow OS sound (silent=false)")
	}
	if !p.RequireInteraction {
		t.Fatal("test payload should requireInteraction")
	}
	if p.Badge != DefaultNotificationBadge {
		t.Fatalf("badge: got %q", p.Badge)
	}
}

func TestBuildNotificationPayload_DailyReminderCopy(t *testing.T) {
	known := make(map[string]string, len(dailyReminderVariants))
	for _, v := range dailyReminderVariants {
		known[v.Title] = v.Body
	}

	// A few draws — each must be one of the configured variants.
	for i := 0; i < 12; i++ {
		p := BuildNotificationPayload(NotificationTypeDailyReminder, nil)
		body, ok := known[p.Title]
		if !ok {
			t.Fatalf("unexpected title %q", p.Title)
		}
		if p.Body != body {
			t.Fatalf("body mismatch for title %q: got %q want %q", p.Title, p.Body, body)
		}
		if p.Tag != "dadiary-daily-reminder" {
			t.Fatalf("tag: got %q", p.Tag)
		}
		if p.Data["type"] != string(NotificationTypeDailyReminder) {
			t.Fatalf("data.type: got %v", p.Data["type"])
		}
		if p.Data["action"] != "check-in" {
			t.Fatalf("data.action: got %v", p.Data["action"])
		}
	}
}

func TestBuildNotificationPayload_StreakAtRiskCopy(t *testing.T) {
	known := make(map[string]string, len(streakAtRiskVariants))
	for _, v := range streakAtRiskVariants {
		known[v.Title] = v.Body
	}
	for i := 0; i < 12; i++ {
		p := BuildNotificationPayload(NotificationTypeStreakAtRisk, nil)
		body, ok := known[p.Title]
		if !ok {
			t.Fatalf("unexpected title %q", p.Title)
		}
		if p.Body != body {
			t.Fatalf("body mismatch for %q: got %q", p.Title, p.Body)
		}
		if p.Tag != "dadiary-streak-at-risk" {
			t.Fatalf("tag: got %q", p.Tag)
		}
		if !p.RequireInteraction {
			t.Fatal("streak_at_risk should requireInteraction")
		}
	}
}

func TestBuildNotificationPayload_OverridesDataAndImage(t *testing.T) {
	p := BuildNotificationPayload(NotificationTypeDailyReminder, map[string]any{
		"url":   "/progress",
		"image": "/splash/iphone-15-pro.png",
		"badge": "/favicon-32.png",
	})
	if p.Data["url"] != "/progress" {
		t.Fatalf("override url: got %v", p.Data["url"])
	}
	if p.Image != "/splash/iphone-15-pro.png" {
		t.Fatalf("override image: got %q", p.Image)
	}
	if p.Badge != "/favicon-32.png" {
		t.Fatalf("override badge: got %q", p.Badge)
	}
	if p.Data["type"] != string(NotificationTypeDailyReminder) {
		t.Fatalf("type should stay: got %v", p.Data["type"])
	}
	if _, ok := p.Data["image"]; ok {
		t.Fatal("image should be top-level only, not duplicated into Data")
	}
	if _, ok := p.Data["badge"]; ok {
		t.Fatal("badge should be top-level only, not duplicated into Data")
	}
}
