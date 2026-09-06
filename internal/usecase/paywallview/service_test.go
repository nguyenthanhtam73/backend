package paywallview

import (
	"context"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupPaywallDB(t *testing.T) *Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:paywall_view_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.PaywallView{}); err != nil {
		t.Fatal(err)
	}
	return NewService(repository.NewPaywallViewRepository(db))
}

func TestLog_PersistsGuestAndAuthed(t *testing.T) {
	svc := setupPaywallDB(t)
	ctx := context.Background()

	guest, err := svc.Log(ctx, uuid.Nil, dto.LogPaywallViewRequest{
		Surface: domain.PaywallSurfacePricing,
	})
	if err != nil {
		t.Fatal(err)
	}
	if guest.ID == "" || guest.LoggedAt == "" {
		t.Fatalf("guest response incomplete: %#v", guest)
	}

	uid := uuid.New()
	authed, err := svc.Log(ctx, uid, dto.LogPaywallViewRequest{
		Surface:         domain.PaywallSurfaceUpsellBanner,
		Feature:         "wardrobe_full",
		RecommendedPlan: "premium",
	})
	if err != nil {
		t.Fatal(err)
	}
	if authed.ID == "" || authed.ID == guest.ID {
		t.Fatalf("authed id=%q guest id=%q", authed.ID, guest.ID)
	}
}

func TestLog_RejectsInvalidSurface(t *testing.T) {
	svc := setupPaywallDB(t)
	_, err := svc.Log(context.Background(), uuid.Nil, dto.LogPaywallViewRequest{Surface: "meta_pixel"})
	if err == nil || !strings.Contains(err.Error(), "invalid surface") {
		t.Fatalf("err=%v", err)
	}
}

func TestLog_Unavailable(t *testing.T) {
	var svc *Service
	if _, err := svc.Log(context.Background(), uuid.Nil, dto.LogPaywallViewRequest{}); err != ErrUnavailable {
		t.Fatalf("nil service err=%v", err)
	}
	svc = NewService(nil)
	if _, err := svc.Log(context.Background(), uuid.Nil, dto.LogPaywallViewRequest{}); err != ErrUnavailable {
		t.Fatalf("nil repo err=%v", err)
	}
}

func TestValidateAndMap_DefaultsFeature(t *testing.T) {
	row, msg := dto.LogPaywallViewRequest{Surface: "  UPGRADE  "}.ValidateAndMap(uuid.Nil)
	if msg != "" || row == nil {
		t.Fatalf("msg=%q row=%v", msg, row)
	}
	if row.Surface != domain.PaywallSurfaceUpgrade {
		t.Fatalf("surface=%q", row.Surface)
	}
	if row.Feature != "generic" {
		t.Fatalf("feature=%q", row.Feature)
	}
	if row.UserID != nil {
		t.Fatalf("guest should have nil user_id")
	}
}
