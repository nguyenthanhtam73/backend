package roomtree

import (
	"context"
	"log/slog"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/lobby"
	"github.com/dadiary/backend/internal/repository"
	"github.com/google/uuid"
)

// LiveRoom delivers tree events on the room socket.
// The hub drops them when nobody is connected and queues them on the next tick.
type LiveRoom interface {
	NotifyTree(code string, ev lobby.Event)
}

// Service is the shared room tree.
type Service struct {
	repo *repository.RoomTreeRepository
	live LiveRoom
	now  func() time.Time
}

// NewService builds the tree service. live may be nil.
func NewService(repo *repository.RoomTreeRepository, live LiveRoom) *Service {
	return &Service{repo: repo, live: live, now: time.Now}
}

// SetNow overrides the clock. Tests use it for the Vietnam day boundary.
func (s *Service) SetNow(now func() time.Time) {
	if s != nil && now != nil {
		s.now = now
	}
}

func (s *Service) clock() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

// Bag is the caller's shards, seeds, and the live trade rules.
func (s *Service) Bag(ctx context.Context, userID uuid.UUID) (dto.SeedBagResponse, error) {
	var zero dto.SeedBagResponse
	if s == nil || s.repo == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	bag, err := s.repo.SeedBag(ctx, userID)
	if err != nil {
		return zero, err
	}
	return toBag(bag), nil
}

// Trade exchanges common shards for one common shard of want.
func (s *Service) Trade(ctx context.Context, userID uuid.UUID, offer []repository.BagStack, want, idem string) (dto.SeedBagResponse, error) {
	var zero dto.SeedBagResponse
	if s == nil || s.repo == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	bag, err := s.repo.TradeShards(ctx, userID, offer, want, idem, s.clock())
	if err != nil {
		return zero, err
	}
	return toBag(bag), nil
}

// Buy spends xu for one common shard. The price is a config row.
func (s *Service) Buy(ctx context.Context, userID uuid.UUID, typeKey, idem string) (dto.SeedBagResponse, error) {
	var zero dto.SeedBagResponse
	if s == nil || s.repo == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	bag, err := s.repo.BuyShard(ctx, userID, typeKey, idem, s.clock())
	if err != nil {
		return zero, err
	}
	return toBag(bag), nil
}

func toBag(bag repository.SeedBag) dto.SeedBagResponse {
	return dto.SeedBagResponse{
		Shards: toStacks(bag.Shards), MysteryShards: bag.MysteryShards, MysteryShardName: domain.MysteryShardName,
		Seeds: toStacks(bag.Seeds), MysterySeeds: bag.MysterySeeds, MysterySeedName: domain.MysterySeedName,
		Rules: dto.SeedBagRules{
			MergeCount: bag.Rules.MergeCount, TradeInCount: bag.Rules.TradeInCount,
			ShardBuyPrice: bag.Rules.ShardBuyPrice, MysteryShardBps: bag.Rules.MysteryShardBps,
		},
		Catalog: toCatalog(bag.Catalog), SlotCount: bag.SlotCount, ComingSoonCount: bag.ComingSoonCount,
		Balance: bag.Balance, Already: bag.Already,
	}
}

func toCatalog(rows []repository.BagCatalogItem) []dto.BagCatalogItem {
	out := make([]dto.BagCatalogItem, len(rows))
	for i, row := range rows {
		out[i] = dto.BagCatalogItem{
			TypeKey: row.TypeKey, NameVI: row.NameVI, Rarity: row.Rarity, ImageKey: row.ImageKey,
			Shards: row.Shards, Seeds: row.Seeds, Available: row.Available, ComingSoon: row.ComingSoon,
			InMysteryPool: row.InMysteryPool, Revealed: row.Revealed,
		}
	}
	if out == nil {
		return []dto.BagCatalogItem{}
	}
	return out
}

func toStacks(rows []repository.BagStack) []dto.BagStack {
	out := make([]dto.BagStack, len(rows))
	for i, row := range rows {
		out[i] = dto.BagStack{TypeKey: row.TypeKey, NameVI: row.NameVI, Count: row.Count}
	}
	if out == nil {
		return []dto.BagStack{}
	}
	return out
}

// Catalog lists enabled tree types for the client to draw.
func (s *Service) Catalog(ctx context.Context, userID uuid.UUID) (dto.TreeCatalogResponse, error) {
	var zero dto.TreeCatalogResponse
	if s == nil || s.repo == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	rows, err := s.repo.ListForUser(ctx, userID)
	if err != nil {
		return zero, err
	}
	out := dto.TreeCatalogResponse{Types: make([]dto.TreeCatalogEntry, len(rows))}
	for i, row := range rows {
		out.Types[i] = dto.TreeCatalogEntry{
			Key: row.Key, NameVI: row.NameVI, Rarity: row.Rarity, ImageKey: row.ImageKey,
			SortOrder: row.SortOrder, ExclusiveItemKey: row.ExclusiveItemKey,
			ExclusiveItemName: row.ExclusiveItemName, Creatable: row.Creatable, Revealed: row.Revealed,
		}
		if _, ok := domain.GiftBoxByTree(row.Key); ok {
			out.Types[i].BloomGift = "box"
		}
	}
	return out, nil
}

// State is the tree for one real member.
func (s *Service) State(ctx context.Context, userID uuid.UUID, code string) (dto.RoomTreeResponse, error) {
	var zero dto.RoomTreeResponse
	if s == nil || s.repo == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	state, err := s.repo.State(ctx, userID, code, s.clock())
	if err != nil {
		return zero, err
	}
	s.broadcast(code, state)
	return toDTO(state), nil
}

