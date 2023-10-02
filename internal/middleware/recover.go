package middleware

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"

	"github.com/vmindtech/vke/pkg/stacktrace"
)

const (
	skipStackTraceFrame = 4
)

func RecoverMiddleware(l *logrus.Logger) func(c *fiber.Ctx) (err error) {
	return func(c *fiber.Ctx) (err error) {
		t := time.Now()

		defer func() {
			if r := recover(); r != nil {
				var ok bool
				if err, ok = r.(error); !ok {
					err = fmt.Errorf("%v", r)
				}

				l.WithFields(logrus.Fields{
					"request":  getRequestLogFields(c),
					"response": getResponseLogFields(fiber.StatusInternalServerError, t),
					"error": fiber.Map{
						"message": err.Error(),
						"stack":   stacktrace.NewStackTrace(skipStackTraceFrame),
					},
				}).Errorf("recover: %v", err)
			}
		}()

		return c.Next()
	}
}
