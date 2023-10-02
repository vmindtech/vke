package logging

import (
	"os"

	"github.com/sirupsen/logrus"
)

const (
	fieldKeyMsg     = "message"
	fieldKeyTime    = "timestamp"
	timestampFormat = "2006-01-02 15:04:05"
)

type Config struct {
	Service ServiceConfig
}

func NewLogger(config Config) *logrus.Logger {
	logger := logrus.New()
	logger.SetOutput(os.Stdout)
	logger.SetReportCaller(true)
	logger.SetFormatter(jsonFormatter())

	logger.AddHook(NewServiceHook(config.Service))

	return logger
}

func jsonFormatter() *logrus.JSONFormatter {
	return &logrus.JSONFormatter{
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyMsg:  fieldKeyMsg,
			logrus.FieldKeyTime: fieldKeyTime,
		},
		TimestampFormat: timestampFormat,
	}
}
