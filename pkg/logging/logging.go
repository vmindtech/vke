package logging

import (
	"io"
	"os"

	"github.com/sirupsen/logrus"
)

const (
	fieldKeyMsg     = "message"
	fieldKeyTime    = "timestamp"
	timestampFormat = "2006-01-02 15:04:05"
)

type Config struct {
	Service    ServiceConfig
	OpenSearch *OpenSearchConfig
}

func NewLogger(config Config) *logrus.Logger {
	logger := logrus.New()

	if config.OpenSearch == nil || len(config.OpenSearch.Addresses) == 0 {
		logger.SetOutput(os.Stdout)
	} else {
		logger.SetOutput(io.Discard)
	}

	logger.SetReportCaller(true)
	logger.SetFormatter(jsonFormatter())

	logger.AddHook(NewServiceHook(config.Service))

	if config.OpenSearch != nil && len(config.OpenSearch.Addresses) > 0 {
		client, err := NewOpenSearchClient(*config.OpenSearch)
		if err != nil {
			return logger
		}
		logger.AddHook(NewOpenSearchHook(client, config.OpenSearch.Index))
	}

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
