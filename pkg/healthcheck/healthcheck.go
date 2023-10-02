package healthcheck

var healthCheck *config

type config struct {
	serverUp bool
}

func InitHealthCheck() {
	healthCheck = &config{
		serverUp: true,
	}
}

func Readiness() bool {
	return healthCheck.serverUp
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
