package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// A test-only SQL boundary keeps these tests independent of a MySQL daemon and
// additional module dependencies. GORM still builds and scans the real queries.
type sqlStep struct {
	query    string
	pattern  string
	args     []any
	columns  []string
	rows     [][]driver.Value
	affected int64
	err      error
	check    func([]driver.NamedValue)
}
type sqlScript struct {
	t                   *testing.T
	steps               []sqlStep
	beginErr, commitErr error
	began, committed    bool
}
type scriptConnector struct{ script *sqlScript }
type scriptDriver struct{}
type scriptConn struct{ script *sqlScript }
type scriptRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}
type repositoryDB struct {
	db  *gorm.DB
	sql *sql.DB
}

func (scriptDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }
func (c scriptConnector) Driver() driver.Driver       { return scriptDriver{} }
func (c scriptConnector) Connect(context.Context) (driver.Conn, error) {
	return &scriptConn{c.script}, nil
}
func (c *scriptConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (c *scriptConn) Close() error              { return nil }
func (c *scriptConn) Begin() (driver.Tx, error) { c.script.began = true; return c, c.script.beginErr }
func (c *scriptConn) Commit() error             { c.script.committed = true; return c.script.commitErr }
func (c *scriptConn) Rollback() error           { return nil }
func (c *scriptConn) next(query string, args []driver.NamedValue) sqlStep {
	c.script.t.Helper()
	if len(c.script.steps) == 0 {
		c.script.t.Errorf("unexpected SQL: %s", query)
		return sqlStep{err: errors.New("unexpected SQL")}
	}
	s := c.script.steps[0]
	c.script.steps = c.script.steps[1:]
	if s.pattern != "" {
		assert.Regexp(c.script.t, s.pattern, query)
	} else {
		assert.Equal(c.script.t, s.query, query)
	}
	if s.args != nil {
		values := make([]any, len(args))
		for i, a := range args {
			values[i] = a.Value
		}
		assert.Equal(c.script.t, s.args, values)
	}
	if s.check != nil {
		s.check(args)
	}
	return s
}
func (c *scriptConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	s := c.next(q, args)
	return &scriptRows{columns: s.columns, rows: s.rows}, s.err
}
func (c *scriptConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	s := c.next(q, args)
	return scriptResult(s.affected), s.err
}
func (r *scriptRows) Columns() []string { return r.columns }
func (r *scriptRows) Close() error      { return nil }
func (r *scriptRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}
func (d *repositoryDB) Database() *gorm.DB             { return d.db }
func (d *repositoryDB) Close() error                   { return d.sql.Close() }
func (d *repositoryDB) Ping(ctx context.Context) error { return d.sql.PingContext(ctx) }

func newRepositoryDB(t *testing.T, steps ...sqlStep) (*repositoryDB, *sqlScript) {
	t.Helper()
	script := &sqlScript{t: t, steps: steps}
	sqldb := sql.OpenDB(scriptConnector{script})
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqldb, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.Empty(t, script.steps, "all expected SQL must execute")
		require.NoError(t, sqldb.Close())
	})
	return &repositoryDB{db, sqldb}, script
}

type scriptResult int64

func (r scriptResult) LastInsertId() (int64, error) { return 1, nil }
func (r scriptResult) RowsAffected() (int64, error) { return int64(r), nil }
