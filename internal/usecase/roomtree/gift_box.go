package roomtree

import (
	"context"
	"strings"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
)

// GiftBoxes lists the caller's unopened boxes.
func (s *Service) GiftBoxes(ctx context.Context, userID uuid.UUID) (dto.GiftBoxListResponse, error) {
	var zero dto.GiftBoxListResponse
	if s == nil || s.repo == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	rows, err := s.repo.ListUnopenedGiftBoxes(ctx, userID)
	if err != nil {
		return zero, err
	}
	return dto.GiftBoxListResponse{Boxes: toBoxSummaries(rows, false)}, nil
}

// GiftRates is the «i» table. treeType empty returns every shipped box.
func (s *Service) GiftRates(_ context.Context, treeType string) (dto.GiftRatesResponse, error) {
	var zero dto.GiftRatesResponse
	if s == nil || s.repo == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	treeType = strings.TrimSpace(treeType)
	var defs []domain.GiftBoxDef
	if treeType == "" {
		defs = domain.GiftBoxes()
	} else {
		def, ok := domain.GiftBoxByTree(treeType)
		if !ok {
			return zero, repository.ErrGiftBoxTree
		}
		defs = []domain.GiftBoxDef{def}
	}
	cards := make([]dto.GiftRateCard, 0, len(defs))
	for _, def := range defs {
		card, err := toRateCard(def)
		if err != nil {
			return zero, err
		}
		cards = append(cards, card)
	}
	return dto.GiftRatesResponse{Boxes: cards}, nil
}

// OpenGiftBox opens one box. Repeating the call returns the first result.
func (s *Service) OpenGiftBox(ctx context.Context, userID uuid.UUID, rawID string) (dto.GiftBoxOpenResponse, error) {
	var zero dto.GiftBoxOpenResponse
	if s == nil || s.repo == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	id, err := uuid.Parse(strings.TrimSpace(rawID))
	if err != nil {
		return zero, repository.ErrGiftBoxID
	}
	got, err := s.repo.OpenGiftBox(ctx, userID, id, s.clock())
	if err != nil {
		return zero, err
	}
	return toGiftOpen(got), nil
}

func toGiftOpen(got repository.GiftOpen) dto.GiftBoxOpenResponse {
	return dto.GiftBoxOpenResponse{
		Already: got.Already, Box: toBoxSummary(got.Box, true),
		Prize: toPrizeView(got.Prize), GrantedItems: toGiftItems(got.Granted),
		Duplicate: got.Duplicate, Partial: got.Partial, DupeCoins: got.DupeCoins,
		Coins: got.Coins, Balance: got.Balance,
	}
}

func toBoxSummaries(rows []domain.TreeGiftBox, opened bool) []dto.GiftBoxSummary {
	out := make([]dto.GiftBoxSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, toBoxSummary(row, opened || row.OpenedAt != nil))
	}
	return out
}

func toBoxSummary(row domain.TreeGiftBox, opened bool) dto.GiftBoxSummary {
	def, _ := domain.GiftBoxByTree(row.TreeType)
	name := def.Name
	treeName := def.TreeName
	art := def.ArtKey
	if name == "" {
		name = row.TreeType
		treeName = row.TreeType
	}
	out := dto.GiftBoxSummary{
		ID: row.ID.String(), TreeType: row.TreeType, ArtKey: art, Name: name, TreeName: treeName,
		RoomCode: row.RoomCode, CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		Opened: opened && row.OpenedAt != nil,
	}
	if row.OpenedAt != nil {
		s := row.OpenedAt.UTC().Format(time.RFC3339)
		out.OpenedAt = &s
		out.Opened = true
	}
	return out
}

func toRateCard(def domain.GiftBoxDef) (dto.GiftRateCard, error) {
	var zero dto.GiftRateCard
	prizes, err := domain.WeightedPrizes(def)
	if err != nil {
		return zero, err
	}
	counts := map[string]int{}
	for _, prize := range prizes {
		counts[prize.Rarity]++
	}
	rates := domain.GiftTierRates()
	tiers := make([]dto.GiftRateTier, len(rates))
	for i, rate := range rates {
		n := counts[rate.Key]
		each := sharedPrizeWeight(prizes, rate.Key)
		tiers[i] = dto.GiftRateTier{
			Key: rate.Key, Name: rate.Name, WeightBps: rate.WeightBps,
			PrizeCount: n, PerPrizeBps: each, DupeCoins: rate.DupeCoins,
		}
	}
	views := make([]dto.GiftPrizeView, len(prizes))
	for i, prize := range prizes {
		views[i] = toPrizeView(prize)
	}
	return dto.GiftRateCard{
		TreeType: def.TreeType, ArtKey: def.ArtKey, Name: def.Name, TreeName: def.TreeName,
		Tiers: tiers, Prizes: views,
	}, nil
}

func toPrizeView(prize domain.GiftPrize) dto.GiftPrizeView {
	return dto.GiftPrizeView{
		ID: prize.ID, Name: prize.Name, Rarity: prize.Rarity, RarityName: rarityName(prize.Rarity),
		Kind: prize.Kind, WeightBps: prize.WeightBps, DupeCoins: prize.DupeCoins,
		Coins: prize.Coins, Items: toGiftItems(prize.Items),
	}
}

func toGiftItems(items []domain.GiftItem) []dto.GiftBoxItem {
	out := make([]dto.GiftBoxItem, 0, len(items))
	for _, item := range items {
		out = append(out, dto.GiftBoxItem{
			ID: item.ID, ArtID: item.ArtID, Name: item.Name, Slot: item.Slot,
			Set: item.Set, Legacy: item.Legacy,
		})
	}
	return out
}

// sharedPrizeWeight is the per-prize weight when every prize in the band
// has the same weight. Xu prizes differ, so that band reports 0 and the
// client reads each prize's weight_bps.
func sharedPrizeWeight(prizes []domain.GiftPrize, rarity string) int {
	weight := 0
	n := 0
	for _, prize := range prizes {
		if prize.Rarity != rarity {
			continue
		}
		n++
		if weight == 0 {
			weight = prize.WeightBps
			continue
		}
		if prize.WeightBps != weight {
			return 0
		}
	}
	if n == 0 {
		return 0
	}
	return weight
}

func rarityName(key string) string {
	for _, rate := range domain.GiftTierRates() {
		if rate.Key == key {
			return rate.Name
		}
	}
	return key
}
