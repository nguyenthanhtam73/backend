package mascot

import (
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
	if err := slot(loadout.Outfit, domain.MascotLayerOutfit, true, catalog, owned); err != nil {
		return err
	}
	if err := slot(loadout.Accessory, domain.MascotLayerAccessory, false, catalog, owned); err != nil {
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
	if _, owns := owned[id]; owns {
		return nil
	}
	return domain.BadRequest("item_not_owned", "that item is not owned")
}
