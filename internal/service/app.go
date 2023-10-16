package service

import (
	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/internal/repository"
)

type IAppService interface {
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
