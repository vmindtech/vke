package config

import (
	"github.com/spf13/viper"
	"golang.org/x/text/language"
)

const (
	VaultEnvPath         = "%s/data/%s"
	EnvironmentTypeLocal = "local"
)

var GlobalConfig IConfigureManager

type IConfigureManager interface {
	GetWebConfig() WebConfig
	GetMysqlDBConfig() MysqlDBConfig
	GetLanguageConfig() LanguageConfig
}

type configureManager struct {
	App      WebConfig
	Mysql    MysqlDBConfig
	Language LanguageConfig
}

func NewConfigureManager() IConfigureManager {
	viper.SetConfigFile("config.json")
	viper.SetConfigType("json")

	_ = viper.ReadInConfig()

	GlobalConfig = &configureManager{
		App:      loadWebConfig(),
		Mysql:    loadMysqlDBConfig(),
		Language: loadLanguageConfig(),
	}

	return GlobalConfig
}

func (c *configureManager) GetWebConfig() WebConfig {
	return c.App
}

func (c *configureManager) GetLanguageConfig() LanguageConfig {
	return c.Language
}

func (c *configureManager) GetMysqlDBConfig() MysqlDBConfig {
	return c.Mysql
}

func loadMysqlDBConfig() MysqlDBConfig {
	return MysqlDBConfig{
		URL: viper.GetString("MYSQL_URL"),
	}
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
