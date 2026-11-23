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

// Catalog lists enabled tree types for the client to draw.
func (s *Service) Catalog(ctx context.Context) (dto.TreeCatalogResponse, error) {
	var zero dto.TreeCatalogResponse
	if s == nil || s.repo == nil {
		return zero, domain.Unavailable("service_unavailable", "mascot unavailable")
	}
	rows, err := s.repo.ListEnabled(ctx)
	if err != nil {
		return zero, err
	}
	out := dto.TreeCatalogResponse{Types: make([]dto.TreeCatalogEntry, len(rows))}
	for i, row := range rows {
		out.Types[i] = dto.TreeCatalogEntry{
			Key: row.Key, NameVI: row.NameVI, Rarity: row.Rarity, ImageKey: row.ImageKey,
			SortOrder: row.SortOrder, ExclusiveItemKey: row.ExclusiveItemKey,
			ExclusiveItemName: row.ExclusiveItemName, Creatable: domain.TreeTypeCreatable(row.Key),
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
	}
}

func toRewards(rows []repository.TreeReward) []dto.TreeReward {
	out := make([]dto.TreeReward, len(rows))
	for i, row := range rows {
		out[i] = dto.TreeReward{
			Stage: row.Stage, Name: row.Name, Coins: row.Coins, ItemID: row.ItemID, ItemName: row.ItemName,
			PreviewOnly: row.PreviewOnly,
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
