package testutil

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/vmindtech/vke/config"
)

// SetConfig sets viper keys and rebuilds config.GlobalConfig; restored on cleanup.
func SetConfig(t *testing.T, kv map[string]any) {
	t.Helper()
	for k, v := range kv {
		viper.Set(k, v)
	}
	config.NewConfigureManager()
	t.Cleanup(func() {
		viper.Reset()
		config.NewConfigureManager()
	})
}
