package usage

import (
	"testing"

	"github.com/dadiary/backend/internal/dto"
)

func TestWardrobeFeatureDTO_FreeSlots(t *testing.T) {
	mid := wardrobeFeatureDTO(dto.WardrobeUsage{
		CanWrite:  true,
		CanManage: false,
		Used:      2,
		Limit:     FreeWardrobeProductLimit,
		Remaining: 1,
	})
	if !mid.Allowed || mid.Unlimited || mid.Remaining != 1 || mid.Limit != FreeWardrobeProductLimit {
		t.Fatalf("free mid: %+v", mid)
	}
	if mid.Kind != "shelf_slots" {
		t.Fatalf("kind: %s", mid.Kind)
	}

	full := wardrobeFeatureDTO(dto.WardrobeUsage{
		CanWrite:  false,
		CanManage: false,
		Used:      FreeWardrobeProductLimit,
		Limit:     FreeWardrobeProductLimit,
		Remaining: 0,
	})
	if full.Allowed || full.Remaining != 0 {
		t.Fatalf("free full: %+v", full)
	}

	paid := wardrobeFeatureDTO(dto.WardrobeUsage{
		CanWrite:  true,
		CanManage: true,
		Used:      10,
		Unlimited: true,
	})
	if !paid.Allowed || !paid.Unlimited {
		t.Fatalf("paid: %+v", paid)
	}
}

func TestFreeWardrobeProductLimit(t *testing.T) {
	if FreeWardrobeProductLimit != 3 {
		t.Fatalf("expected FreeWardrobeProductLimit=3, got %d", FreeWardrobeProductLimit)
	}
}
