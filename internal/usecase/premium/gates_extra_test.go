package premium

import (
	"context"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestAssertFeature_ExportAndMilestone_FreeDenied(t *testing.T) {
	svc := NewService(stubUserRepo{tier: domain.PlanFree}, nil)
	uid := uuid.New()

	for _, f := range []domain.Feature{
		domain.FeatureExportData,
		domain.FeatureMilestoneFull,
		domain.FeatureAdvancedSkinAnalysis,
		domain.FeatureNoAds,
	} {
		if err := svc.AssertFeature(context.Background(), uid, f); err != ErrFeatureDenied {
			t.Fatalf("%s: want ErrFeatureDenied, got %v", f, err)
		}
		ok, reason, err := svc.CanUseFeature(context.Background(), uid, f)
		if err != nil || ok || reason != ReasonFeatureDenied {
			t.Fatalf("%s: CanUseFeature = (%v,%q,%v)", f, ok, reason, err)
		}
	}
}

func TestAssertFeature_PremiumPlus_AdvancedSkinAndNoAds(t *testing.T) {
	svc := NewService(stubUserRepo{tier: domain.PlanPremiumPlus}, nil)
	uid := uuid.New()

	for _, f := range []domain.Feature{
		domain.FeatureExportData,
		domain.FeatureMilestoneFull,
		domain.FeatureAdvancedSkinAnalysis,
		domain.FeatureNoAds,
	} {
		if err := svc.AssertFeature(context.Background(), uid, f); err != nil {
			t.Fatalf("%s: unexpected %v", f, err)
		}
	}
}

func TestAssertFeature_Premium_NoAdvancedSkin(t *testing.T) {
	svc := NewService(stubUserRepo{tier: domain.PlanPremium}, nil)
	uid := uuid.New()

	if err := svc.AssertFeature(context.Background(), uid, domain.FeatureExportData); err != nil {
		t.Fatalf("export: %v", err)
	}
	if err := svc.AssertFeature(context.Background(), uid, domain.FeatureMilestoneFull); err != nil {
		t.Fatalf("milestone: %v", err)
	}
	if err := svc.AssertFeature(context.Background(), uid, domain.FeatureNoAds); err != nil {
		t.Fatalf("no_ads: %v", err)
	}
	if err := svc.AssertFeature(context.Background(), uid, domain.FeatureAdvancedSkinAnalysis); err != ErrFeatureDenied {
		t.Fatalf("advanced_skin: want ErrFeatureDenied, got %v", err)
	}
}
