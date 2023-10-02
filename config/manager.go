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
	GetLanguageConfig() LanguageConfig
}

type configureManager struct {
	Web      WebConfig
	Language LanguageConfig
}

func NewConfigureManager() IConfigureManager {
	viper.SetConfigFile("config.json")
	viper.SetConfigType("json")
	viper.AddConfigPath("./config.json")

	_ = viper.ReadInConfig()

	GlobalConfig = &configureManager{
		Web:      loadWebConfig(),
		Language: loadLanguageConfig(),
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

func (c *configureManager) GetWebConfig() WebConfig {
	return c.Web
}

func (c *configureManager) GetLanguageConfig() LanguageConfig {
	return c.Language
}
