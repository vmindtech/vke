package service

import (
	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/internal/repository"
)

type IAppService interface {
	Cluster() IClusterService
}

type appService struct {
	logger         *logrus.Logger
	repository     repository.IRepository
	clusterService IClusterService
}

func NewAppService(l *logrus.Logger, r repository.IRepository, cs IClusterService) IAppService {
	return &appService{
		logger:         l,
		repository:     r,
		clusterService: cs,
	}
}

func (a *appService) Cluster() IClusterService {
	return a.clusterService
}
