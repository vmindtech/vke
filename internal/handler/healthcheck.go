package handler

import (
	"github.com/gofiber/fiber/v2"

	"github.com/vmindtech/vke/pkg/healthcheck"
)

const (
	healthLivenessStatusOk       = "UP"
	healthLivenessStatusShutdown = "SHUTDOWN"
	healthReadinessStatusOk      = "READY"
)

type IHealthCheckHandler interface {
	Liveness(c *fiber.Ctx) error
	Readiness(c *fiber.Ctx) error
}

type healthCheckHandler struct{}

func NewHealthCheckHandler() IHealthCheckHandler {
	return &healthCheckHandler{}
}

func (h *healthCheckHandler) Liveness(c *fiber.Ctx) error {
	if !healthcheck.Liveness() {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"status": healthLivenessStatusShutdown})
	}

	return c.JSON(fiber.Map{"status": healthLivenessStatusOk})
}

func (h *healthCheckHandler) Readiness(c *fiber.Ctx) error {
	// readiness := healthcheck.Readiness()
	// if !healthcheck.IsConnectionSuccessful(readiness) {
	// 	return c.Status(fiber.StatusInternalServerError).JSON(readiness)
	// }

	return c.JSON(fiber.Map{"status": healthReadinessStatusOk})
}
