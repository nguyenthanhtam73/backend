package middleware

import (
	"io"
	"os"

	"github.com/dadiary/backend/internal/logmask"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
)

// RegisterDefault wires safe defaults for a JSON API: panic recovery, request IDs, logging, CORS.
//
// The access line is status, latency, method, and path, plus a redacted ${error}.
// It does not record the body, query string, cookies, Authorization, or client IP.
// Those logger tags are overridden anyway, so a later format edit cannot print them
// in full. Rate-limit keys still use c.IP() (Railway's X-Real-IP when that ships);
// that value is not written here. Panic recovery does not print a stack (Fiber's
// default). A panic string that reaches ${error} is redacted with everything else.
func RegisterDefault(app *fiber.App) {
	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(logger.New(accessLoggerConfig(os.Stdout)))
	app.Use(cors.New(cors.Config{
		// Use "*" so local dev works without per-origin config. Do not pair this with AllowCredentials: true
		// (browsers forbid * + credentials). Frontend must use Bearer in headers, not cookie sessions, unless you set explicit AllowOrigins.
		AllowOrigins:     "*",
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization, X-Preview-Token",
		AllowCredentials: false,
	}))
}

func accessLoggerConfig(w io.Writer) logger.Config {
	return logger.Config{
		Format:     "[${time}] ${status} - ${latency} ${method} ${path} ${error}\n",
		Output:     w,
		CustomTags: privacyLogTags(),
	}
}

func privacyLogTags() map[string]logger.LogFunc {
	drop := func(output logger.Buffer, c *fiber.Ctx, data *logger.Data, extra string) (int, error) {
		return output.WriteString("***")
	}
	return map[string]logger.LogFunc{
		logger.TagError: func(output logger.Buffer, c *fiber.Ctx, data *logger.Data, extra string) (int, error) {
			if data.ChainErr == nil {
				return output.WriteString("-")
			}
			return output.WriteString(logmask.Redact(data.ChainErr.Error()))
		},
		logger.TagBody:              drop,
		logger.TagResBody:           drop,
		logger.TagReqHeaders:        drop,
		logger.TagQueryStringParams: drop,
		logger.TagCookie:            drop,
		logger.TagForm:              drop,
		logger.TagHeader:            drop,
		logger.TagReqHeader:         drop,
		logger.TagRespHeader:        drop,
		logger.TagQuery:             drop,
		logger.TagURL: func(output logger.Buffer, c *fiber.Ctx, data *logger.Data, extra string) (int, error) {
			// Path only — OriginalURL includes the query string.
			return output.WriteString(logmask.Redact(c.Path()))
		},
		logger.TagIP: func(output logger.Buffer, c *fiber.Ctx, data *logger.Data, extra string) (int, error) {
			return output.WriteString(logmask.MaskIP(c.IP()))
		},
		logger.TagIPs: func(output logger.Buffer, c *fiber.Ctx, data *logger.Data, extra string) (int, error) {
			return output.WriteString(logmask.MaskIPList(c.Get(fiber.HeaderXForwardedFor)))
		},
	}
}
