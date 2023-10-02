package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/nicksnyder/go-i18n/v2/i18n"

	"github.com/vmindtech/vke/pkg/utils"
)

func LocalizerMiddleware(b *i18n.Bundle) func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		l := i18n.NewLocalizer(b, utils.GetLanguageWithContext())
		c.Locals(utils.LocalizerKey, l)

		return c.Next()
	}
}
