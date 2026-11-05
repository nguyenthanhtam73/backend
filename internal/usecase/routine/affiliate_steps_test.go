package routine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/service/ai"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMarshalRoutineSteps_OmitsAffiliateFields(t *testing.T) {
	t.Parallel()
	link := "https://s.shopee.vn/70HPIj8Km4"
	id := "cerave-foaming-cleanser"
	name := "Sữa rửa mặt tạo bọt"
	raw, err := marshalRoutineSteps([]dto.RoutineStep{
		{
			ID:                 "1",
			Title:              "Sữa rửa mặt tạo bọt",
			Category:           "cleanser",
			Completed:          true,
			AffiliateLink:      &link,
			AffiliateProductID: &id,
			ProductName:        &name,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, forbidden := range []string{"affiliate_link", "affiliate_product_id", "product_name", "shopee", "evil.example"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("stored JSON contains %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"title":"Sữa rửa mặt tạo bọt"`) || !strings.Contains(body, `"completed":true`) {
		t.Fatalf("stored step lost identity: %s", body)
	}
}

func TestUpsertAndGetCurrent_AffiliateFromCatalogOnly(t *testing.T) {
	db := openRoutineAffiliateDB(t)
	repo := repository.NewRoutineEntryRepository(db)
	svc := NewService(nil, repo, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()
	uid := uuid.New()
	if err := db.Create(&domain.User{
		ID:       uid,
		Email:    uid.String() + "@example.com",
		Username: "aff" + uid.String()[:8],
		Provider: domain.AuthProviderLocal,
	}).Error; err != nil {
		t.Fatalf("user: %v", err)
	}

	evil := "https://evil.example/phish"
	fakeID := "fake-id"
	fakeName := "Invented Product"
	saved, err := svc.Upsert(ctx, uid, dto.PutRoutineRequest{
		Morning: []dto.RoutineStep{
			{
				ID:                 "am-1",
				Title:              "Sữa rửa mặt tạo bọt",
				Category:           "cleanser",
				AffiliateLink:      &evil,
				AffiliateProductID: &fakeID,
				ProductName:        &fakeName,
			},
			{ID: "am-2", Title: "Kem chống nắng", Category: "spf"},
		},
		Evening: []dto.RoutineStep{
			{ID: "pm-1", Title: "Biore", Category: "spf"},
			{ID: "pm-2", Title: "Kem dưỡng ẩm", Category: "moisturizer"},
			{ID: "pm-3", Title: "Mặt nạ", Category: "mask"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	assertAffiliate(t, saved.Morning[0], "cerave-foaming-cleanser", "Sữa rửa mặt tạo bọt", "https://s.shopee.vn/70HPIj8Km4")
	assertGuidanceAffiliate(t, saved.Morning[1], "", "spf", "Kem chống nắng")
	assertAffiliate(t, saved.Evening[0], "biore-uv-aqua-rich", "Tinh chất chống nắng Biore UV Aqua Rich SPF50+", "https://s.shopee.vn/1BJcMMK4au")
	assertGuidanceAffiliate(t, saved.Evening[1], "", "moisturizer", "Kem dưỡng ẩm")
	assertNoAffiliate(t, saved.Evening[2])

	var row domain.RoutineEntry
	if err := db.Where("user_id = ?", uid).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	stored := string(row.Morning) + string(row.Evening)
	for _, forbidden := range []string{"affiliate_link", "affiliate_product_id", "product_name", "evil.example", "shopee"} {
		if strings.Contains(stored, forbidden) {
			t.Fatalf("persisted routine contains %q: %s", forbidden, stored)
		}
	}

	got, err := svc.GetCurrent(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Saved {
		t.Fatal("expected today's row")
	}
	assertAffiliate(t, got.Morning[0], "cerave-foaming-cleanser", "Sữa rửa mặt tạo bọt", "https://s.shopee.vn/70HPIj8Km4")
	assertGuidanceAffiliate(t, got.Morning[1], "", "spf", "Kem chống nắng")
	assertAffiliate(t, got.Evening[0], "biore-uv-aqua-rich", "Tinh chất chống nắng Biore UV Aqua Rich SPF50+", "https://s.shopee.vn/1BJcMMK4au")
	assertNoAffiliate(t, got.Evening[2])

	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"affiliate_link":null`) {
		t.Fatalf("unmatched steps should serialize null: %s", body)
	}
	if strings.Contains(string(body), "evil.example") || strings.Contains(string(body), "http://") {
		t.Fatalf("response leaked a non-catalog link: %s", body)
	}

	hist, err := svc.History(ctx, uid, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist.Entries) != 1 {
		t.Fatalf("history entries %d", len(hist.Entries))
	}
	assertAffiliate(t, hist.Entries[0].Morning[0], "cerave-foaming-cleanser", "Sữa rửa mặt tạo bọt", "https://s.shopee.vn/70HPIj8Km4")
	assertGuidanceAffiliate(t, hist.Entries[0].Morning[1], "", "spf", "Kem chống nắng")
	assertNoAffiliate(t, hist.Entries[0].Evening[2])
}

func TestGetCurrent_CarriedRoutineStillMapsCatalog(t *testing.T) {
	db := openRoutineAffiliateDB(t)
	repo := repository.NewRoutineEntryRepository(db)
	svc := NewService(nil, repo, nil, nil, nil, nil, nil, nil)
	ctx := context.Background()
	uid := uuid.New()
	if err := db.Create(&domain.User{
		ID:       uid,
		Email:    uid.String() + "@example.com",
		Username: "car" + uid.String()[:8],
		Provider: domain.AuthProviderLocal,
	}).Error; err != nil {
		t.Fatalf("user: %v", err)
	}
	yesterday := routineToday().AddDate(0, 0, -1)
	if _, err := repo.UpsertForDay(ctx, &domain.RoutineEntry{
		UserID:      uid,
		RoutineDate: yesterday,
		Morning:     json.RawMessage(`[{"id":"1","title":"Cicaplast","category":"moisturizer"}]`),
		Evening:     json.RawMessage(`[{"id":"2","title":"Rửa mặt dịu","category":"cleanser"}]`),
		Source:      "manual",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetCurrent(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Saved {
		t.Fatal("carry-forward must not look saved")
	}
	if got.CarriedFromDate != yesterday.UTC().Format("2006-01-02") && got.CarriedFromDate != yesterday.Format("2006-01-02") {
		t.Fatalf("carried_from_date %q yesterday %s", got.CarriedFromDate, yesterday.Format("2006-01-02"))
	}
	assertAffiliate(t, got.Morning[0], "lrp-cicaplast-b5", "Kem phục hồi Cicaplast Baume B5+", "https://s.shopee.vn/7VDg438uB9")
	assertGuidanceAffiliate(t, got.Evening[0], "", "cleanser", "Rửa mặt dịu")
}

func TestGetCurrent_GenericStepUsesGuidanceForSkinType(t *testing.T) {
	db := openRoutineAffiliateDB(t)
	if err := db.AutoMigrate(&domain.SkinProfile{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewRoutineEntryRepository(db)
	profiles := repository.NewSkinProfileRepository(db)
	svc := NewService(nil, repo, profiles, nil, nil, nil, nil, nil)
	ctx := context.Background()

	oily := seedRoutineUser(t, db, "oily")
	dry := seedRoutineUser(t, db, "dry")
	for _, uid := range []uuid.UUID{oily, dry} {
		if _, err := svc.Upsert(ctx, uid, dto.PutRoutineRequest{
			Morning: []dto.RoutineStep{{ID: "1", Title: "Rửa mặt dịu", Category: "cleanser"}},
			Evening: []dto.RoutineStep{{ID: "2", Title: "Uống nước", Category: "other"}},
		}); err != nil {
			t.Fatal(err)
		}
	}

	oilyRes, err := svc.GetCurrent(ctx, oily)
	if err != nil {
		t.Fatal(err)
	}
	dryRes, err := svc.GetCurrent(ctx, dry)
	if err != nil {
		t.Fatal(err)
	}
	assertGuidanceAffiliate(t, oilyRes.Morning[0], "oily", "cleanser", "Rửa mặt dịu")
	assertGuidanceAffiliate(t, dryRes.Morning[0], "dry", "cleanser", "Rửa mặt dịu")
	if oilyRes.Morning[0].AffiliateProductID == nil || dryRes.Morning[0].AffiliateProductID == nil ||
		*oilyRes.Morning[0].AffiliateProductID == *dryRes.Morning[0].AffiliateProductID {
		t.Fatalf("skin types should pick different cleansers: oily %#v dry %#v",
			oilyRes.Morning[0].AffiliateProductID, dryRes.Morning[0].AffiliateProductID)
	}
	assertNoAffiliate(t, oilyRes.Evening[0])
	assertNoAffiliate(t, dryRes.Evening[0])

	var stored domain.RoutineEntry
	if err := db.Where("user_id = ?", oily).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored.Morning), "affiliate") || strings.Contains(string(stored.Morning), "https://") {
		t.Fatalf("guidance link was persisted: %s", stored.Morning)
	}
}

func seedRoutineUser(t *testing.T, db *gorm.DB, skin string) uuid.UUID {
	t.Helper()
	uid := uuid.New()
	if err := db.Create(&domain.User{
		ID:       uid,
		Email:    uid.String() + "@example.com",
		Username: skin + uid.String()[:8],
		Provider: domain.AuthProviderLocal,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if skin != "" {
		if err := db.Create(&domain.SkinProfile{UserID: uid, SkinType: skin}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return uid
}

func assertGuidanceAffiliate(t *testing.T, step dto.RoutineStep, skin, category, title string) {
	t.Helper()
	id, name, link, ok := ai.GuidanceAffiliateForRoutineStep(skin, category, title)
	if !ok {
		t.Fatalf("guidance returned no product for skin %q category %q", skin, category)
	}
	assertAffiliate(t, step, id, name, link)
}

func assertAffiliate(t *testing.T, step dto.RoutineStep, id, name, link string) {
	t.Helper()
	if step.AffiliateProductID == nil || *step.AffiliateProductID != id {
		t.Fatalf("id got %#v want %s", step.AffiliateProductID, id)
	}
	if step.ProductName == nil || *step.ProductName != name {
		t.Fatalf("name got %#v want %s", step.ProductName, name)
	}
	if step.AffiliateLink == nil || *step.AffiliateLink != link {
		t.Fatalf("link got %#v want %s", step.AffiliateLink, link)
	}
	if !strings.HasPrefix(*step.AffiliateLink, "https://") {
		t.Fatalf("link %s", *step.AffiliateLink)
	}
}

func assertNoAffiliate(t *testing.T, step dto.RoutineStep) {
	t.Helper()
	if step.AffiliateLink != nil || step.AffiliateProductID != nil || step.ProductName != nil {
		t.Fatalf("expected null affiliate fields, got %#v", step)
	}
}

func openRoutineAffiliateDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:routine_aff_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.RoutineEntry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_routine_entries_user_date_live
		ON routine_entries (user_id, routine_date)
		WHERE deleted_at IS NULL
	`).Error; err != nil {
		t.Fatalf("unique index: %v", err)
	}
	return db
}
