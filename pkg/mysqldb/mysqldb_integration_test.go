//go:build integration

package mysqldb

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitMysqlDB_LocalDatabase(t *testing.T) {
	dsn := os.Getenv("VKE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set VKE_TEST_MYSQL_DSN to a disposable local MySQL database")
	}
	cfg, err := driver.ParseDSN(dsn)
	require.NoError(t, err)
	require.Equal(t, "tcp", cfg.Net)
	host, _, err := net.SplitHostPort(cfg.Addr)
	require.NoError(t, err)
	require.Contains(t, []string{"localhost", "127.0.0.1", "::1"}, host)
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 5*time.Second, 5*time.Second, 5*time.Second
	db, err := InitMysqlDB(cfg.FormatDSN())
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })
	require.NotNil(t, db.Database())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, db.Ping(ctx))
	var one int
	require.NoError(t, db.Database().WithContext(ctx).Raw("SELECT 1").Scan(&one).Error)
	assert.Equal(t, 1, one)
	sqlDB, err := db.Database().DB()
	require.NoError(t, err)
	assert.Equal(t, dbMaxOpenConn, sqlDB.Stats().MaxOpenConnections)
}
