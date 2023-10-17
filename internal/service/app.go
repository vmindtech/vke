package service

import (
	"context"

	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/repository"
	"github.com/vmindtech/vke/pkg/utils"
)

type IAppService interface {
	ListClusters(ctx context.Context, projectUUID string) ([]*model.Cluster, error)
}

type appService struct {
	logger     *logrus.Logger
	repository repository.IRepository
}

func NewAppService(l *logrus.Logger, r repository.IRepository) IAppService {
	return &appService{
		logger:     l,
		repository: r,
	}
}

func (a *appService) ListClusters(ctx context.Context, projectUUID string) ([]*model.Cluster, error) {
	clusters, err := a.repository.Cluster().ListClustersByProjectUUID(ctx, projectUUID)
	if err != nil {
		return nil, err
	}

	if len(clusters) == 0 {
		return nil, utils.ErrorBag{Code: utils.NotFoundErrCode, Cause: err}
	}

	return clusters, nil
}
