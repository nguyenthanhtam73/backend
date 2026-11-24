package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dadiary/backend/internal/config"
	"github.com/dadiary/backend/internal/domain"
	"github.com/dadiary/backend/internal/middleware"
	"github.com/dadiary/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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

func TestShopBuyIgnoresClientPrice(t *testing.T) {
	db := openShopHTTP(t)
	user := &domain.User{
		Email: "shop-price-" + uuid.NewString() + "@example.com", Username: "p-" + uuid.NewString(),
		DisplayName: "price", IsActive: true,
	}
	if err := repository.NewUserRepository(db).Create(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domain.UserCoinWallet{
		UserID: user.ID, Balance: 20000, EarnedBalance: 20000, UpdatedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	h := NewMascotHandler(nil, &config.Config{AdminEmails: []string{user.Email}})
	h.AttachShop(repository.NewXuShopRepository(db))
	app := fiber.New()
	app.Post("/api/v1/mascot/shop/buy", func(c *fiber.Ctx) error {
		c.Locals(middleware.LocalsUserID, user.ID)
		c.Locals(middleware.LocalsUserEmail, user.Email)
		return c.Next()
	}, h.ShopBuy)
	body := `{"idempotency_key":"tap","item_id":"spa_01","price":1,"charge":1}`
	req := httptest.NewRequest("POST", "/api/v1/mascot/shop/buy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d %s", res.StatusCode, raw)
	}
	var env struct {
		Data struct {
			Charged int   `json:"charged"`
			Balance int64 `json:"balance"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Charged != 9000 || env.Data.Balance != 11000 {
		t.Fatalf("client price was used: %s", raw)
	}
}

func openShopHTTP(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:shop_http_"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.MascotItem{},
		&domain.UserMascotItem{},
		&domain.UserCoinWallet{},
		&domain.UserCoinLedger{},
		&domain.UserShopOrder{},
		&domain.UserShopUnlock{},
		&domain.Streak{},
	); err != nil {
		t.Fatal(err)
	}
	if err := repository.SeedShopCatalog(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func repositoryErrCoins() error { return repository.ErrNotEnoughCoins }

func errStreak() error { return shopSentinel("that set unlocks after a 30 day check-in streak") }

func errNotSold() error { return shopSentinel("that item is not sold") }

type shopSentinel string

func (e shopSentinel) Error() string { return string(e) }
