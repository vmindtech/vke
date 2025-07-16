package logging

import (
	"fmt"
	"io"
	"net"
	"os"

	"github.com/sirupsen/logrus"
)

const (
	fieldKeyMsg     = "message"
	fieldKeyTime    = "timestamp"
	timestampFormat = "2006-01-02 15:04:05"
)

type Config struct {
	Service  ServiceConfig
	Logstash *LogstashConfig
}

func NewLogger(config Config) *logrus.Logger {
	logger := logrus.New()

	if config.Logstash == nil || config.Logstash.Host == "" {
		logger.SetOutput(os.Stdout)
	} else {
		logger.SetOutput(io.Discard)
	}

	logger.SetReportCaller(true)
	logger.SetFormatter(jsonFormatter())

	logger.AddHook(NewServiceHook(config.Service))

	if config.Logstash != nil && config.Logstash.Host != "" {
		conn, err := NewLogstashClient(*config.Logstash)
		if err != nil {
			return logger
		}

		addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", config.Logstash.Host, config.Logstash.Port))
		if err != nil {
			return logger
		}

		logger.AddHook(NewLogstashHook(conn, addr))
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
