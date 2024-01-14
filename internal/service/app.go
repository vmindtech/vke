package service

import (
	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/internal/repository"
)

type IAppService interface {
	Cluster() IClusterService
	Compute() IComputeService
	NodeGroups() INodeGroupsService
}

type appService struct {
	logger            *logrus.Logger
	repository        repository.IRepository
	clusterService    IClusterService
	computeService    IComputeService
	nodeGroupsService INodeGroupsService
}

func NewAppService(l *logrus.Logger, r repository.IRepository, cs IClusterService, coms IComputeService, nodg INodeGroupsService) IAppService {
	return &appService{
		logger:            l,
		repository:        r,
		clusterService:    cs,
		computeService:    coms,
		nodeGroupsService: nodg,
	}
}

func (a *appService) Cluster() IClusterService {
	return a.clusterService
}
func (a *appService) Compute() IComputeService {
	return a.computeService
}
func (a *appService) NodeGroups() INodeGroupsService {
	return a.nodeGroupsService
}
