package roomquest

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/internal/roomday"
	pushsvc "github.com/dadiary/backend/internal/service/push"
	"github.com/dadiary/backend/internal/streaktime"
	pushuc "github.com/dadiary/backend/internal/usecase/push"
	"github.com/google/uuid"
)

// LiveRoom delivers an in-app nudge on the room socket.
// The hub drops it when nobody is connected.
type LiveRoom interface {
	NotifyNudge(code string, fromUser, toUser uuid.UUID, message string)
}

// Sender is the existing Web Push path. A missing subscription is not a failure.
type Sender interface {
	SendToUser(ctx context.Context, userID uuid.UUID, payload pushsvc.NotificationPayload) error
}

// Service is the shared-room quest, today board, and nudge.
type Service struct {
	repo *repository.RoomQuestRepository
	live LiveRoom
	push Sender
	now  func() time.Time
}

// NewService builds the shared-room service. live and push may be nil.
func NewService(repo *repository.RoomQuestRepository, live LiveRoom) *Service {
	return &Service{repo: repo, live: live, now: time.Now}
}

// SetPush attaches the Web Push sender. Safe to call before traffic.
func (s *Service) SetPush(push Sender) {
	if s != nil {
		s.push = push
	}
}

// SetNow overrides the clock. Tests use it for the Vietnam date boundary.
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

// State is the progress bar and which chests this member can open.
func (s *Service) State(ctx context.Context, userID uuid.UUID, code string) (dto.RoomQuestResponse, error) {
	var zero dto.RoomQuestResponse
	loaded, err := s.repo.Load(ctx, userID, code, s.clock(), true)
	if err != nil {
		return zero, err
	}
	return questView(loaded, s.clock()), nil
}

// Today is the Hôm nay board: display name, mascot look, and today's two flags.
func (s *Service) Today(ctx context.Context, userID uuid.UUID, code string) (dto.RoomTodayResponse, error) {
	var zero dto.RoomTodayResponse
	now := s.clock()
	loaded, err := s.repo.Load(ctx, userID, code, now, true)
	if err != nil {
		return zero, err
	}
	members := make([]dto.RoomTodayMember, len(loaded.Members))
	for i, member := range loaded.Members {
		cared, checked := roomday.TodayFlags(now, loaded.Cared[member.UserID], loaded.Checked[member.UserID])
		members[i] = dto.RoomTodayMember{
			UserID:       member.UserID.String(),
			DisplayName:  boardName(member.DisplayName, member.Nickname),
			Look:         lookJSON(member.Look, member.Gender),
			CaredToday:   cared,
			CheckedToday: checked,
		}
	}
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].DisplayName != members[j].DisplayName {
			return members[i].DisplayName < members[j].DisplayName
		}
		return members[i].UserID < members[j].UserID
	})
	return dto.RoomTodayResponse{
		VNDate:  streaktime.DateOf(now).Format("2006-01-02"),
		Members: members,
	}, nil
}

// Claim opens one chest. A repeat returns the same coins and does not pay again.
func (s *Service) Claim(ctx context.Context, userID uuid.UUID, code string, memberDays int) (dto.RoomQuestClaimResponse, error) {
	var zero dto.RoomQuestClaimResponse
	got, err := s.repo.Claim(ctx, userID, code, memberDays, s.clock())
	if err != nil {
		return zero, err
	}
	return dto.RoomQuestClaimResponse{
		MemberDays:     got.MemberDays,
		Coins:          got.Coins,
		Tier:           got.Tier,
		Balance:        got.Balance,
		AlreadyClaimed: got.Already,
		Progress:       got.Progress,
	}, nil
}

