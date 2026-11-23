package handler

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dadiary/backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

func TestTreeErrorCodes(t *testing.T) {
	app := fiber.New()
	app.Get("/err", func(c *fiber.Ctx) error {
		switch c.Query("kind") {
		case "skin":
			return writeTreeErr(c, repository.ErrTreeNeedsSkinCheck)
		case "watered":
			return writeTreeErr(c, repository.ErrTreeAlreadyWatered)
		case "bloomed":
			return writeTreeErr(c, repository.ErrTreeBloomed)
		case "ai":
			return writeTreeErr(c, repository.ErrTreeNotReal)
		case "type":
			return writeTreeErr(c, repository.ErrTreeTypeUnavailable)
		default:
			return writeTreeErr(c, repository.ErrMascotRoomEnded)
		}
	})
	cases := []struct {
		kind   string
		status int
		code   string
	}{
		{"skin", 409, "needs_skin_check"},
		{"watered", 409, "already_watered"},
		{"bloomed", 409, "bloomed"},
		{"ai", 403, "not_real_member"},
		{"type", 400, "tree_type_unavailable"},
		{"ended", 409, "room_ended"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/err?kind="+tc.kind, nil)
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != tc.status || !strings.Contains(string(raw), `"code":"`+tc.code+`"`) {
			t.Fatalf("%s %d %s", tc.kind, res.StatusCode, raw)
		}
	}
}
