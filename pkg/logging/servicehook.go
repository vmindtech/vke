package logging

import (
	"github.com/sirupsen/logrus"
)

type ServiceConfig struct {
	Env     string
	AppName string
}

type ServiceHook struct {
	Env     string
	Service string
}

func (s *ServiceHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (s *ServiceHook) Fire(entry *logrus.Entry) error {
	entry.Data["env"] = s.Env
	entry.Data["serviceName"] = s.Service

	return nil
}

func NewServiceHook(config ServiceConfig) *ServiceHook {
	return &ServiceHook{
		Env:     config.Env,
		Service: config.AppName,
	}
}
