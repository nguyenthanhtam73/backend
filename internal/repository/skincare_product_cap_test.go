package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/dadiary/backend/internal/domain"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testWardrobeDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:wardrobe_cap_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&domain.User{}, &domain.SkincareProduct{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestCreateUnderFreeCap_BlocksOverLimit(t *testing.T) {
	db := testWardrobeDB(t)
	repo := NewSkincareProductRepository(db)
	ctx := context.Background()
	user := &domain.User{
		Email:    "shelf@dadiary.test",
		Username: "shelf_user",
		IsActive: true,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("user: %v", err)
	}

	const cap = 3
	for i := 0; i < cap; i++ {
		p := &domain.SkincareProduct{UserID: user.ID, Name: "item"}
		if err := repo.CreateUnderFreeCap(ctx, p, cap); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	over := &domain.SkincareProduct{UserID: user.ID, Name: "overflow"}
	if err := repo.CreateUnderFreeCap(ctx, over, cap); !errors.Is(err, ErrShelfCapExceeded) {
		t.Fatalf("want cap error, got %v", err)
	}
	n, err := repo.CountByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != int64(cap) {
		t.Fatalf("want %d products, got %d", cap, n)
	}
}