// Water adds today's water for one real member.
func (s *Service) Water(ctx context.Context, userID uuid.UUID, code, idempotencyKey string) (dto.RoomTreeResponse, error) {
	var zero dto.RoomTreeResponse
	if s == nil || s.repo == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	state, err := s.repo.Water(ctx, userID, code, idempotencyKey, s.clock())
	if state.Stage != 0 || len(state.Levels) > 0 {
		s.broadcast(code, state)
	}
	if err != nil {
		return zero, err
	}
	return toDTO(state), nil
}

// MarkRewardsSeen marks this member's unseen stage rewards as seen.
// stages empty marks all of them. Other members keep their own unseen rows.
func (s *Service) MarkRewardsSeen(ctx context.Context, userID uuid.UUID, code string, stages []int) (dto.RoomTreeResponse, error) {
	var zero dto.RoomTreeResponse
	if s == nil || s.repo == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	state, err := s.repo.MarkRewardsSeen(ctx, userID, code, stages, s.clock())
	if err != nil {
		return zero, err
	}
	return toDTO(state), nil
}

// Sync applies a membership change. A leave can drop the required count and
// level the tree. A deleted room is ignored.
func (s *Service) Sync(ctx context.Context, code string) error {
	if s == nil || s.repo == nil {
		return nil
	}
	state, err := s.repo.Sync(ctx, code, s.clock())
	if err != nil {
		slog.Warn("room tree sync", "err", err)
		return err
	}
	s.broadcast(code, state)
	return nil
}

func (s *Service) broadcast(code string, state repository.TreeState) {
	if s == nil || s.live == nil || code == "" {
		return
	}
	if state.FreshWater {
		stage := state.Stage
		progress := state.Progress
		if len(state.Levels) > 0 {
			stage = state.Levels[0] - 1
			if stage < 1 {
				stage = 1
			}
			progress = 1
		}
		s.live.NotifyTree(code, lobby.Event{
			Op: lobby.OpTreeWatered,
			Tree: &lobby.TreeEvent{
				Name: state.WatererName, Waters: state.Waters, Required: state.Required,
				Progress: progress, Stage: stage, TreeType: state.TreeType,
			},
		})
	}
	for i, stage := range state.Levels {
		progress := 0.0
		if i == len(state.Levels)-1 {
			progress = state.Progress
		}
		s.live.NotifyTree(code, lobby.Event{
			Op: lobby.OpTreeLevelUp,
			Tree: &lobby.TreeEvent{
				Name: state.WatererName, Waters: state.Waters, Required: state.Required,
				Progress: progress, Stage: stage, TreeType: state.TreeType,
			},
		})
	}
}

func toDTO(state repository.TreeState) dto.RoomTreeResponse {
	return dto.RoomTreeResponse{
		Stage: state.Stage, StageName: state.StageName,
		TreeType: state.TreeType, TreeName: state.TreeName, Rarity: state.Rarity, ImageKey: state.ImageKey,
		Waters: state.Waters, Required: state.Required, Watered: nonNil(state.Watered),
		Progress: state.Progress, CanWater: state.CanWater, Reason: state.Reason, Bloomed: state.Bloomed,
		Rewards: toRewards(state.Rewards), MyRewards: toRewards(state.MyRewards), Balance: state.Balance,
		Already: state.Already, Leveled: len(state.JustGranted) > 0, JustGranted: omitEmpty(state.JustGranted),
		UnseenRewards: toRewards(state.UnseenRewards),
		CanDraw:       state.CanDraw, MyShard: toShard(state.MyShard), Draws: toDraws(state.Draws),
	}
}

// Draw reveals this member's bloom shard. pick is cosmetic.
func (s *Service) Draw(ctx context.Context, userID uuid.UUID, code, idem string, pick *int) (dto.DrawResponse, error) {
	var zero dto.DrawResponse
	if s == nil || s.repo == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	got, err := s.repo.DrawShard(ctx, userID, code, idem, pick, s.clock())
	if err != nil {
		return zero, err
	}
	return dto.DrawResponse{Already: got.Already, Pick: got.Pick, MyShard: dto.TreeShard{
		TypeKey: got.Shard.TypeKey, NameVI: got.Shard.NameVI, Mystery: got.Shard.Mystery,
	}}, nil
}

func toDraws(rows []repository.BloomDraw) []dto.TreeDraw {
	out := make([]dto.TreeDraw, len(rows))
	for i, row := range rows {
		out[i] = dto.TreeDraw{Name: row.Name, Pending: row.Pending, Shard: toShard(row.Shard)}
	}
	if out == nil {
		return []dto.TreeDraw{}
	}
	return out
}

func toShard(shard *repository.ShardView) *dto.TreeShard {
	if shard == nil {
		return nil
	}
	return &dto.TreeShard{TypeKey: shard.TypeKey, NameVI: shard.NameVI, Mystery: shard.Mystery}
}

func toRewards(rows []repository.TreeReward) []dto.TreeReward {
	out := make([]dto.TreeReward, len(rows))
	for i, row := range rows {
		out[i] = dto.TreeReward{
			Stage: row.Stage, Name: row.Name, Coins: row.Coins, ItemID: row.ItemID, ItemName: row.ItemName,
			PreviewOnly: row.PreviewOnly, RewardedCount: row.RewardedCount,
			GiftBox: row.GiftBox, BoxID: row.BoxID,
		}
	}
	return out
}

func omitEmpty(rows []repository.TreeReward) []dto.TreeReward {
	if len(rows) == 0 {
		return nil
	}
	return toRewards(rows)
}

func nonNil(names []string) []string {
	if names == nil {
		return []string{}
	}
	return names
}
