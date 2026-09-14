package service

//go:generate mockgen -destination=mocks/service_mock.go -package=mocks github.com/vmindtech/vke/internal/service IAppService,IClusterService,IComputeService,INetworkService,ILoadbalancerService,ICloudflareService,IIdentityService,INodeGroupsService
//go:generate mockgen -destination=service_mocks_test.go -package=service github.com/vmindtech/vke/internal/service IComputeService,INetworkService,ILoadbalancerService,ICloudflareService,IIdentityService,INodeGroupsService
