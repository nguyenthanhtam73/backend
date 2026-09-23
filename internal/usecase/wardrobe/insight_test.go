package wardrobe

import (
	"context"
	"errors"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSaveInsight_PersistsAndClearsWhenIdentityChanges(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:wardrobe_insight_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.SkincareProduct{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	user := &domain.User{Email: "insight@dadiary.test", Username: "insight_user", IsActive: true}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("user: %v", err)
	}
	svc := NewService(repository.NewSkincareProductRepository(db), nil, nil)
	ctx := context.Background()
	created, err := svc.Create(ctx, user.ID, dto.CreateWardrobeProductRequest{
		Name:     "Sữa rửa mặt dịu",
		Brand:    "CeraVe",
		Category: "cleanser",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	productID, err := uuid.Parse(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if created.Insight != nil {
		t.Fatal("new product has no card yet")
	}

	saved, err := svc.SaveInsight(ctx, user.ID, productID, dto.WardrobeProductInsight{
		WhatItDoes: "Sữa rửa mặt dịu, lấy dầu thừa.",
		Fit:        dto.WardrobeProductFit{Verdict: dto.WardrobeFitYes, Reason: "Da dầu."},
		Buy:        dto.WardrobeProductBuy{Advice: dto.WardrobeBuyYes, Why: "Tủ chưa có sữa rửa mặt."},
		Actives:    []dto.WardrobeProductActive{{Name: "Ceramide", Gloss: "giữ lớp bảo vệ da"}},
		Disclaimer: "sai",
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.Insight == nil || saved.Insight.WhatItDoes == "" || saved.InsightAt == "" {
		t.Fatalf("saved card: %+v", saved.Insight)
	}
	if saved.Insight.Disclaimer != dto.WardrobeInsightDisclaimer {
		t.Fatalf("disclaimer: %q", saved.Insight.Disclaimer)
	}
	if saved.Insight.Fit.Verdict != dto.WardrobeFitYes || saved.Insight.Buy.Advice != dto.WardrobeBuyYes {
		t.Fatalf("card: %+v", saved.Insight)
	}

	listed, err := svc.List(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Products) != 1 || listed.Products[0].Insight == nil {
		t.Fatalf("list card: %+v", listed.Products)
	}
	if listed.Products[0].Insight.Actives[0].Gloss == "" {
		t.Fatal("expected gloss on list")
	}

	kept, err := svc.Update(ctx, user.ID, productID, dto.UpdateWardrobeProductRequest{
		Name:     "Sữa rửa mặt dịu",
		Brand:    "CeraVe",
		Category: "cleanser",
		OpenedAt: "2026-09-01",
	})
	if err != nil {
		t.Fatalf("opened_at: %v", err)
	}
	if kept.Insight == nil || kept.OpenedAt != "2026-09-01" {
		t.Fatalf("opened date must keep the card: %+v", kept)
	}
	listed, err = svc.List(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if listed.Products[0].Insight == nil || listed.Products[0].Insight.WhatItDoes == "" {
		t.Fatal("opened date must keep the stored card")
	}

	cleared, err := svc.Update(ctx, user.ID, productID, dto.UpdateWardrobeProductRequest{
		Name:     "Sữa rửa mặt khác",
		Brand:    "CeraVe",
		Category: "cleanser",
		OpenedAt: "2026-09-01",
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if cleared.Insight != nil || cleared.InsightAt != "" {
		t.Fatalf("rename must drop the card: %+v %q", cleared.Insight, cleared.InsightAt)
	}
	listed, err = svc.List(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if listed.Products[0].Insight != nil {
		t.Fatal("list still has a stale card")
	}

	_, err = svc.SaveInsight(ctx, user.ID, uuid.New(), dto.WardrobeProductInsight{WhatItDoes: "x"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing product: %v", err)
	}
}
