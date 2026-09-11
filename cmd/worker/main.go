package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"

	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/repository"
	"github.com/vmindtech/vke/internal/service"
	"github.com/vmindtech/vke/pkg/constants"
	"github.com/vmindtech/vke/pkg/localizer"
	"github.com/vmindtech/vke/pkg/logging"
	"github.com/vmindtech/vke/pkg/mysqldb"
	"github.com/vmindtech/vke/pkg/queue"
	"github.com/vmindtech/vke/pkg/utils"
)

// A running job refreshes locked_at every jobLockHeartbeat; the lock is considered stale after
// jobLockStaleAfter without a heartbeat (worker crashed / force-killed), so retries resume within
// minutes instead of waiting out the whole job runtime.
const (
	jobLockHeartbeat  = 30 * time.Second
	jobLockStaleAfter = 3 * time.Minute
)

// workerID identifies this process in jobs.locked_by so heartbeats only refresh locks we own.
func workerID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "vke-worker"
	}
	id := fmt.Sprintf("%s-%d", host, os.Getpid())
	if len(id) > 64 { // jobs.locked_by is varchar(64)
		id = id[len(id)-64:]
	}
	return id
}

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

	// consumeCtx stops new deliveries on the first signal; jobCtx lets in-flight jobs finish
	// (drained below) and is only cancelled by a second signal (force quit).
	consumeCtx, stopConsuming := context.WithCancel(context.Background())
	defer stopConsuming()
	jobCtx, cancelJobs := context.WithCancel(context.Background())
	defer cancelJobs()

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
	deliveries, closeFn, err := rmq.Consume(consumeCtx, "vke-worker", prefetch)
	if err != nil {
		logger.WithError(err).Fatal("failed to start consumer")
	}
	defer func() { _ = closeFn() }()

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		<-c
		logger.Info("shutdown signal received; finishing in-flight jobs (send again to force quit)")
		stopConsuming()
		<-c
		cancelJobs()
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

			// Errors are logged inside handleDelivery with jobUUID + clusterUUID when known;
			// terminal outcomes (success or permanent failure) are already persisted there.
			requeue, retryDelay, _ := handleDelivery(jobCtx, logger, iRepository, iClusterService, d)
			ackMu.Lock()
			defer ackMu.Unlock()
			if requeue {
				if retryDelay <= 0 {
					retryDelay = 5 * time.Second
				}
				// Republish first; only drop the original once the retry copy is safely in the broker.
				if pubErr := rmq.PublishJSONWithDelay(context.Background(), json.RawMessage(d.Body), retryDelay); pubErr != nil {
					logger.WithError(pubErr).Error("failed to republish job for retry; nacking for broker redelivery")
					_ = d.Nack(false, true)
					return
				}
				_ = d.Ack(false)
				return
			}
			_ = d.Ack(false)
		}()
	}

	// Channel closed (SIGINT/SIGTERM): wait for in-flight jobs so create/delete steps are not killed mid-way.
	logger.Info("shutting down; waiting for in-flight jobs")
	for i := 0; i < conc; i++ {
		sem <- struct{}{}
	}
	logger.Info("worker stopped")
}

