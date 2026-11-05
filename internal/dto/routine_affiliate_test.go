package dto

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/google/uuid"
)

func TestRoutineStep_NullAffiliateFieldsWhenUnset(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(RoutineStep{ID: "1", Title: "Rửa mặt", Category: "cleanser"})
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	for _, key := range []string{`"affiliate_link":null`, `"affiliate_product_id":null`, `"product_name":null`} {
		if !strings.Contains(body, key) {
			t.Fatalf("missing %s in %s", key, body)
		}
	}
}

func TestRoutineStep_EmitsHTTPSAffiliateFields(t *testing.T) {
	t.Parallel()
	link := "https://s.shopee.vn/70HPIj8Km4"
	id := "cerave-foaming-cleanser"
	name := "Sữa rửa mặt tạo bọt"
	b, err := json.Marshal(RoutineStep{
		ID:                 "1",
		Title:              "Sữa rửa mặt tạo bọt",
		AffiliateLink:      &link,
		AffiliateProductID: &id,
		ProductName:        &name,
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	if !strings.Contains(body, `"affiliate_link":"https://s.shopee.vn/70HPIj8Km4"`) {
		t.Fatalf("body %s", body)
	}
	if strings.Contains(body, "http://") {
		t.Fatalf("non-https link in %s", body)
	}
}

func TestRoutineFromDomain_DropsStoredAffiliateFields(t *testing.T) {
	t.Parallel()
	row := &domain.RoutineEntry{
		ID:          uuid.New(),
		UserID:      uuid.New(),
		RoutineDate: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC),
		Morning: json.RawMessage(`[
			{"id":"1","title":"Sữa rửa mặt tạo bọt","category":"cleanser","affiliate_link":"https://evil.example/x","affiliate_product_id":"nope","product_name":"Fake"}
		]`),
		Evening:   json.RawMessage(`[]`),
		UpdatedAt: time.Date(2026, 11, 5, 1, 0, 0, 0, time.UTC),
	}
	res := RoutineFromDomain(row, true)
	if len(res.Morning) != 1 {
		t.Fatalf("steps %#v", res.Morning)
	}
	step := res.Morning[0]
	if step.Title != "Sữa rửa mặt tạo bọt" {
		t.Fatalf("title %q", step.Title)
	}
	if step.AffiliateLink != nil || step.AffiliateProductID != nil || step.ProductName != nil {
		t.Fatalf("stored affiliate fields leaked: %#v", step)
	}
}
