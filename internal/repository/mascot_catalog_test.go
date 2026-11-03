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
	if !strings.Contains(sql, "user_mascot") || !strings.Contains(sql, "mascot_items") || !strings.Contains(sql, "user_mascot_items") {
		t.Fatal("tables")
	}
	for _, col := range []string{"expires_at", "source_room_id", "acquired_at", "source"} {
		if !strings.Contains(sql, col) {
			t.Fatalf("migration missing %s", col)
		}
	}
}
