package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"
)

func TestConfigurationFromFile(t *testing.T) {
	old := GlobalConfig
	t.Cleanup(func() { viper.Reset(); GlobalConfig = old })
	viper.Reset()
	dir := t.TempDir()
	t.Setenv("GO_VAULT_PATH", dir+string(os.PathSeparator))
	t.Setenv("golang_env", "test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config-test.json"), []byte(`{
	"APP_NAME":"vke","PORT":"8080","ENV":"production","VERSION":"test","ENDPOINT":"http://api",
	"MYSQL_URL":"mysql-dsn","RABBITMQ_URL":"amqp://test","RABBITMQ_QUEUE":"jobs","WORKER_CONCURRENCY":12,
	"VKE_ENCRYPTION_KEY":"test-key","CLOUDFLARE_AUTH_TOKEN":"test-token","CLOUDFLARE_ZONE_ID":"zone","CLOUDFLARE_DOMAIN":"example.test",
	"IMAGE_REF":"image","PUBLIC_NETWORK_ID":"public","LOGSTASH_HOST":"127.0.0.1","LOGSTASH_PORT":5044,
	"COMPUTE_ENDPOINT":"compute","NETWORK_ENDPOINT":"network","LOAD_BALANCER_ENDPOINT":"lb","IDENTITY_ENDPOINT":"identity","BLOCK_STORAGE_ENDPOINT":"storage","ENVOY_ENDPOINT":"envoy",
	"NOVA_MICRO_VERSION":"2.15","LOADBALANCER_PROVIDER":"ovn","VKE_AGENT_VERSION":"agent","CLUSTER_AGENT_VERSION":"cluster-agent","CLUSTER_AUTOSCALER_VERSION":"autoscaler","CLOUD_PROVIDER_VKE_VERSION":"provider",
	"OPENSTACK_LOADBALANCER_ADMIN_ROLE":"lb-role","OPENSTACK_USER_OR_MEMBER_ROLE":"member"
	}`), 0600))
	c := NewConfigureManager()
	assert.Same(t, c, GlobalConfig)
	assert.Equal(t, WebConfig{AppName: "vke", Port: "8080", Env: "production", Version: "test", Endpoint: "http://api"}, c.GetWebConfig())
	assert.True(t, c.GetWebConfig().IsProductionEnv())
	assert.False(t, (WebConfig{Env: "test"}).IsProductionEnv())
	assert.Equal(t, MysqlDBConfig{URL: "mysql-dsn"}, c.GetMysqlDBConfig())
	assert.Equal(t, RabbitMQConfig{URL: "amqp://test", QueueName: "jobs", Concurrency: 12}, c.GetRabbitMQConfig())
	assert.Equal(t, EncryptionConfig{Key: "test-key"}, c.GetEncryptionConfig())
	assert.Equal(t, CloudflareConfig{CfToken: "test-token", ZoneID: "zone", Domain: "example.test"}, c.GetCloudflareConfig())
	assert.Equal(t, ImageRef{ImageRef: "image"}, c.GetImageRefConfig())
	assert.Equal(t, PublicNetworkID{PublicNetworkID: "public"}, c.GetPublicNetworkIDConfig())
	assert.Equal(t, LogstashConfig{Host: "127.0.0.1", Port: 5044}, c.GetLogstashConfig())
	assert.Equal(t, APIEndpointsConfig{ComputeEndpoint: "compute", NetworkEndpoint: "network", LoadBalancerEndpoint: "lb", IdentityEndpoint: "identity", BlockStorageEndpoint: "storage", EnvoyEndpoint: "envoy"}, c.GetEndpointsConfig())
	assert.Equal(t, OpenStackApiConfig{NovaMicroVersion: "2.15", LoadbalancerProvider: "ovn"}, c.GetOpenStackApiConfig())
	assert.Equal(t, VkeAgentConfig{VkeAgentVersion: "agent", ClusterAgentVersion: "cluster-agent", ClusterAutoscalerVersion: "autoscaler", CloudProviderVkeVersion: "provider"}, c.GetVkeAgentConfig())
	assert.Equal(t, OpenStackRolesConfig{OpenstackLoadbalancerRole: "lb-role", OpenstackMemberOrUserRole: "member"}, c.GetOpenstackRolesConfig())
	assert.Equal(t, LanguageConfig{Default: language.English, Languages: []language.Tag{language.English}}, c.GetLanguageConfig())
}

func TestConfigurationDefaultsAndConcurrencyBounds(t *testing.T) {
	old := GlobalConfig
	t.Cleanup(func() { viper.Reset(); GlobalConfig = old })
	viper.Reset()
	t.Chdir(t.TempDir())
	t.Setenv("GO_VAULT_PATH", "")
	t.Setenv("golang_env", "missing")
	c := NewConfigureManager()
	assert.Equal(t, 8, c.GetRabbitMQConfig().Concurrency)
	assert.Empty(t, c.GetMysqlDBConfig().URL)
	for _, tc := range []struct{ input, want int }{{-1, 8}, {0, 8}, {1, 1}, {64, 64}, {65, 64}} {
		viper.Set("WORKER_CONCURRENCY", tc.input)
		assert.Equal(t, tc.want, loadRabbitMQConfig().Concurrency)
	}
}
