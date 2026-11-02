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

	ok := domain.DefaultMascotLoadout()
	if err := ValidateLoadout(ok, catalog, nil); err != nil {
		t.Fatal(err)
	}

	emptyAccessory := ok
	emptyAccessory.Accessory = ""
	emptyAccessory.RoomItems = nil
	if err := ValidateLoadout(emptyAccessory, catalog, nil); err != nil {
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
		{"wrong layer", func(l *domain.MascotLoadout) { l.Body = domain.MascotItemHairDefault }, "wrong_layer", nil},
		{"paid", func(l *domain.MascotLoadout) { l.Outfit = "outfit_gold" }, "item_not_free", nil},
		{"bad id", func(l *domain.MascotLoadout) { l.Hair = "Hair Default" }, "invalid_loadout", nil},
		{"dup room", func(l *domain.MascotLoadout) {
			l.RoomItems = []string{domain.MascotItemRoomPlant, domain.MascotItemRoomPlant}
		}, "invalid_loadout", nil},
		{"room layer", func(l *domain.MascotLoadout) { l.RoomItems = []string{domain.MascotItemBodyDefault} }, "wrong_layer", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := ok
			l.RoomItems = append([]string(nil), ok.RoomItems...)
			tc.edit(&l)
			err := ValidateLoadout(l, catalog, tc.own)
			ae, is := domain.AsAppError(err)
			if !is || ae.Code != tc.code {
				t.Fatalf("got %v", err)
			}
		})
	}

	owned := ok
	owned.Accessory = "hat_paid"
	if err := ValidateLoadout(owned, catalog, map[string]struct{}{"hat_paid": {}}); err != nil {
		t.Fatal(err)
	}
}
