package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
	"golang.org/x/text/language"
)

const (
	EnvironmentTypeLocal = "local"
)

var GlobalConfig IConfigureManager

type IConfigureManager interface {
	GetWebConfig() WebConfig
	GetMysqlDBConfig() MysqlDBConfig
	GetCloudflareConfig() CloudflareConfig
	GetImageRefConfig() ImageRef
	GetPublicNetworkIDConfig() PublicNetworkID
	GetLanguageConfig() LanguageConfig
	GetEndpointsConfig() APIEndpointsConfig
	GetOpenStackApiConfig() OpenStackApiConfig
	GetVkeAgentConfig() VkeAgentConfig
	GetOpenstackRolesConfig() OpenStackRolesConfig
	GetOpenSearchConfig() OpenSearchConfig
}

type configureManager struct {
	Web                  WebConfig
	Mysql                MysqlDBConfig
	APIEndpoints         APIEndpointsConfig
	ImageRef             ImageRef
	PublicNetworkID      PublicNetworkID
	Cloudflare           CloudflareConfig
	Language             LanguageConfig
	OpenStackApiConfig   OpenStackApiConfig
	VkeAgentConfig       VkeAgentConfig
	OpenStackRolesConfig OpenStackRolesConfig
	OpenSearchConfig     OpenSearchConfig
}

func NewConfigureManager() IConfigureManager {
	configPath := "./"

	if os.Getenv("GO_VAULT_PATH") != "" {
		configPath = os.Getenv("GO_VAULT_PATH")
	}

	viper.SetConfigFile(fmt.Sprintf("%sconfig-%s.json", configPath, os.Getenv("golang_env")))
	viper.SetConfigType("json")

	_ = viper.ReadInConfig()

	GlobalConfig = &configureManager{
		Web:                  loadWebConfig(),
		Language:             loadLanguageConfig(),
		Mysql:                loadMysqlDBConfig(),
		Cloudflare:           loadCloudflareConfig(),
		ImageRef:             loadImageRefConfig(),
		PublicNetworkID:      loadPublicNetworkIDConfig(),
		OpenSearchConfig:     loadOpenSearchConfig(),
		APIEndpoints:         loadAPIEndpointsConfig(),
		OpenStackApiConfig:   loadOpenStackApiConfig(),
		VkeAgentConfig:       loadVkeAgentConfig(),
		OpenStackRolesConfig: loadOpenstackRolesConfig(),
	}

	return GlobalConfig
}

func loadWebConfig() WebConfig {
	return WebConfig{
		AppName:  viper.GetString("APP_NAME"),
		Port:     viper.GetString("PORT"),
		Env:      viper.GetString("ENV"),
		Version:  viper.GetString("VERSION"),
		Endpoint: viper.GetString("ENDPOINT"),
	}
}

func loadLanguageConfig() LanguageConfig {
	return LanguageConfig{
		Default: language.English,
		Languages: []language.Tag{
			language.English,
		},
	}
}

func loadCloudflareConfig() CloudflareConfig {
	return CloudflareConfig{
		CfToken: viper.GetString("CLOUDFLARE_AUTH_TOKEN"),
		ZoneID:  viper.GetString("CLOUDFLARE_ZONE_ID"),
		Domain:  viper.GetString("CLOUDFLARE_DOMAIN"),
	}
}

func loadOpenSearchConfig() OpenSearchConfig {
	return OpenSearchConfig{
		Addresses: viper.GetStringSlice("OPENSEARCH_ADDRESSES"),
		Username:  viper.GetString("OPENSEARCH_USERNAME"),
		Password:  viper.GetString("OPENSEARCH_PASSWORD"),
		Index:     viper.GetString("OPENSEARCH_INDEX"),
	}
}

func loadImageRefConfig() ImageRef {
	return ImageRef{
		ImageRef: viper.GetString("IMAGE_REF"),
	}
}

func loadPublicNetworkIDConfig() PublicNetworkID {
	return PublicNetworkID{
		PublicNetworkID: viper.GetString("PUBLIC_NETWORK_ID"),
	}
}

func loadMysqlDBConfig() MysqlDBConfig {
	return MysqlDBConfig{
		URL: viper.GetString("MYSQL_URL"),
	}
}

