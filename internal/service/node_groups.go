package service

import (
	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/internal/repository"
)

type INodeGroupsService interface {
}

type nodeGroupsService struct {
	repository repository.IRepository
	logger     *logrus.Logger
}

func NewNodeGroupsService(logger *logrus.Logger, repository repository.IRepository) INodeGroupsService {
	return &nodeGroupsService{
		repository: repository,
		logger:     logger,
	}
}
