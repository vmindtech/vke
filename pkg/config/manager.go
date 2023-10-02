package config

import (
	"log"

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
	GetLanguageConfig() LanguageConfig
}

type configureManager struct {
	App      WebConfig
	Language LanguageConfig
}

func NewConfigureManager() IConfigureManager {
	viper.SetConfigFile("config.json")
	viper.SetConfigType("json")

	var configuration Configuration
	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("Error reading config file, %s", err)
	}

	err := viper.Unmarshal(&configuration)
	if err != nil {
		log.Fatalf("unable to decode into struct, %v", err)
	}

	GlobalConfig = &configureManager{
		App:      configuration.App,
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

func loadLanguageConfig() LanguageConfig {
	return LanguageConfig{
		Default: language.English,
		Languages: []language.Tag{
			language.English,
		},
	}
}
