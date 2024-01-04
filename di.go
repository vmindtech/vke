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
	iAuditRepository := repository.NewAuditLogRepository(mysqlInstance)
	iKubeConfigRepository := repository.NewKubeconfigRepository(mysqlInstance)
	iNodeGroupsRepository := repository.NewNodeGroupsRepository(mysqlInstance)
	iRepository := repository.NewRepository(mysqlInstance, iClusterRepository, iAuditRepository, iKubeConfigRepository, iNodeGroupsRepository)

	iComputeService := service.NewComputeService(l)
	iNetworkService := service.NewNetworkService(l)
	iCloudflareService := service.NewCloudflareService(l)
	iLoadbalancerService := service.NewLoadbalancerService(l)
	iClusterService := service.NewClusterService(l, iCloudflareService, iLoadbalancerService, iNetworkService, iComputeService, iRepository)
	iAppService := service.NewAppService(l, iRepository, iClusterService)
	iAppHandler := handler.NewAppHandler(iAppService)
	iRoute := route.NewRoute(iAppHandler)
	return iRoute
}
