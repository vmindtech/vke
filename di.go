package goboilerplate

import (
	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/internal/handler"
	"github.com/vmindtech/vke/internal/route"
	"github.com/vmindtech/vke/internal/service"
)

func InitHealthCheckHandler() handler.IHealthCheckHandler {
	iHealthCheckHandler := handler.NewHealthCheckHandler()
	return iHealthCheckHandler
}

func InitRoute(l *logrus.Logger) route.IRoute {
	// iRepository := repository.NewRepository()
	iAppService := service.NewAppService(l)
	iAppHandler := handler.NewAppHandler(iAppService)
	iRoute := route.NewRoute(iAppHandler)
	return iRoute
}
