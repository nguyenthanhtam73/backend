package handler

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

func TestShopErrorFields(t *testing.T) {
	app := fiber.New()
	app.Get("/err", func(c *fiber.Ctx) error {
		switch c.Query("kind") {
		case "short":
			return writeShopErr(c, &repository.ShopError{
				Err: repositoryErrCoins(), Code: "not_enough_coins", Needed: 500, Balance: 100,
			})
		case "streak":
			return writeShopErr(c, &repository.ShopError{
				Err: errStreak(), Code: "streak_locked", Streak: 12, Need: 30,
			})
		case "sale":
			return writeShopErr(c, &repository.ShopError{
				Err: errNotSold(), Code: "not_for_sale", Reason: "mystery",
			})
		default:
			return writeShopErr(c, repository.ErrIdempotencyConflict)
		}
	})
	short := shopErrBody(t, app, "short")
	if !strings.Contains(short, `"needed":500`) || !strings.Contains(short, `"balance":100`) {
		t.Fatal(short)
	}
	streak := shopErrBody(t, app, "streak")
	if !strings.Contains(streak, `"streak":12`) || !strings.Contains(streak, `"streak_required":30`) {
		t.Fatal(streak)
	}
	sale := shopErrBody(t, app, "sale")
	if !strings.Contains(sale, `"reason":"mystery"`) || !strings.Contains(sale, "Món này không bán") {
		t.Fatal(sale)
	}
}

func shopErrBody(t *testing.T, app *fiber.App, kind string) string {
	t.Helper()
	res, err := app.Test(httptest.NewRequest("GET", "/err?kind="+kind, nil))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	return string(raw)
}

func repositoryErrCoins() error { return repository.ErrNotEnoughCoins }

func errStreak() error { return shopSentinel("that set unlocks after a 30 day check-in streak") }

func errNotSold() error { return shopSentinel("that item is not sold") }

type shopSentinel string

func (e shopSentinel) Error() string { return string(e) }
