package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestReplaceInsightCopy_LeavesOtherColumns(t *testing.T) {
	db := testWardrobeDB(t)
	repo := NewSkincareProductRepository(db)
	ctx := context.Background()
	user := &domain.User{Email: "insight-copy@dadiary.test", Username: "insight_copy", IsActive: true}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("user: %v", err)
	}
	opened := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	p := &domain.SkincareProduct{
		UserID:   user.ID,
		Name:     "CeraVe Foaming Cleanser",
		Brand:    "CeraVe",
		Category: "cleanser",
		Notes:    "dùng tối",
		OpenedAt: &opened,
		Insight:  []byte(`{"buy":{"advice":"nên mua","why":"Nên mua vì hợp."}}`),
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Model(&domain.SkincareProduct{}).Where("id = ?", p.ID).
		UpdateColumns(map[string]any{"updated_at": updated}).Error; err != nil {
		t.Fatalf("stamp updated_at: %v", err)
	}

	insightAt := time.Date(2026, 9, 25, 8, 0, 0, 0, time.UTC)
	next := []byte(`{"buy":{"advice":"nên mua","why":"Nên dùng tiếp vì hợp."}}`)
	ok, err := repo.ReplaceInsightCopy(ctx, user.ID, p.ID, next, insightAt)
	if err != nil || !ok {
		t.Fatalf("replace: ok=%v err=%v", ok, err)
	}

	var got domain.SkincareProduct
	if err := db.First(&got, "id = ?", p.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Name != "CeraVe Foaming Cleanser" || got.Brand != "CeraVe" || got.Category != "cleanser" || got.Notes != "dùng tối" {
		t.Fatalf("identity changed: %+v", got)
	}
	if got.OpenedAt == nil || !got.OpenedAt.Equal(opened) {
		t.Fatalf("opened_at: %v", got.OpenedAt)
	}
	if !got.UpdatedAt.Equal(updated) {
		t.Fatalf("updated_at changed: got %s want %s", got.UpdatedAt, updated)
	}
	if got.InsightAt == nil || !got.InsightAt.UTC().Equal(insightAt) {
		t.Fatalf("insight_at: %v", got.InsightAt)
	}
	if !strings.Contains(string(got.Insight), "Nên dùng tiếp vì hợp.") {
		t.Fatalf("insight: %s", got.Insight)
	}

	ok, err = repo.ReplaceInsightCopy(ctx, uuid.New(), p.ID, next, insightAt)
	if err != nil || ok {
		t.Fatalf("other user must not match: ok=%v err=%v", ok, err)
	}
}
