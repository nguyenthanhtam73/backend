package domain

import "testing"

func TestPublicOutfitHidesAdminTestShirt(t *testing.T) {
	if PublicOutfit(MascotAdminTestShirt, false) != MascotDefaultTrialOutfit {
		t.Fatal("non-admin was shown the test shirt")
	}
	if PublicOutfit(MascotAdminTestShirt, true) != MascotAdminTestShirt {
		t.Fatal("admin lost the test shirt")
	}
	if PublicOutfit("outfit_pajamas", false) != "outfit_pajamas" {
		t.Fatal("default outfit changed")
	}
	if !AdminOnlyClosetItem(MascotAdminTestShirt) || AdminOnlyClosetItem("outfit_pajamas") {
		t.Fatal("closet flag")
	}
}
