package config

import (
	"os"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"
)

func TestConfiguration(t *testing.T) {
	old := GlobalConfig
	t.Cleanup(func() { viper.Reset(); GlobalConfig = old })
	viper.Reset()
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("config.json", []byte(`{"APP_NAME":"vke","PORT":"8080","ENV":"production","VERSION":"test","MYSQL_URL":"mysql-dsn"}`), 0600))
	c := NewConfigureManager()
	assert.Same(t, c, GlobalConfig)
	assert.Equal(t, WebConfig{AppName: "vke", Port: "8080", Env: "production", Version: "test"}, c.GetWebConfig())
	assert.True(t, c.GetWebConfig().IsProductionEnv())
	assert.False(t, (WebConfig{Env: "test"}).IsProductionEnv())
	assert.Equal(t, MysqlDBConfig{URL: "mysql-dsn"}, c.GetMysqlDBConfig())
	assert.Equal(t, LanguageConfig{Default: language.English, Languages: []language.Tag{language.English}}, c.GetLanguageConfig())
}
