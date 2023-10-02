package service

import (
	"github.com/sirupsen/logrus"
)

type IAppService interface {
}

type appService struct {
	logger *logrus.Logger
}

func NewAppService(l *logrus.Logger) IAppService {
	return &appService{
		logger: l,
	}
}
