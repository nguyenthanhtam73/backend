package domain

import (
	"os"
	"strings"
	"testing"
)

// frontendMascotItemIDs is the equippable set from frontend PR
// nguyenthanhtam73/frontend#82, branch cursor/be-da-mascot-3bf9:
// public/mascot asset filenames plus DEFAULT_LOADOUT in lib/mascot/model.ts.
// Mood-only files (body_sit, body_back, faces, messy, fx) are not shop items.
var frontendMascotItemIDs = map[string]string{
	"body_chibi":          MascotLayerBody,
	"hair_sleep_buns":     MascotLayerHair,
	"outfit_cozy_pajamas": MascotLayerOutfit,
	"accessory_star_clip": MascotLayerAccessory,
	"room_moon":           MascotLayerRoom,
	"room_plant":          MascotLayerRoom,
	"room_pillow":         MascotLayerRoom,
}

func TestDefaultMascotIDsMatchFrontendAssets(t *testing.T) {
	seen := map[string]struct{}{}
	for _, item := range DefaultMascotItems() {
		layer, ok := frontendMascotItemIDs[item.ID]
		if !ok {
			t.Fatalf("seeded id %q is not a frontend mascot asset", item.ID)
		}
		if item.Layer != layer {
			t.Fatalf("%s layer %s, frontend %s", item.ID, item.Layer, layer)
		}
		if !item.IsFree {
			t.Fatalf("%s should be a free default", item.ID)
		}
		seen[item.ID] = struct{}{}
	}
	if len(seen) != len(frontendMascotItemIDs) {
		t.Fatalf("seeded %d ids, frontend catalog %d", len(seen), len(frontendMascotItemIDs))
	}

	loadout := DefaultMascotLoadout()
	for _, id := range []string{loadout.Body, loadout.Hair, loadout.Outfit, loadout.Accessory} {
		if _, ok := seen[id]; !ok {
			t.Fatalf("default loadout id %q is not seeded", id)
		}
	}
	if loadout.Body != "body_chibi" ||
		loadout.Hair != "hair_sleep_buns" ||
		loadout.Outfit != "outfit_cozy_pajamas" ||
		loadout.Accessory != "accessory_star_clip" {
		t.Fatalf("loadout %+v", loadout)
	}
	wantRooms := []string{"room_moon", "room_plant", "room_pillow"}
	if len(loadout.RoomItems) != len(wantRooms) {
		t.Fatalf("rooms %+v", loadout.RoomItems)
	}
	for i, id := range wantRooms {
		if loadout.RoomItems[i] != id {
			t.Fatalf("rooms %+v", loadout.RoomItems)
		}
		if _, ok := seen[id]; !ok {
			t.Fatalf("room %s is not seeded", id)
		}
	}

	raw, err := os.ReadFile("../../migrations/030_user_mascot.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for id := range frontendMascotItemIDs {
		if !strings.Contains(sql, "'"+id+"'") {
			t.Fatalf("migration missing %s", id)
		}
	}
	for _, retired := range []string{"body_default", "hair_default", "outfit_default", "accessory_default"} {
		if strings.Contains(sql, retired) {
			t.Fatalf("migration still has %s", retired)
		}
	}
}