func loadAPIEndpointsConfig() APIEndpointsConfig {
	return APIEndpointsConfig{
		ComputeEndpoint:      viper.GetString("COMPUTE_ENDPOINT"),
		NetworkEndpoint:      viper.GetString("NETWORK_ENDPOINT"),
		LoadBalancerEndpoint: viper.GetString("LOAD_BALANCER_ENDPOINT"),
		IdentityEndpoint:     viper.GetString("IDENTITY_ENDPOINT"),
		BlockStorageEndpoint: viper.GetString("BLOCK_STORAGE_ENDPOINT"),
		EnvoyEndpoint:        viper.GetString("ENVOY_ENDPOINT"),
	}
}

func loadOpenStackApiConfig() OpenStackApiConfig {
	return OpenStackApiConfig{
		NovaMicroVersion:     viper.GetString("NOVA_MICRO_VERSION"),
		LoadbalancerProvider: viper.GetString("LOADBALANCER_PROVIDER"),
	}
}

func loadVkeAgentConfig() VkeAgentConfig {
	return VkeAgentConfig{
		VkeAgentVersion:     viper.GetString("VKE_AGENT_VERSION"),
		ClusterAgentVersion: viper.GetString("CLUSTER_AGENT_VERSION"),
	}
}

func loadOpenstackRolesConfig() OpenStackRolesConfig {
	return OpenStackRolesConfig{
		OpenstackLoadbalancerRole: viper.GetString("OPENSTACK_LOADBALANCER_ADMIN_ROLE"),
		OpenstackMemberOrUserRole: viper.GetString("OPENSTACK_USER_OR_MEMBER_ROLE"),
	}
}

func (c *configureManager) GetWebConfig() WebConfig {
	return c.Web
}

func (c *configureManager) GetLanguageConfig() LanguageConfig {
	return c.Language
}

func (c *configureManager) GetMysqlDBConfig() MysqlDBConfig {
	return c.Mysql
}

func (c *configureManager) GetCloudflareConfig() CloudflareConfig {
	return c.Cloudflare
}

func (c *configureManager) GetImageRefConfig() ImageRef {
	return c.ImageRef
}

func (c *configureManager) GetPublicNetworkIDConfig() PublicNetworkID {
	return c.PublicNetworkID
}

func (c *configureManager) GetOpenSearchConfig() OpenSearchConfig {
	return c.OpenSearchConfig
}

func (c *configureManager) GetEndpointsConfig() APIEndpointsConfig {
	return APIEndpointsConfig{
		ComputeEndpoint:      viper.GetString("COMPUTE_ENDPOINT"),
		NetworkEndpoint:      viper.GetString("NETWORK_ENDPOINT"),
		LoadBalancerEndpoint: viper.GetString("LOAD_BALANCER_ENDPOINT"),
		IdentityEndpoint:     viper.GetString("IDENTITY_ENDPOINT"),
		BlockStorageEndpoint: viper.GetString("BLOCK_STORAGE_ENDPOINT"),
		EnvoyEndpoint:        viper.GetString("ENVOY_ENDPOINT"),
	}
}

func (c *configureManager) GetOpenStackApiConfig() OpenStackApiConfig {
	return OpenStackApiConfig{
		NovaMicroVersion:     viper.GetString("NOVA_MICRO_VERSION"),
		LoadbalancerProvider: viper.GetString("LOADBALANCER_PROVIDER"),
	}
}

func (c *configureManager) GetVkeAgentConfig() VkeAgentConfig {
	return VkeAgentConfig{
		VkeAgentVersion:          viper.GetString("VKE_AGENT_VERSION"),
		ClusterAgentVersion:      viper.GetString("CLUSTER_AGENT_VERSION"),
		ClusterAutoscalerVersion: viper.GetString("CLUSTER_AUTOSCALER_VERSION"),
		CloudProviderVkeVersion:  viper.GetString("CLOUD_PROVIDER_VKE_VERSION"),
	}
}

func (c *configureManager) GetOpenstackRolesConfig() OpenStackRolesConfig {
	return OpenStackRolesConfig{
		OpenstackLoadbalancerRole: viper.GetString("OPENSTACK_LOADBALANCER_ADMIN_ROLE"),
		OpenstackMemberOrUserRole: viper.GetString("OPENSTACK_USER_OR_MEMBER_ROLE"),
	}
}
