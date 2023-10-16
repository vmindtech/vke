package goboilerplate

import (
	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/internal/handler"
	"github.com/vmindtech/vke/internal/repository"
	"github.com/vmindtech/vke/internal/route"
	"github.com/vmindtech/vke/internal/service"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

func InitHealthCheckHandler() handler.IHealthCheckHandler {
	iHealthCheckHandler := handler.NewHealthCheckHandler()
	return iHealthCheckHandler
}

func InitRoute(l *logrus.Logger, mysqlInstance mysqldb.IMysqlInstance) route.IRoute {
	iClusterRepository := repository.NewClusterRepository(mysqlInstance)
	iRepository := repository.NewRepository(mysqlInstance, iClusterRepository)

	iAppService := service.NewAppService(l, iRepository)
	iAppHandler := handler.NewAppHandler(iAppService)
	iRoute := route.NewRoute(iAppHandler)
	return iRoute
}
