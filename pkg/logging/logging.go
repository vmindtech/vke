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

	if config.Logstash != nil && config.Logstash.Host != "" && config.Logstash.Port > 0 {
		logger.SetOutput(io.Discard)

		conn, err := NewLogstashClient(*config.Logstash)
		if err != nil {
			logger.SetOutput(os.Stdout)
			fmt.Printf("Failed to connect to Logstash: %v, falling back to console output\n", err)
		} else {
			addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", config.Logstash.Host, config.Logstash.Port))
			if err != nil {
				logger.SetOutput(os.Stdout)
				fmt.Printf("Failed to resolve Logstash address: %v, falling back to console output\n", err)
			} else {
				logger.AddHook(NewLogstashHook(conn, addr))
			}
		}
	} else {
		logger.SetOutput(os.Stdout)
	}

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
