package domain

import "testing"

func TestMilestoneCatalogForPlan_FreeIsBasic(t *testing.T) {
	days := MilestoneDaysForPlan(PlanFree)
	if len(days) != 2 || days[0] != 3 || days[1] != 7 {
		t.Fatalf("free catalog = %v, want [3 7]", days)
	}
}

func TestMilestoneCatalogForPlan_PremiumIsFull(t *testing.T) {
	days := MilestoneDaysForPlan(PlanPremium)
	if len(days) != len(FullMilestoneDays) {
		t.Fatalf("premium catalog len=%d, want %d", len(days), len(FullMilestoneDays))
	}
	for i, want := range FullMilestoneDays {
		if days[i] != want {
			t.Fatalf("premium days[%d]=%d, want %d", i, days[i], want)
		}
	}
}
