package wardrobe

import (
	"context"
	"testing"

	"github.com/dadiary/backend/internal/dto"
	"github.com/google/uuid"
)

func TestCreate_RequiresNameAndBrand(t *testing.T) {
	svc := &Service{products: nil}
	uid := uuid.New()

	_, err := svc.Create(context.Background(), uid, dto.CreateWardrobeProductRequest{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}

	_, err = svc.Create(context.Background(), uid, dto.CreateWardrobeProductRequest{Name: "Cleanser", Brand: ""})
	if err == nil {
		t.Fatal("expected error for empty brand")
	}

	_, err = svc.Create(context.Background(), uid, dto.CreateWardrobeProductRequest{Name: "   ", Brand: "CeraVe"})
	if err == nil {
		t.Fatal("expected error for whitespace-only name")
	}
}

func TestParseOpenedAt(t *testing.T) {
	got, err := parseOpenedAt("2026-06-02")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got == nil {
		t.Fatal("expected date")
	}
	if got.Format("2006-01-02") != "2026-06-02" {
		t.Fatalf("got %s", got.Format("2006-01-02"))
	}
	if _, err := parseOpenedAt("not-a-date"); err == nil {
		t.Fatal("expected error for invalid date")
	}
}
