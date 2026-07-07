package service

//go:generate mockgen -destination=mocks/service_mock.go -package=mocks github.com/vmindtech/vke/internal/service IAppService,IClusterService,IComputeService,INetworkService,ILoadbalancerService,ICloudflareService,IIdentityService,INodeGroupsService
