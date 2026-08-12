package repository

//go:generate mockgen -destination=mocks/repository_mock.go -package=mocks github.com/vmindtech/vke/internal/repository IRepository,IClusterRepository,IJobsRepository,IAuditLogRepository,IKubeconfigRepository,INodeGroupsRepository,IResourcesRepository,IErrorRepository
