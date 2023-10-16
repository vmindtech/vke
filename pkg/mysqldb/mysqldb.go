package mysqldb

//nolint:revive
import (
	"context"
	"database/sql"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

const (
	dbMaxIdleConn     = 10
	dbMaxOpenConn     = 100
	dbConnMaxLifetime = time.Hour

	mysqlDriverName = "mysql"
)

type IMysqlInstance interface {
	Database() *gorm.DB
	Close() error
	Ping(ctx context.Context) error
}

type mysqlInstance struct {
	db    *gorm.DB
	sqlDB *sql.DB
}

func InitMysqlDB(url string) (IMysqlInstance, error) {
	sqlDB, err := sql.Open(getDriverName(), url)
	if err != nil {
		return nil, err
	}

	if err = sqlDB.Ping(); err != nil {
		return nil, err
	}

	sqlDB.SetConnMaxLifetime(dbConnMaxLifetime)
	sqlDB.SetMaxOpenConns(dbMaxOpenConn)
	sqlDB.SetMaxIdleConns(dbMaxIdleConn)

	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB}))
	if err != nil {
		return nil, err
	}

	return &mysqlInstance{
		db:    db,
		sqlDB: sqlDB,
	}, nil
}

func (m mysqlInstance) Database() *gorm.DB {
	return m.db
}

func (m mysqlInstance) Close() error {
	return m.sqlDB.Close()
}

func (m mysqlInstance) Ping(ctx context.Context) error {
	return m.sqlDB.PingContext(ctx)
}

func getDriverName() string {
	return mysqlDriverName
}
