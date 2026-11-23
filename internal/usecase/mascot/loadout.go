package mascot

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/dadiary/backend/internal/domain"
)

const (
	maxRoomItems   = 12
	maxAccessories = 8
)

var itemIDPattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// ValidateLoadout checks slots against the catalog and the caller's active grants.
// owned is the set of item ids that are still valid at now (expires_at null or in the future).
// A free catalog row is not enough: the user must hold a user_mascot_items grant.
func ValidateLoadout(loadout domain.MascotLoadout, catalog map[string]domain.MascotItem, owned map[string]struct{}) error {
	if err := slot(loadout.Body, domain.MascotLayerBody, true, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Hair, domain.MascotLayerHair, true, catalog, owned); err != nil {
		return err
	}
	// A top or a bottom replaces the one-piece outfit. The outfit is required
	// only when both split slots are empty, so the mascot is never stored with
	// no clothes at all.
	if loadout.Top != "" || loadout.Bottom != "" {
		if loadout.Outfit != "" {
			return domain.BadRequest("invalid_loadout", "outfit and separate pieces cannot be worn together")
		}
	} else if err := slot(loadout.Outfit, domain.MascotLayerOutfit, true, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Top, domain.MascotLayerTop, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Bottom, domain.MascotLayerBottom, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Accessory, domain.MascotLayerAccessory, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.HairAcc, domain.MascotLayerHairAcc, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Hat, domain.MascotLayerHat, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Earring, domain.MascotLayerEarring, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Glasses, domain.MascotLayerGlasses, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Cheek, domain.MascotLayerCheek, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Bracelet, domain.MascotLayerBracelet, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Bag, domain.MascotLayerBag, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Shoes, domain.MascotLayerShoes, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Held, domain.MascotLayerHeld, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Necklace, domain.MascotLayerNecklace, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Scarf, domain.MascotLayerScarf, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Gloves, domain.MascotLayerGloves, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.NameFrame, domain.MascotLayerNameFrame, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Sticker, domain.MascotLayerSticker, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Bubble, domain.MascotLayerBubble, false, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.FX, domain.MascotLayerFX, false, catalog, owned); err != nil {
		return err
	}
	if len(loadout.Accessories) > maxAccessories {
		return domain.BadRequest("invalid_loadout", "too many accessories")
	}
	seenAcc := map[string]struct{}{}
	for _, id := range loadout.Accessories {
		if _, dup := seenAcc[id]; dup {
			return domain.BadRequest("invalid_loadout", "accessories must be unique")
		}
		seenAcc[id] = struct{}{}
		if err := slot(id, domain.MascotLayerAccessory, true, catalog, owned); err != nil {
			return err
		}
	}
	if len(loadout.RoomItems) > maxRoomItems {
		return domain.BadRequest("invalid_loadout", "too many room items")
	}
	seen := map[string]struct{}{}
	for _, id := range loadout.RoomItems {
		if _, dup := seen[id]; dup {
			return domain.BadRequest("invalid_loadout", "room items must be unique")
		}
		seen[id] = struct{}{}
		if err := slot(id, domain.MascotLayerRoom, true, catalog, owned); err != nil {
			return err
		}
	}
	return nil
}

func slot(id, layer string, required bool, catalog map[string]domain.MascotItem, owned map[string]struct{}) error {
	id = strings.TrimSpace(id)
	if id == "" {
		if required {
			return domain.BadRequest("invalid_loadout", "missing "+layer)
		}
		return nil
	}
	if !itemIDPattern.MatchString(id) {
		return domain.BadRequest("invalid_loadout", "item id is not valid")
	}
	item, ok := catalog[id]
	if !ok {
		return domain.BadRequest("unknown_item", "that item is not in the catalog")
	}
	if item.Layer != layer {
		return domain.BadRequest("wrong_layer", layer+" does not match that item")
	}
	if item.PreviewOnly {
		return domain.BadRequest("preview_only", "that item can be previewed only")
	}
	if _, owns := owned[id]; owns {
		return nil
	}
	return domain.BadRequest("item_not_owned", "that item is not owned")
}

// keptSlotKeys are the slots an older client does not send. A missing key
// keeps the stored piece. An explicit empty string clears it.
var keptSlotKeys = []string{
	"top", "bottom", "hair_acc", "hat", "earring", "glasses", "cheek", "bracelet", "bag",
	"shoes", "held", "necklace", "scarf", "gloves", "name_frame", "sticker", "bubble", "fx",
}

// mergeKeptSlots copies stored values for new slots the request left out.
func mergeKeptSlots(prev, incoming domain.MascotLoadout, raw []byte) domain.MascotLoadout {
	present := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &present)
	keep := map[string]string{
		"top":        prev.Top,
		"bottom":     prev.Bottom,
		"hair_acc":   prev.HairAcc,
		"hat":        prev.Hat,
		"earring":    prev.Earring,
		"glasses":    prev.Glasses,
		"cheek":      prev.Cheek,
		"bracelet":   prev.Bracelet,
		"bag":        prev.Bag,
		"shoes":      prev.Shoes,
		"held":       prev.Held,
		"necklace":   prev.Necklace,
		"scarf":      prev.Scarf,
		"gloves":     prev.Gloves,
		"name_frame": prev.NameFrame,
		"sticker":    prev.Sticker,
		"bubble":     prev.Bubble,
		"fx":         prev.FX,
	}
	for _, key := range keptSlotKeys {
		if _, ok := present[key]; ok {
			continue
		}
		switch key {
		case "top":
			incoming.Top = keep[key]
		case "bottom":
			incoming.Bottom = keep[key]
		case "hair_acc":
			incoming.HairAcc = keep[key]
		case "hat":
			incoming.Hat = keep[key]
		case "earring":
			incoming.Earring = keep[key]
		case "glasses":
			incoming.Glasses = keep[key]
		case "cheek":
			incoming.Cheek = keep[key]
		case "bracelet":
			incoming.Bracelet = keep[key]
		case "bag":
			incoming.Bag = keep[key]
		case "shoes":
			incoming.Shoes = keep[key]
		case "held":
			incoming.Held = keep[key]
		case "necklace":
			incoming.Necklace = keep[key]
		case "scarf":
			incoming.Scarf = keep[key]
		case "gloves":
			incoming.Gloves = keep[key]
		case "name_frame":
			incoming.NameFrame = keep[key]
		case "sticker":
			incoming.Sticker = keep[key]
		case "bubble":
			incoming.Bubble = keep[key]
		case "fx":
			incoming.FX = keep[key]
		}
	}
	return incoming
}
