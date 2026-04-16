package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"os/signal"
	"sync"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"

	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/repository"
	"github.com/vmindtech/vke/internal/service"
	"github.com/vmindtech/vke/pkg/constants"
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
	conc := rmqCfg.Concurrency
	if conc <= 0 {
		conc = 8
	}
	// Prefetch must allow multiple unacked deliveries so another cluster's job can run while one delete is slow.
	prefetch := conc
	if prefetch < 8 {
		prefetch = 8
	}
	deliveries, closeFn, err := rmq.Consume(ctx, "vke-worker", prefetch)
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

	sem := make(chan struct{}, conc)
	var ackMu sync.Mutex // amqp Channel is not safe for concurrent Ack

	logger.WithFields(logrus.Fields{
		"queue":       rmqCfg.QueueName,
		"concurrency": conc,
		"prefetch":    prefetch,
	}).Info("worker started")

	for d := range deliveries {
		d := d
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()

			requeue, err := handleDelivery(ctx, logger, iRepository, iClusterService, d)
			ackMu.Lock()
			defer ackMu.Unlock()
			if err != nil {
				// Errors are logged inside handleDelivery with jobUUID + clusterUUID when known.
				if !requeue {
					_ = d.Ack(false)
					return
				}
				_ = rmq.PublishJSONWithDelay(ctx, json.RawMessage(d.Body), 5*time.Second)
				_ = d.Ack(false)
				return
			}
			_ = d.Ack(false)
		}()
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
		logger.WithError(err).Error("job message unmarshal failed")
		return false, err
	}
	if msg.JobUUID == "" {
		return false, nil
	}

	job, err := repo.Jobs().GetJobByUUID(ctx, msg.JobUUID)
	if err != nil {
		logger.WithError(err).WithField("jobUUID", msg.JobUUID).Error("get job by UUID failed")
		return true, err
	}

	var createPayload request.CreateClusterJobPayload
	var deletePayload request.DeleteClusterJobPayload
	clusterLogID := strings.TrimSpace(job.ClusterUUID)

	switch job.JobType {
	case "CLUSTER_CREATE":
		if err := json.Unmarshal(job.Payload, &createPayload); err != nil {
			logger.WithError(err).WithField("jobUUID", job.JobUUID).Error("cluster create job payload unmarshal failed")
			return true, err
		}
		if clusterLogID == "" {
			clusterLogID = strings.TrimSpace(createPayload.ClusterUUID)
		}
	case "CLUSTER_DELETE":
		if err := json.Unmarshal(job.Payload, &deletePayload); err != nil {
			logger.WithError(err).WithField("jobUUID", job.JobUUID).Error("cluster delete job payload unmarshal failed")
			return true, err
		}
		if clusterLogID == "" {
			clusterLogID = strings.TrimSpace(deletePayload.ClusterID)
		}
	}

	entry := logger.WithFields(logrus.Fields{
		"jobUUID": job.JobUUID,
		"type":    job.JobType,
	})
	if clusterLogID != "" {
		entry = entry.WithField("clusterUUID", clusterLogID)
	}

	// Duplicate queue deliveries after a real success: skip only when cluster is in a terminal good state.
	// If job row is SUCCEEDED but cluster is still Creating (inconsistent / old bug), RunCreateCluster must run to reconcile.
	if job.Status == "SUCCEEDED" {
		switch job.JobType {
		case "CLUSTER_CREATE":
			cl, cerr := repo.Cluster().GetClusterByUUID(ctx, createPayload.ClusterUUID)
			if cerr == nil && (cl.ClusterStatus == service.ActiveClusterStatus || cl.ClusterStatus == service.DeletedClusterStatus) {
				entry.WithField("clusterStatus", cl.ClusterStatus).Debug("create job succeeded and cluster terminal; ack stale queue message")
				return false, nil
			}
			if cerr != nil {
				entry.WithError(cerr).Warn("could not load cluster for duplicate check; reconciling create")
			} else {
				entry.WithFields(logrus.Fields{"clusterStatus": cl.ClusterStatus}).Info("job marked succeeded but cluster not terminal; reconciling create")
			}
		case "CLUSTER_DELETE":
			cl, cerr := repo.Cluster().GetClusterByUUID(ctx, deletePayload.ClusterID)
			if cerr != nil {
				entry.WithError(cerr).Warn("could not load cluster for delete duplicate check; reconciling destroy")
				break
			}
			if cl.ClusterStatus == service.DeletedClusterStatus || cl.DeleteState == constants.DeleteStateCompleted {
				entry.WithField("clusterStatus", cl.ClusterStatus).Info("delete job succeeded and cluster removed; ack duplicate queue message")
				return false, nil
			}
			entry.WithFields(logrus.Fields{"clusterStatus": cl.ClusterStatus, "deleteState": cl.DeleteState}).Info("job marked succeeded but cluster not fully deleted; reconciling destroy")
		default:
			entry.Info("job already succeeded; ack duplicate queue message")
			return false, nil
		}
	}

	// mark running (best effort)
	_ = repo.Jobs().MarkJobStarted(ctx, job.JobUUID, "vke-worker")

	defer func() {
		if r := recover(); r != nil {
			_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, "panic", time.Now().Add(30*time.Second), job.Attempts+1)
			entry.WithField("panic", r).Error("job panic")
		}
	}()

	switch job.JobType {
	case "CLUSTER_CREATE":
		// RunCreateCluster: create_state through master-1 + infra, then wait for kubeconfig in DB, then masters 2–3 + workers + LB; cluster becomes Active at the end.
		if err := clusterSvc.RunCreateCluster(ctx, createPayload.ClusterUUID); err != nil {
			if errors.Is(err, service.ErrStaleCreateClusterJob) {
				_ = repo.Jobs().MarkJobSucceeded(ctx, job.JobUUID)
				entry.WithError(err).Warn("stale create queue message (cluster gone); ack without retry")
				return false, nil
			}
			// Kubeconfig never arrived: retrying the whole job every N minutes is harmful (log spam, no benefit).
			if errors.Is(err, service.ErrKubeconfigTimeout) {
				_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), time.Now(), job.MaxAttempts)
				entry.WithError(err).Error("cluster create: kubeconfig wait exceeded; job failed without retry")
				return false, err
			}
			attempts := job.Attempts + 1
			backoff := time.Duration(attempts*attempts) * 10 * time.Second
			next := time.Now().Add(backoff)
			if attempts >= job.MaxAttempts {
				_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), next, attempts)
				entry.WithError(err).WithField("attempts", attempts).Error("cluster create job failed permanently")
				return false, err
			}
			_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), next, attempts)
			entry.WithError(err).WithFields(logrus.Fields{"attempts": attempts, "nextRunAt": next}).Warn("cluster create job failed; will retry")
			return true, err
		}
		_ = repo.Jobs().MarkJobSucceeded(ctx, job.JobUUID)
		entry.Info("cluster create job succeeded")
		return false, nil

	case "CLUSTER_DELETE":
		if err := clusterSvc.DestroyCluster(ctx, deletePayload.AuthToken, deletePayload.ClusterID); err != nil {
			attempts := job.Attempts + 1
			backoff := time.Duration(attempts*attempts) * 10 * time.Second
			next := time.Now().Add(backoff)
			if attempts >= job.MaxAttempts {
				_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), next, attempts)
				entry.WithError(err).WithField("attempts", attempts).Error("cluster delete job failed permanently")
				return false, err
			}
			_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), next, attempts)
			entry.WithError(err).WithFields(logrus.Fields{"attempts": attempts, "nextRunAt": next}).Warn("cluster delete job failed; will retry")
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

