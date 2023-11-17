package config

import (
	"golang.org/x/text/language"
)

const (
	productionEnv = "production"
)

type WebConfig struct {
	AppName string
	Port    string
	Env     string
	Version string
}

type LanguageConfig struct {
	Default   language.Tag
	Languages []language.Tag
}

type MysqlDBConfig struct {
	URL string
}

type APIEndpointsConfig struct {
	ComputeEndpoint      string
	NetworkEndpoint      string
	LoadBalancerEndpoint string
}

type CloudflareConfig struct {
	AuthToken string
	ZoneID    string
	Domain    string
}

func (w WebConfig) IsProductionEnv() bool {
	return w.Env == productionEnv
}
