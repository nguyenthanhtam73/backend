package roomtree

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/domain"
)

func TestLeafRateCardListsXuPrizes(t *testing.T) {
	def, ok := domain.GiftBoxByTree(domain.TreeTypeLeaf)
	if !ok {
		t.Fatal("leaf box")
	}
	card, err := toRateCard(def)
	if err != nil {
		t.Fatal(err)
	}
	if len(card.Tiers) != 4 || card.Tiers[0].Key != domain.GiftRarityCoins || card.Tiers[0].WeightBps != 5000 || card.Tiers[0].PerPrizeBps != 0 {
		t.Fatalf("tiers %+v", card.Tiers)
	}
	if card.Tiers[1].PerPrizeBps != 479 || card.Tiers[2].PerPrizeBps != 50 || card.Tiers[3].PerPrizeBps != 5 {
		t.Fatalf("item bands %+v", card.Tiers)
	}
	if card.UltraFallback == nil || card.UltraFallback.Active || card.UltraFallback.WeightBps != 10 || card.UltraFallback.Coins != 188 {
		t.Fatalf("fallback %+v", card.UltraFallback)
	}
	sum := 0
	bands := map[string]int{}
	var coins []string
	for _, prize := range card.Prizes {
		sum += prize.WeightBps
		bands[prize.Rarity] += prize.WeightBps
		if prize.Kind == domain.GiftKindCoins {
			coins = append(coins, prize.Name)
			if prize.Coins == 0 || prize.DupeCoins != 0 || len(prize.Items) != 0 {
				t.Fatalf("xu prize %+v", prize)
			}
		}
	}
	if sum != domain.GiftRollSpan {
		t.Fatalf("sum %d", sum)
	}
	if bands[domain.GiftRarityCoins] != 5000 || bands[domain.GiftRarityCommon] != 4790 || bands[domain.GiftRarityRare] != 200 || bands[domain.GiftRarityUltra] != 10 {
		t.Fatalf("bands %#v", bands)
	}
	if strings.Join(coins, ",") != "8 xu,18 xu,28 xu,88 xu" {
		t.Fatalf("xu names %v", coins)
	}
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	for _, piece := range []string{`"kind":"coins"`, `"coins":8`, `"weight_bps":2000`, `"weight_bps":1500`, `"weight_bps":1000`, `"weight_bps":500`} {
		if !strings.Contains(string(raw), piece) {
			t.Fatalf("rates JSON missing %s", piece)
		}
	}
}
