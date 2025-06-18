package config

import (
	"golang.org/x/text/language"
)

const (
	productionEnv = "production"
)

type WebConfig struct {
	AppName  string
	Port     string
	Env      string
	Version  string
	Endpoint string
}

type OpenSearchConfig struct {
	Addresses []string
	Username  string
	Password  string
	Index     string
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
	IdentityEndpoint     string
	BlockStorageEndpoint string
	EnvoyEndpoint        string
}

type CloudflareConfig struct {
	CfToken string
	ZoneID  string
	Domain  string
}

type ImageRef struct {
	ImageRef string
}

type PublicNetworkID struct {
	PublicNetworkID string
}

type OpenStackApiConfig struct {
	NovaMicroVersion     string
	LoadbalancerProvider string
}

type VkeAgentConfig struct {
	VkeAgentVersion          string
	ClusterAgentVersion      string
	ClusterAutoscalerVersion string
	CloudProviderVkeVersion  string
}

type OpenStackRolesConfig struct {
	OpenstackLoadbalancerRole string
	OpenstackMemberOrUserRole string
}

func (w WebConfig) IsProductionEnv() bool {
	return w.Env == productionEnv
}
