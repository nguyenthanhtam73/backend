package domain

import "testing"

func TestDailyQuestPayoutIsTheOnlySchedule(t *testing.T) {
	if DailyQuestMorningCoins != 8 || DailyQuestEveningCoins != 8 || DailyQuestBonusCoins != 2 {
		t.Fatalf("amounts morning %d evening %d bonus %d", DailyQuestMorningCoins, DailyQuestEveningCoins, DailyQuestBonusCoins)
	}
	total := DailyQuestMorningCoins + DailyQuestEveningCoins + DailyQuestBonusCoins
	if total != 18 {
		t.Fatalf("day total %d", total)
	}
	if DailyQuestSessionCoins(DailyQuestSessionMorning) != DailyQuestMorningCoins {
		t.Fatal("morning coins")
	}
	if DailyQuestSessionCoins(DailyQuestSessionEvening) != DailyQuestEveningCoins {
		t.Fatal("evening coins")
	}
	if DailyQuestSessionCoins("noon") != 0 || DailyQuestSessionReason("noon") != "" {
		t.Fatal("unknown session must not pay")
	}
	if DailyQuestSessionReason(DailyQuestSessionMorning) != CoinReasonDailyQuestMorning {
		t.Fatal("morning reason")
	}
	if DailyQuestSessionReason(DailyQuestSessionEvening) != CoinReasonDailyQuestEvening {
		t.Fatal("evening reason")
	}
}

func TestNormalizeDailyQuestSession(t *testing.T) {
	got, ok := NormalizeDailyQuestSession(" Morning ")
	if !ok || got != DailyQuestSessionMorning {
		t.Fatalf("morning %q %v", got, ok)
	}
	got, ok = NormalizeDailyQuestSession("EVENING")
	if !ok || got != DailyQuestSessionEvening {
		t.Fatalf("evening %q %v", got, ok)
	}
	if _, ok := NormalizeDailyQuestSession("bonus"); ok {
		t.Fatal("bonus is not a session the client can claim")
	}
	if _, ok := NormalizeDailyQuestSession(""); ok {
		t.Fatal("empty")
	}
}
