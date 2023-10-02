package config

import (
	"fmt"
	"time"

	"golang.org/x/text/language"
)

const (
	productionEnv = "production"
)

type Configuration struct {
	App          WebConfig
	Mysql        MysqlDBConfig
	Redis        RedisConfig
	NewRelic     NewRelicConfig
	Slack        SlackConfig
	Language     LanguageConfig
	JWT          JWTConfig
	AWS          AWSConfig
	S3           S3Config
	SES          SESConfig
	SQSConsumer  SQSConfig
	SQSPublisher SQSConfig
}

type WebConfig struct {
	AppName   string
	Port      string
	Env       string
	WebAppURL string
	Debug     bool
}

type MysqlDBConfig struct {
	URL string
}

type RedisConfig struct {
	URL      string
	Port     string
	Password string
	Database int
}
type NewRelicConfig struct {
	AppName string
	Key     string
}

type SlackConfig struct {
	URL      string
	Channel  string
	Username string
}

type LanguageConfig struct {
	Default   language.Tag
	Languages []language.Tag
}

type JWTConfig struct {
	AppName    string
	UserSecret string
	UserExpr   string
}

type AWSConfig struct {
	Region string
}

type S3Config struct {
	StorageBucket             string
	URLExpirationTimeAsMinute int
}

type SESConfig struct {
	Region string
	Sender string
}

type SQSConfig struct {
	QueueName                       string
	MaxNumberOfMessages             int64
	VisibilityTimeout               int64
	WaitTimeSeconds                 int64
	ErrorCountThreshold             int
	ConsumerPollingTimeMilliseconds int64
}

func (w WebConfig) IsProductionEnv() bool {
	return w.Env == productionEnv
}

func (j JWTConfig) GetUserExpirationTime() time.Duration {
	d, _ := time.ParseDuration(fmt.Sprintf("%ss", j.UserExpr))
	return d
}
