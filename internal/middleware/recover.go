package middleware

import (
	"fmt"
	"strings"
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

				errMsg := err.Error()
				stack := stacktrace.NewStackTrace(skipStackTraceFrame)

				if strings.Contains(errMsg, "hpack") ||
					strings.Contains(errMsg, "http2") ||
					strings.Contains(errMsg, "id <= evictCount") {

					c.Request().Header.Set("Connection", "close")

					l.WithFields(logrus.Fields{
						"ip":       c.IP(),
						"request":  getRequestLogFields(c),
						"response": getResponseLogFields(fiber.StatusServiceUnavailable, t),
						"error": fiber.Map{
							"message": errMsg,
							"type":    "http2_hpack_error",
							"stack":   stack,
						},
					}).Warn("HTTP/2 HPACK error, falling back to HTTP/1.1")

					err = c.SendStatus(fiber.StatusServiceUnavailable)
					return
				}

				l.WithFields(logrus.Fields{
					"ip":       c.IP(),
					"request":  getRequestLogFields(c),
					"response": getResponseLogFields(fiber.StatusInternalServerError, t),
					"error": fiber.Map{
						"message": errMsg,
						"stack":   stack,
					},
				}).Errorf("recover: %v", err)

				err = c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "Internal Server Error",
				})
			}
		}()

		return c.Next()
	}
}
