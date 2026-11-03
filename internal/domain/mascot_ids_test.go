package domain

import (
	"os"
	"strings"
	"testing"
)

// frontendCatalog is MASCOT_CATALOG from frontend lib/mascot/catalog.ts
// on branch cursor/be-da-mascot-3bf9 (PR nguyenthanhtam73/frontend#82).
var frontendCatalog = []MascotItem{
	{ID: "body_chibi", Layer: MascotLayerBody, Name: "Bé Da", IsFree: true},
	{ID: "hair_sleep_buns", Layer: MascotLayerHair, Name: "Tóc búi ngủ", IsFree: true},
	{ID: "outfit_cozy_pajamas", Layer: MascotLayerOutfit, Name: "Pijama ấm", IsFree: true},
	{ID: "accessory_star_clip", Layer: MascotLayerAccessory, Name: "Kẹp sao", IsFree: true},
	{ID: "accessory_pink_headband", Layer: MascotLayerAccessory, Name: "Băng đô hồng", IsFree: true},
	{ID: "room_moon", Layer: MascotLayerRoom, Name: "Trăng", IsFree: true},
	{ID: "room_plant", Layer: MascotLayerRoom, Name: "Cây nhỏ", IsFree: true},
	{ID: "room_pillow", Layer: MascotLayerRoom, Name: "Gối", IsFree: true},
}

func TestDefaultMascotIDsMatchFrontendCatalog(t *testing.T) {
	got := DefaultMascotItems()
	if len(got) != len(frontendCatalog) {
		t.Fatalf("seeded %d ids, catalog.ts %d", len(got), len(frontendCatalog))
	}
	for i, item := range got {
		want := frontendCatalog[i]
		if item.ID != want.ID || item.Layer != want.Layer || item.Name != want.Name || !item.IsFree {
			t.Fatalf("item %d: got %+v want %+v", i, item, want)
		}
	}

	loadout := DefaultMascotLoadout()
	if loadout.Body != "body_chibi" || loadout.Hair != "hair_sleep_buns" || loadout.Outfit != "outfit_cozy_pajamas" {
		t.Fatalf("loadout %+v", loadout)
	}
	wantAcc := []string{"accessory_pink_headband", "accessory_star_clip"}
	if len(loadout.Accessories) != len(wantAcc) {
		t.Fatalf("accessories %+v", loadout.Accessories)
	}
	for i, id := range wantAcc {
		if loadout.Accessories[i] != id {
			t.Fatalf("accessories %+v", loadout.Accessories)
		}
	}
	if loadout.Accessory != wantAcc[0] {
		t.Fatalf("accessory compat field %q, want first %q", loadout.Accessory, wantAcc[0])
	}
	wantRooms := []string{"room_moon", "room_plant", "room_pillow"}
	if len(loadout.RoomItems) != len(wantRooms) {
		t.Fatalf("rooms %+v", loadout.RoomItems)
	}
	for i, id := range wantRooms {
		if loadout.RoomItems[i] != id {
			t.Fatalf("rooms %+v", loadout.RoomItems)
		}
	}

	raw, err := os.ReadFile("../../migrations/030_user_mascot.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, item := range frontendCatalog {
		if !strings.Contains(sql, "'"+item.ID+"'") {
			t.Fatalf("migration missing %s", item.ID)
		}
	}
	for _, retired := range []string{"body_default", "hair_default", "outfit_default", "accessory_default"} {
		if strings.Contains(sql, retired) {
			t.Fatalf("migration still has %s", retired)
		}
	}
}
