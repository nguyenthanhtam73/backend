package mascot

import (
	"testing"

	"github.com/dadiary/backend/internal/domain"
)

func TestValidateLoadout(t *testing.T) {
	catalog := map[string]domain.MascotItem{}
	for _, item := range domain.DefaultMascotItems() {
		catalog[item.ID] = item
	}
	price := 20
	catalog["outfit_gold"] = domain.MascotItem{ID: "outfit_gold", Layer: domain.MascotLayerOutfit, IsFree: false, Price: &price}
	catalog["hat_paid"] = domain.MascotItem{ID: "hat_paid", Layer: domain.MascotLayerAccessory, IsFree: false, Price: &price}

	ownedFree := map[string]struct{}{}
	for _, item := range domain.DefaultMascotItems() {
		ownedFree[item.ID] = struct{}{}
	}

	ok := domain.DefaultMascotLoadout()
	if err := ValidateLoadout(ok, catalog, ownedFree); err != nil {
		t.Fatal(err)
	}

	emptyAccessory := ok
	emptyAccessory.Accessory = ""
	emptyAccessory.RoomItems = nil
	if err := ValidateLoadout(emptyAccessory, catalog, ownedFree); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		edit func(*domain.MascotLoadout)
		code string
		own  map[string]struct{}
	}{
		{"missing body", func(l *domain.MascotLoadout) { l.Body = "" }, "invalid_loadout", nil},
		{"unknown", func(l *domain.MascotLoadout) { l.Hair = "hair_nope" }, "unknown_item", nil},
		{"wrong layer", func(l *domain.MascotLoadout) { l.Body = domain.MascotItemHairSleepBuns }, "wrong_layer", nil},
		{"not owned", func(l *domain.MascotLoadout) { l.Outfit = "outfit_gold" }, "item_not_owned", nil},
		{"free but not granted", func(l *domain.MascotLoadout) { l.Hair = domain.MascotItemHairSleepBuns }, "item_not_owned", map[string]struct{}{}},
		{"bad id", func(l *domain.MascotLoadout) { l.Hair = "Hair Default" }, "invalid_loadout", nil},
		{"dup room", func(l *domain.MascotLoadout) {
			l.RoomItems = []string{domain.MascotItemRoomPlant, domain.MascotItemRoomPlant}
		}, "invalid_loadout", nil},
		{"room layer", func(l *domain.MascotLoadout) { l.RoomItems = []string{domain.MascotItemBodyChibi} }, "wrong_layer", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := ok
			l.RoomItems = append([]string(nil), ok.RoomItems...)
			tc.edit(&l)
			owned := ownedFree
			if tc.own != nil {
				owned = tc.own
			}
			err := ValidateLoadout(l, catalog, owned)
			ae, is := domain.AsAppError(err)
			if !is || ae.Code != tc.code {
				t.Fatalf("got %v", err)
			}
		})
	}

	withHat := ok
	withHat.Accessory = "hat_paid"
	ownedHat := map[string]struct{}{"hat_paid": {}}
	for id := range ownedFree {
		ownedHat[id] = struct{}{}
	}
	if err := ValidateLoadout(withHat, catalog, ownedHat); err != nil {
		t.Fatal(err)
	}
}
