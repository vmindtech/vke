package middleware

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"

	"github.com/vmindtech/vke/pkg/utils"
)

func LoggerMiddleware(l *logrus.Logger) func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) (err error) {
		t := time.Now()
		err = c.Next()

		l.WithFields(logrus.Fields{
			"request":  getRequestLogFields(c),
			"response": getResponseLogFields(c.Response().StatusCode(), t),
		}).Info("weblogger")

		return err
	}
}

func getRequestLogFields(c *fiber.Ctx) logrus.Fields {
	return logrus.Fields{
		"id":     c.Locals(utils.RequestIDKey),
		"method": c.Method(),
		"path":   c.Path(),
	}
}

func getResponseLogFields(status int, t time.Time) logrus.Fields {
	return logrus.Fields{
		"status":   status,
		"duration": fmt.Sprint(time.Since(t).Round(time.Millisecond)),
		"body":     fiber.Map{},
	}
}
