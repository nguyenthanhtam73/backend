package handler

import (
	"errors"
	"time"

	"github.com/dadiary/backend/internal/dto"
	"github.com/dadiary/backend/internal/repository"
	"github.com/dadiary/backend/pkg/response"
	"github.com/gofiber/fiber/v2"
)

type dailyQuestClaimBody struct {
	Session string `json:"session"`
}

// ClaimDailyQuest is POST /api/v1/mascot/daily-quest/claim.
// Body is {"session":"morning"} or {"session":"evening"}.
// The server reads the stored routine for the care day and credits the
// configured amount. A repeat returns the same balance and already_claimed.
func (h *MascotHandler) ClaimDailyQuest(c *fiber.Ctx) error {
	if h == nil || h.quests == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "service_unavailable", "mascot unavailable")
	}
	uid, ok := h.allow(c)
	if !ok {
		return nil
	}
	var body dailyQuestClaimBody
	if len(c.Body()) == 0 {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "body must be valid JSON")
	}
	if err := decodeStrict(c.Body(), &body); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid_body", "body must be valid JSON")
	}
	got, err := h.quests.Claim(c.UserContext(), uid, body.Session, h.questClock())
	if err != nil {
		return writeDailyQuestErr(c, err)
	}
	return response.JSON(c, fiber.StatusOK, dto.DailyQuestClaimResponse{
		Granted:        got.Granted,
		Bonus:          got.Bonus,
		Balance:        got.Balance,
		AlreadyClaimed: got.Already,
		Session:        got.Session,
	})
}

func (h *MascotHandler) questClock() time.Time {
	if h != nil && h.questNow != nil {
		return h.questNow()
	}
	return time.Now()
}

func writeDailyQuestErr(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, repository.ErrDailyQuestSession):
		return response.Error(c, fiber.StatusBadRequest, "invalid_session", "Chọn buổi sáng hoặc buổi tối.")
	case errors.Is(err, repository.ErrDailyQuestSteps):
		return response.Error(c, fiber.StatusConflict, "steps_not_done", err.Error())
	default:
		return response.Error(c, fiber.StatusServiceUnavailable, "database_unavailable", "database is not available")
	}
}
