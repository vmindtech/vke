package main

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"

	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/repository"
	"github.com/vmindtech/vke/internal/service"
	"github.com/vmindtech/vke/pkg/localizer"
	"github.com/vmindtech/vke/pkg/logging"
	"github.com/vmindtech/vke/pkg/mysqldb"
	"github.com/vmindtech/vke/pkg/queue"
)

func main() {
	configureManager := config.NewConfigureManager()

	var logstashConfig *logging.LogstashConfig
	if configureManager.GetLogstashConfig().Host != "" && configureManager.GetLogstashConfig().Port > 0 {
		logstashConfig = &logging.LogstashConfig{
			Host: configureManager.GetLogstashConfig().Host,
			Port: configureManager.GetLogstashConfig().Port,
		}
	}

	logger := logging.NewLogger(logging.Config{
		Service: logging.ServiceConfig{
			Env:     configureManager.GetWebConfig().Env,
			AppName: configureManager.GetWebConfig().AppName + "-worker",
		},
		Logstash: logstashConfig,
	})
	_ = localizer.InitLocalizer(
		configureManager.GetLanguageConfig().Default, configureManager.GetLanguageConfig().Languages,
	)

	mysqlInstance, mysqlErr := mysqldb.InitMysqlDB(configureManager.GetMysqlDBConfig().URL)
	if mysqlErr != nil {
		logger.Fatalf("connection: mysqldb %v", mysqlErr)
	}
	defer mysqlInstance.Close()

	// repositories
	iClusterRepository := repository.NewClusterRepository(mysqlInstance)
	iAuditRepository := repository.NewAuditLogRepository(mysqlInstance)
	iKubeConfigRepository := repository.NewKubeconfigRepository(mysqlInstance)
	iNodeGroupsRepository := repository.NewNodeGroupsRepository(mysqlInstance)
	iResourcesRepository := repository.NewResourcesRepository(mysqlInstance)
	iErrorRepository := repository.NewErrorRepository(mysqlInstance)
	iJobsRepository := repository.NewJobsRepository(mysqlInstance)
	iRepository := repository.NewRepository(mysqlInstance, iClusterRepository, iAuditRepository, iKubeConfigRepository, iNodeGroupsRepository, iResourcesRepository, iErrorRepository, iJobsRepository)

	// services
	iIdentityService := service.NewIdentityService(logger)
	iNetworkService := service.NewNetworkService(logger)
	iCloudflareService := service.NewCloudflareService(logger)
	iLoadbalancerService := service.NewLoadbalancerService(logger)
	iComputeService := service.NewComputeService(logger, iIdentityService, iRepository)
	iNodeGroupsService := service.NewNodeGroupsService(logger, iRepository, iIdentityService, iComputeService, iNetworkService)
	iClusterService := service.NewClusterService(logger, iCloudflareService, iLoadbalancerService, iNetworkService, iComputeService, iNodeGroupsService, iIdentityService, iRepository)

	rmqCfg := configureManager.GetRabbitMQConfig()
	if rmqCfg.URL == "" || rmqCfg.QueueName == "" {
		logger.Fatal("RABBITMQ_URL and RABBITMQ_QUEUE must be set")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rmq := queue.NewRabbitMQ(rmqCfg.URL, rmqCfg.QueueName)
	deliveries, closeFn, err := rmq.Consume(ctx, "vke-worker", 1)
	if err != nil {
		logger.WithError(err).Fatal("failed to start consumer")
	}
	defer func() { _ = closeFn() }()

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		<-c
		cancel()
	}()

	logger.WithFields(logrus.Fields{
		"queue": rmqCfg.QueueName,
	}).Info("worker started")

	for d := range deliveries {
		requeue, err := handleDelivery(ctx, logger, iRepository, iClusterService, d)
		if err != nil {
			logger.WithError(err).Error("job failed")
			// if not requeueing, ack to drop (job status is FAILED)
			if !requeue {
				_ = d.Ack(false)
				continue
			}
			// move message to retry queue with TTL, then ack to avoid hot-loop
			_ = rmq.PublishJSONWithDelay(ctx, json.RawMessage(d.Body), 5*time.Second)
			_ = d.Ack(false)
			continue
		}
		_ = d.Ack(false)
	}
}

func handleDelivery(
	ctx context.Context,
	logger *logrus.Logger,
	repo repository.IRepository,
	clusterSvc service.IClusterService,
	d amqp.Delivery,
) (bool, error) {
	var msg request.JobMessage
	if err := json.Unmarshal(d.Body, &msg); err != nil {
		return false, err
	}
	if msg.JobUUID == "" {
		return false, nil
	}

	job, err := repo.Jobs().GetJobByUUID(ctx, msg.JobUUID)
	if err != nil {
		return true, err
	}

	// mark running (best effort)
	_ = repo.Jobs().MarkJobStarted(ctx, job.JobUUID, "vke-worker")

	entry := logger.WithFields(logrus.Fields{
		"jobUUID": job.JobUUID,
		"type":    job.JobType,
	})

	defer func() {
		if r := recover(); r != nil {
			_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, "panic", time.Now().Add(30*time.Second), job.Attempts+1)
		}
	}()

	switch job.JobType {
	case "CLUSTER_CREATE":
		var payload request.CreateClusterJobPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return true, err
		}
		entry = entry.WithField("clusterUUID", payload.ClusterUUID)
		if err := clusterSvc.RunCreateCluster(ctx, payload.ClusterUUID); err != nil {
			attempts := job.Attempts + 1
			backoff := time.Duration(attempts*attempts) * 10 * time.Second
			next := time.Now().Add(backoff)
			if attempts >= job.MaxAttempts {
				_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), next, attempts)
				entry.WithField("attempts", attempts).Error("cluster create job failed permanently")
				return false, err
			}
			_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), next, attempts)
			entry.WithFields(logrus.Fields{"attempts": attempts, "nextRunAt": next}).Warn("cluster create job failed; will retry")
			return true, err
		}
		_ = repo.Jobs().MarkJobSucceeded(ctx, job.JobUUID)
		entry.Info("cluster create job succeeded")
		return false, nil

	case "CLUSTER_DELETE":
		var payload request.DeleteClusterJobPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return true, err
		}
		entry = entry.WithField("clusterUUID", payload.ClusterID)
		if err := clusterSvc.DestroyCluster(ctx, payload.AuthToken, payload.ClusterID); err != nil {
			attempts := job.Attempts + 1
			backoff := time.Duration(attempts*attempts) * 10 * time.Second
			next := time.Now().Add(backoff)
			if attempts >= job.MaxAttempts {
				_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), next, attempts)
				entry.WithField("attempts", attempts).Error("cluster delete job failed permanently")
				return false, err
			}
			_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), next, attempts)
			entry.WithFields(logrus.Fields{"attempts": attempts, "nextRunAt": next}).Warn("cluster delete job failed; will retry")
			return true, err
		}
		_ = repo.Jobs().MarkJobSucceeded(ctx, job.JobUUID)
		entry.Info("cluster delete job succeeded")
		return false, nil
	default:
		_ = repo.Jobs().MarkJobSucceeded(ctx, job.JobUUID)
		entry.Warn("unknown job type; marked succeeded")
		return false, nil
	}
}