// handleDelivery processes one queue message. It returns requeue=true with a retry delay when the
// message must be republished (transient failure or lock contention); err is informational only.
func handleDelivery(
	ctx context.Context,
	logger *logrus.Logger,
	repo repository.IRepository,
	clusterSvc service.IClusterService,
	d amqp.Delivery,
) (bool, time.Duration, error) {
	var msg request.JobMessage
	if err := json.Unmarshal(d.Body, &msg); err != nil {
		logger.WithError(err).Error("job message unmarshal failed")
		return false, 0, err
	}
	if msg.JobUUID == "" {
		return false, 0, nil
	}

	job, err := repo.Jobs().GetJobByUUID(ctx, msg.JobUUID)
	if err != nil {
		logger.WithError(err).WithField("jobUUID", msg.JobUUID).Error("get job by UUID failed")
		return true, 5 * time.Second, err
	}

	var createPayload request.CreateClusterJobPayload
	var deletePayload request.DeleteClusterJobPayload
	clusterLogID := strings.TrimSpace(job.ClusterUUID)

	switch job.JobType {
	case "CLUSTER_CREATE":
		if err := json.Unmarshal(job.Payload, &createPayload); err != nil {
			logger.WithError(err).WithField("jobUUID", job.JobUUID).Error("cluster create job payload unmarshal failed")
			return true, 5 * time.Second, err
		}
		if clusterLogID == "" {
			clusterLogID = strings.TrimSpace(createPayload.ClusterUUID)
		}
	case "CLUSTER_DELETE":
		if err := json.Unmarshal(job.Payload, &deletePayload); err != nil {
			logger.WithError(err).WithField("jobUUID", job.JobUUID).Error("cluster delete job payload unmarshal failed")
			return true, 5 * time.Second, err
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
				return false, 0, nil
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
				return false, 0, nil
			}
			entry.WithFields(logrus.Fields{"clusterStatus": cl.ClusterStatus, "deleteState": cl.DeleteState}).Info("job marked succeeded but cluster not fully deleted; reconciling RunDestroyCluster")
		default:
			entry.Info("job already succeeded; ack duplicate queue message")
			return false, 0, nil
		}
	}

	// Claim the job so duplicate deliveries (e.g. after a connection drop) cannot run the same
	// cluster create/delete concurrently. The stale window covers worker crashes that left RUNNING rows.
	wid := workerID()
	acquired, lockErr := repo.Jobs().TryMarkJobStarted(ctx, job.JobUUID, wid, jobLockStaleAfter)
	if lockErr != nil {
		entry.WithError(lockErr).Error("failed to claim job lock")
		return true, 5 * time.Second, lockErr
	}
	if !acquired {
		entry.Info("job already running on another worker; requeueing duplicate delivery")
		return true, time.Minute, nil
	}

	// Heartbeat keeps the lock fresh while the job runs; it stops (and the lock goes stale) when
	// this process exits for any reason, including SIGKILL.
	hbCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	go func() {
		t := time.NewTicker(jobLockHeartbeat)
		defer t.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-t.C:
				if herr := repo.Jobs().TouchJobLock(hbCtx, job.JobUUID, wid); herr != nil {
					entry.WithError(herr).Warn("job lock heartbeat failed")
				}
			}
		}
	}()

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
				return false, 0, nil
			}
			// Kubeconfig never arrived: retrying the whole job every N minutes is harmful (log spam, no benefit).
			if errors.Is(err, service.ErrKubeconfigTimeout) {
				_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), time.Now(), job.MaxAttempts)
				entry.WithError(err).Error("cluster create: kubeconfig wait exceeded; job failed without retry")
				return false, 0, err
			}
			attempts := job.Attempts + 1
			backoff := retryBackoff(attempts)
			next := time.Now().Add(backoff)
			if attempts >= job.MaxAttempts {
				_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), next, attempts)
				entry.WithError(err).WithField("attempts", attempts).Error("cluster create job failed permanently")
				return false, 0, err
			}
			_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), next, attempts)
			entry.WithError(err).WithFields(logrus.Fields{"attempts": attempts, "nextRunAt": next}).Warn("cluster create job failed; will retry")
			return true, backoff, err
		}
		_ = repo.Jobs().MarkJobSucceeded(ctx, job.JobUUID)
		entry.Info("cluster create job succeeded")
		return false, 0, nil

	case "CLUSTER_DELETE":
		authToken, tokenErr := deleteJobAuthToken(deletePayload)
		if tokenErr != nil {
			// Destroy can still proceed via the cluster's application credential; the raw token is only a fallback.
			entry.WithError(tokenErr).Warn("could not recover auth token from delete job payload; relying on application credential")
		}
		// RunDestroyCluster: delete_state machine until COMPLETED; idempotent for queue retries.
		if err := clusterSvc.RunDestroyCluster(ctx, authToken, deletePayload.ClusterID); err != nil {
			attempts := job.Attempts + 1
			if isDeleteJobPermanent(err, attempts, job.MaxAttempts) {
				_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), time.Now(), attempts)
				persistDeleteJobFailure(ctx, entry, repo, clusterLogID)
				entry.WithError(err).WithFields(logrus.Fields{
					"attempts":  attempts,
					"retryable": false,
					"outcome":   "failed_permanently",
				}).Error("cluster delete job failed permanently")
				return false, 0, err
			}
			backoff := retryBackoff(attempts)
			next := time.Now().Add(backoff)
			_ = repo.Jobs().MarkJobFailed(ctx, job.JobUUID, err.Error(), next, attempts)
			entry.WithError(err).WithFields(logrus.Fields{
				"attempts":  attempts,
				"nextRunAt": next,
				"retryable": true,
				"outcome":   "retrying",
			}).Info("cluster delete job failed; will retry")
			return true, backoff, err
		}
		_ = repo.Jobs().MarkJobSucceeded(ctx, job.JobUUID)
		entry.Info("cluster delete job succeeded")
		return false, 0, nil
	default:
		_ = repo.Jobs().MarkJobSucceeded(ctx, job.JobUUID)
		entry.Warn("unknown job type; marked succeeded")
		return false, 0, nil
	}
}

// retryBackoff is the quadratic delay between job attempts; it is both persisted to jobs.next_run_at
// and used as the RabbitMQ retry-queue TTL so the two stay consistent.
func retryBackoff(attempts int) time.Duration {
	return time.Duration(attempts*attempts) * 10 * time.Second
}

func isDeleteJobPermanent(err error, attempts, maxAttempts int) bool {
	return errors.Is(err, service.ErrDestroyClusterPermanent) || attempts >= maxAttempts
}

func persistDeleteJobFailure(ctx context.Context, logger *logrus.Entry, repo repository.IRepository, clusterID string) {
	if strings.TrimSpace(clusterID) == "" {
		return
	}
	recErr := repo.Error().CreateError(ctx, &model.Error{
		ClusterUUID:  clusterID,
		ErrorMessage: constants.GetErrorMessage(constants.ErrClusterDeleteFailed, "cluster_deletion", clusterID),
		CreatedAt:    time.Now(),
	})
	if recErr != nil {
		logger.WithError(recErr).Warn("failed to persist cluster delete failure")
	}
}

// deleteJobAuthToken returns the fallback OpenStack token from a delete payload, decrypting
// auth_token_enc when present (plain auth_token only exists in jobs enqueued by older builds).
func deleteJobAuthToken(p request.DeleteClusterJobPayload) (string, error) {
	if p.AuthTokenEnc == "" {
		return p.AuthToken, nil
	}
	encKey := config.GlobalConfig.GetEncryptionConfig().Key
	if encKey == "" {
		return "", fmt.Errorf("VKE_ENCRYPTION_KEY must be set to decrypt delete job auth token")
	}
	return utils.DecryptAESGCM(utils.DeriveKeySHA256(encKey), p.AuthTokenEnc)
}
