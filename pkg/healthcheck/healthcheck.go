package healthcheck

import (
	"context"
	"time"

	"github.com/vmindtech/vke/pkg/mysqldb"
)

var healthCheck *config

const (
	mysqlConnTimeout = 20 * time.Second
)

type config struct {
	serverUp bool
	mysql    mysqldb.IMysqlInstance
}

func InitHealthCheck(mi mysqldb.IMysqlInstance) {
	healthCheck = &config{
		serverUp: true,
		mysql:    mi,
	}
}

func Readiness() map[string]bool {
	mysqlCtx, mysqlCtxCancel := context.WithTimeout(context.Background(), mysqlConnTimeout)
	defer mysqlCtxCancel()

	mysqlErr := healthCheck.mysql.Ping(mysqlCtx)

	return map[string]bool{
		"mysql": mysqlErr == nil,
	}
}

func Liveness() bool {
	return healthCheck.serverUp
}

func ServerShutdown() {
	healthCheck.serverUp = false
}

func IsConnectionSuccessful(conn map[string]bool) bool {
	for _, status := range conn {
		if !status {
			return false
		}
	}

	return true
}
