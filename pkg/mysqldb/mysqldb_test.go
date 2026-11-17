package mysqldb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type pingConnector struct{ conn *pingConn }
type pingDriver struct{}
type pingConn struct {
	err    error
	closed bool
	ctx    context.Context
}

func (pingDriver) Open(string) (driver.Conn, error)                  { return nil, errors.New("use connector") }
func (c pingConnector) Driver() driver.Driver                        { return pingDriver{} }
func (c pingConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c *pingConn) Prepare(string) (driver.Stmt, error)              { return nil, errors.New("not supported") }
func (c *pingConn) Begin() (driver.Tx, error)                        { return nil, errors.New("not supported") }
func (c *pingConn) Close() error                                     { c.closed = true; return nil }
func (c *pingConn) Ping(ctx context.Context) error                   { c.ctx = ctx; return c.err }

func TestMysqlInstance(t *testing.T) {
	for _, pingErr := range []error{nil, errors.New("ping failed")} {
		conn := &pingConn{err: pingErr}
		db := sql.OpenDB(pingConnector{conn})
		defer db.Close()
		instance := mysqlInstance{db: &gorm.DB{}, sqlDB: db}
		assert.Same(t, instance.db, instance.Database())
		ctx, cancel := context.WithCancel(context.Background())
		require.ErrorIs(t, instance.Ping(ctx), pingErr)
		assert.Same(t, ctx, conn.ctx)
		cancel()
		require.ErrorIs(t, instance.Ping(ctx), context.Canceled)
		require.NoError(t, instance.Close())
		assert.True(t, conn.closed)
	}
}

func TestInitMysqlDBInvalidDSN(t *testing.T) {
	assert.Equal(t, "mysql", getDriverName())
	instance, err := InitMysqlDB("user:password@tcp(127.0.0.1:3306)/db?timeout=invalid-duration")
	require.Error(t, err)
	assert.Nil(t, instance)
	instance, err = InitMysqlDB("user:password@unix(/nonexistent-vke-test-mysql.sock)/db?timeout=1s")
	require.Error(t, err)
	assert.Nil(t, instance)
}