// Nudge sends one fixed sentence to a roommate who has not ticked today.
func (s *Service) Nudge(ctx context.Context, userID uuid.UUID, code string, target uuid.UUID) (dto.RoomNudgeResponse, error) {
	var zero dto.RoomNudgeResponse
	now := s.clock()
	roomCode, vnDate, message, err := s.repo.SaveNudge(ctx, userID, code, target, now, func(display, nick string) string {
		return NudgeMessage(display, nick, userID, target, streaktime.DateOf(now).Format("2006-01-02"))
	})
	if err != nil {
		return zero, err
	}
	if vnDate == "" {
		vnDate = streaktime.DateOf(now).Format("2006-01-02")
	}
	pushStatus := s.deliverNudge(ctx, roomCode, userID, target, message)
	return dto.RoomNudgeResponse{
		UserID:  target.String(),
		Message: message,
		VNDate:  vnDate,
		Push:    pushStatus,
	}, nil
}

func (s *Service) deliverNudge(ctx context.Context, code string, from, to uuid.UUID, message string) string {
	status := "skipped"
	if s != nil && s.push != nil {
		payload := pushsvc.NotificationPayload{
			Title:     "Bé Da nhắc bạn",
			Body:      message,
			Icon:      pushsvc.DefaultNotificationIcon,
			Badge:     pushsvc.DefaultNotificationBadge,
			Tag:       "dadiary-room-nudge-" + code,
			Renotify:  true,
			Timestamp: time.Now().UnixMilli(),
			Data: map[string]any{
				"type":      "room_nudge",
				"url":       "/be-da/phong/" + code,
				"room_code": code,
			},
		}
		err := s.push.SendToUser(ctx, to, payload)
		switch {
		case err == nil:
			status = "sent"
		case errors.Is(err, pushuc.ErrSenderUnavailable),
			errors.Is(err, pushuc.ErrNotFound),
			errors.Is(err, pushsvc.ErrNotConfigured),
			errors.Is(err, pushsvc.ErrNoSubscription):
			status = "skipped"
		default:
			slog.Warn("room nudge push", "error", err)
			status = "skipped"
		}
	}
	if s != nil && s.live != nil {
		s.live.NotifyNudge(code, from, to, message)
	}
	return status
}

func questView(loaded repository.SharedRoom, now time.Time) dto.RoomQuestResponse {
	progress := roomday.CountMemberDays(loaded.CreatedAt, loaded.EndsAt, memberSpans(loaded.Members), loaded.Cared, loaded.Checked)
	span := roomday.SpanDays(loaded.CreatedAt, loaded.EndsAt)
	claimed := map[int]struct{}{}
	for _, days := range loaded.Claimed {
		claimed[days] = struct{}{}
	}
	milestones := make([]dto.RoomQuestMilestoneJSON, len(domain.RoomQuestMilestones))
	for i, chest := range domain.RoomQuestMilestones {
		_, did := claimed[chest.MemberDays]
		reached := progress >= chest.MemberDays
		milestones[i] = dto.RoomQuestMilestoneJSON{
			MemberDays: chest.MemberDays,
			Coins:      chest.Coins,
			Tier:       chest.Tier,
			Reached:    reached,
			Claimed:    did,
			Claimable:  reached && !did,
		}
	}
	return dto.RoomQuestResponse{
		Progress:      progress,
		SpanDays:      span,
		MemberCount:   len(loaded.Members),
		MaxMemberDays: span * len(loaded.Members),
		VNToday:       streaktime.DateOf(now).Format("2006-01-02"),
		Milestones:    milestones,
	}
}

func memberSpans(members []repository.SharedMember) []roomday.MemberSpan {
	out := make([]roomday.MemberSpan, len(members))
	for i, member := range members {
		out[i] = roomday.MemberSpan{UserID: member.UserID, JoinedAt: member.JoinedAt}
	}
	return out
}

func boardName(displayName, nickname string) string {
	name := cleanName(displayName)
	if name == "" {
		name = cleanName(nickname)
	}
	return name
}

func lookJSON(look domain.MascotLoadout, gender string) dto.RoomMascotLook {
	accessories := look.Accessories
	if accessories == nil {
		accessories = []string{}
	}
	items := look.RoomItems
	if items == nil {
		items = []string{}
	}
	return dto.RoomMascotLook{
		Body:        look.Body,
		Hair:        look.Hair,
		Outfit:      look.Outfit,
		Accessory:   look.Accessory,
		Accessories: accessories,
		RoomItems:   items,
		Gender:      gender,
	}
}
