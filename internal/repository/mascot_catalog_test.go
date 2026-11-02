package repository

import (
	"os"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
)

func TestMascotMigrationSeedsDefaultIDs(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/030_user_mascot.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, item := range domain.DefaultMascotItems() {
		if !strings.Contains(sql, "'"+item.ID+"'") {
			t.Fatalf("migration missing %s", item.ID)
		}
		if !strings.Contains(sql, "'"+item.Layer+"'") {
			t.Fatalf("migration missing layer %s", item.Layer)
		}
	}
	if !strings.Contains(sql, "user_mascot") || !strings.Contains(sql, "mascot_items") {
		t.Fatal("tables")
	}
}
