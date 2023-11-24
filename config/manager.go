package config

import (
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
	GetPublicSubnetIDConfig() PublicSubnetID
	GetLanguageConfig() LanguageConfig
	GetEndpointsConfig() APIEndpointsConfig
}

type configureManager struct {
	Web            WebConfig
	Mysql          MysqlDBConfig
	APIEndpoints   APIEndpointsConfig
	ImageRef       ImageRef
	PublicSubnetID PublicSubnetID
	Cloudflare     CloudflareConfig
	Language       LanguageConfig
}

func NewConfigureManager() IConfigureManager {
	viper.SetConfigFile("config.json")
	viper.SetConfigType("json")
	viper.AddConfigPath("./config.json")

	_ = viper.ReadInConfig()

	GlobalConfig = &configureManager{
		Web:            loadWebConfig(),
		Language:       loadLanguageConfig(),
		Mysql:          loadMysqlDBConfig(),
		Cloudflare:     loadCloudflareConfig(),
		ImageRef:       loadImageRefConfig(),
		PublicSubnetID: loadPublicSubnetIDConfig(),
		APIEndpoints:   loadAPIEndpointsConfig(),
	}

	return GlobalConfig
}

func loadWebConfig() WebConfig {
	return WebConfig{
		AppName: viper.GetString("APP_NAME"),
		Port:    viper.GetString("PORT"),
		Env:     viper.GetString("ENV"),
		Version: viper.GetString("VERSION"),
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
		AuthToken: viper.GetString("CLOUDFLARE_AUTH_TOKEN"),
		ZoneID:    viper.GetString("CLOUDFLARE_ZONE_ID"),
		Domain:    viper.GetString("CLOUDFLARE_DOMAIN"),
	}
}

func loadImageRefConfig() ImageRef {
	return ImageRef{
		ImageRef: viper.GetString("IMAGE_REF"),
	}
}

func loadPublicSubnetIDConfig() PublicSubnetID {
	return PublicSubnetID{
		PublicSubnetID: viper.GetString("PUBLIC_SUBNET_ID"),
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

func (c *configureManager) GetPublicSubnetIDConfig() PublicSubnetID {
	return c.PublicSubnetID
}

func (c *configureManager) GetEndpointsConfig() APIEndpointsConfig {
	return APIEndpointsConfig{
		ComputeEndpoint:      viper.GetString("COMPUTE_ENDPOINT"),
		NetworkEndpoint:      viper.GetString("NETWORK_ENDPOINT"),
		LoadBalancerEndpoint: viper.GetString("LOAD_BALANCER_ENDPOINT"),
		IdentityEndpoint:     viper.GetString("IDENTITY_ENDPOINT"),
	}
}
