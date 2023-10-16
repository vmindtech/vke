package main

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/sirupsen/logrus"

	"github.com/vmindtech/vke/pkg/mysqldb"
	"github.com/vmindtech/vke/pkg/response"
	"github.com/vmindtech/vke/pkg/utils"
	"github.com/vmindtech/vke/pkg/validation"

	di "github.com/vmindtech/vke"
	"github.com/vmindtech/vke/internal/middleware"
	"github.com/vmindtech/vke/internal/route"
)

type application struct {
	Logger         *logrus.Logger
	LanguageBundle *i18n.Bundle
	MysqlInstance  mysqldb.IMysqlInstance
}

func initApplication(a *application) *fiber.App {
	app := fiber.New(fiber.Config{
		// Override default error handler - Internal server err
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}

			errBag := utils.ErrorBag{Code: utils.UnexpectedErrCode, Message: utils.UnexpectedMsg}

			return c.Status(code).JSON(response.NewErrorResponse(c.Context(), errBag))
		},
	})

	// Health check routes
	a.addHealthCheckRoutes(app)

	// Common middleware
	a.addCommonMiddleware(app)

	r := di.InitRoute(a.Logger, a.MysqlInstance)
	r.SetupRoutes(&route.AppContext{
		App: app,
	})

	// 404 handler
	app.Use(func(c *fiber.Ctx) error {
		errBag := utils.ErrorBag{Code: utils.NotFoundErrCode, Message: utils.NotFoundMsg}

		return c.Status(fiber.StatusNotFound).JSON(response.NewErrorResponse(c.Context(), errBag))
	})

	return app
}

func (a *application) addCommonMiddleware(app *fiber.App) {
	app.Use(middleware.RecoverMiddleware(a.Logger))
	app.Use(requestid.New())
	app.Use(middleware.LoggerMiddleware(a.Logger))
	app.Use(middleware.LocalizerMiddleware(a.LanguageBundle))
	app.Use(cors.New())

	// Validator
	validator := validation.InitValidator()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(utils.ValidatorKey, validator)

		return c.Next()
	})

	// Tokenizer
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(utils.TokenizerKey)

		return c.Next()
	})
}

func (a *application) addHealthCheckRoutes(app *fiber.App) {
	healthCheckHandler := di.InitHealthCheckHandler()
	app.Get("/liveness", healthCheckHandler.Liveness)
	app.Get("/readiness", healthCheckHandler.Readiness)
}
