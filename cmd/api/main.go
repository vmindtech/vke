package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/pkg/healthcheck"
	"github.com/vmindtech/vke/pkg/localizer"
	"github.com/vmindtech/vke/pkg/logging"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

func main() {
	configureManager := config.NewConfigureManager()
	logger := logging.NewLogger(logging.Config{
		Service: logging.ServiceConfig{
			Env:     configureManager.GetWebConfig().Env,
			AppName: configureManager.GetWebConfig().AppName,
		},
	})

	logger.Info("starting app")

	mysqlInstance, mysqlErr := mysqldb.InitMysqlDB(configureManager.GetMysqlDBConfig().URL)
	if mysqlErr != nil {
		logger.Fatalf("connection: mysqldb %v", mysqlErr)
	}
	defer mysqlInstance.Close()

	app := initApplication(&application{
		Logger: logger,
		LanguageBundle: localizer.InitLocalizer(
			configureManager.GetLanguageConfig().Default, configureManager.GetLanguageConfig().Languages,
		),
		MysqlInstance: mysqlInstance,
	})

	go func() {
		healthcheck.InitHealthCheck(mysqlInstance)

		if serveErr := app.Listen(fmt.Sprintf(":%s", configureManager.GetWebConfig().Port)); serveErr != nil {
			logger.Fatalf("connection: web server %v", serveErr)
		}
	}()

	// Wait for gracefully shutdown (Interrupt)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	<-c

	healthcheck.ServerShutdown()
	if shutdownErr := app.Shutdown(); shutdownErr != nil {
		logger.Error(shutdownErr)
	}
}
