package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/repository"
)

type IClusterService interface {
	CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest, clUUID chan string)
	GetCluster(ctx context.Context, authToken, clusterID string) (resource.GetClusterResponse, error)
	GetClusterDetails(ctx context.Context, authToken, clusterID string) (resource.GetClusterDetailsResponse, error)
	GetClustersByProjectId(ctx context.Context, authToken, projectID string) ([]resource.GetClusterResponse, error)
	DestroyCluster(ctx context.Context, authToken, clusterID string, clUUID chan string)
	GetKubeConfig(ctx context.Context, authToken, clusterID string) (resource.GetKubeConfigResponse, error)
	CreateKubeConfig(ctx context.Context, authToken string, req request.CreateKubeconfigRequest) (resource.CreateKubeconfigResponse, error)
	CreateAuditLog(ctx context.Context, clusterUUID, projectUUID, event string) error
}

type clusterService struct {
	cloudflareService   ICloudflareService
	loadbalancerService ILoadbalancerService
	networkService      INetworkService
	computeService      IComputeService
	nodeGroupsService   INodeGroupsService
	logger              *logrus.Logger
	identityService     IIdentityService
	repository          repository.IRepository
}

func NewClusterService(l *logrus.Logger, cf ICloudflareService, lbc ILoadbalancerService, ns INetworkService, cs IComputeService, ng INodeGroupsService, i IIdentityService, r repository.IRepository) IClusterService {
	return &clusterService{
		cloudflareService:   cf,
		loadbalancerService: lbc,
		networkService:      ns,
		computeService:      cs,
		nodeGroupsService:   ng,
		logger:              l,
		identityService:     i,
		repository:          r,
	}
}

const (
	ActiveClusterStatus   = "Active"
	CreatingClusterStatus = "Creating"
	UpdatingClusterStatus = "Updating"
	DeletingClusterStatus = "Deleting"
	DeletedClusterStatus  = "Deleted"
	ErrorClusterStatus    = "Error"
)

const (
	LoadBalancerStatusActive  = "ACTIVE"
	LoadBalancerStatusDeleted = "DELETED"
	LoadBalancerStatusError   = "ERROR"
)

const (
	NodeGroupCreatingStatus = "Creating"
	NodeGroupActiveStatus   = "Active"
	NodeGroupUpdatingStatus = "Updating"
	NodeGroupDeletedStatus  = "Deleted"
)

const (
	NodeGroupMasterType = "master"
	NodeGroupWorkerType = "worker"
)

const (
	MasterServerType = "server"
	WorkerServerType = "agent"
)

const (
	loadBalancerPath       = "v2/lbaas/loadbalancers"
	listenersPath          = "v2/lbaas/listeners"
	subnetsPath            = "v2.0/subnets"
	createMemberPath       = "v2/lbaas/pools"
	networkPort            = "v2.0/ports"
	securityGroupPath      = "v2.0/security-groups"
	SecurityGroupRulesPath = "v2.0/security-group-rules"
	ListenerPoolPath       = "v2/lbaas/pools"
	healthMonitorPath      = "v2/lbaas/healthmonitors"
	computePath            = "v2.1/servers"
	flavorPath             = "v2.1/flavors"
	projectPath            = "v3/projects"
	serverGroupPath        = "v2.1/os-server-groups"
	amphoraePath           = "v2/octavia/amphorae"
	floatingIPPath         = "v2.0/floatingips"
	listernersPath         = "v2/lbaas/listeners"
	osInterfacePath        = "os-interface"
	tokenPath              = "v3/auth/tokens"
)

const (
	cloudflareEndpoint = "https://api.cloudflare.com/client/v4/zones"
)

func (c *clusterService) CreateAuditLog(ctx context.Context, clusterUUID, projectUUID, event string) error {
	auditLog := &model.AuditLog{
		ClusterUUID: clusterUUID,
		ProjectUUID: projectUUID,
		Event:       event,
		CreateDate:  time.Now(),
	}

	return c.repository.AuditLog().CreateAuditLog(ctx, auditLog)
}

func (c *clusterService) CheckKubeConfig(ctx context.Context, clusterUUID string) error {
	waitIterator := 0
	waitSeconds := 10
	for {
		if waitIterator < 6 {
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			c.logger.WithFields(logrus.Fields{
				"ClusterUUID": clusterUUID,
				"Waited":      waitSeconds,
			}).Info("Waiting for Kubeconfig to be ACTIVE")
			waitIterator++
		} else {
			err := fmt.Errorf("failed to send kubeconfig for ClusterUUID: %s", clusterUUID)
			return err
		}
		_, err := c.repository.Kubeconfig().GetKubeconfigByUUID(ctx, clusterUUID)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"ClusterUUID": clusterUUID,
			}).Error("failed to get kubeconfig")
		} else {
			break
		}
	}
	return nil
}
func (c *clusterService) CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest, clUUID chan string) {
	clusterUUID := uuid.New().String()

	clUUID <- clusterUUID

	subnetIdsJSON, err := json.Marshal(req.SubnetIDs)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to marshal subnet ids")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}
		return
	}
	createApplicationCredentialReq, err := c.identityService.CreateApplicationCredential(ctx, clusterUUID, authToken)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create application credential")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}
		return
	}
	clusterModel := &model.Cluster{
		ClusterUUID:                  clusterUUID,
		ClusterName:                  req.ClusterName,
		ClusterCreateDate:            time.Now(),
		ClusterVersion:               req.KubernetesVersion,
		ClusterStatus:                CreatingClusterStatus,
		ClusterProjectUUID:           req.ProjectID,
		ClusterLoadbalancerUUID:      "",
		ClusterRegisterToken:         "",
		ClusterAgentToken:            "",
		ClusterSubnets:               subnetIdsJSON,
		ClusterNodeKeypairName:       req.NodeKeyPairName,
		ClusterAPIAccess:             req.ClusterAPIAccess,
		FloatingIPUUID:               "",
		ClusterSharedSecurityGroup:   "",
		ApplicationCredentialID:      createApplicationCredentialReq.Credential.ID,
		ClusterCertificateExpireDate: time.Now().AddDate(0, 0, 365),
	}

	err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create audit log")
		return
	}

	err = c.repository.Cluster().CreateCluster(ctx, clusterModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create cluster")

		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}
		return
	}

	floatingIPUUID := ""
	// Create Load Balancer for masters
	createLBReq := &request.CreateLoadBalancerRequest{
		LoadBalancer: request.LoadBalancer{
			Name:         fmt.Sprintf("%v-lb", req.ClusterName),
			Description:  fmt.Sprintf("%v-lb", req.ClusterName),
			AdminStateUp: true,
			VIPSubnetID:  req.SubnetIDs[0],
			Provider:     "ovn", //ToDo: get from config
		},
	}

	lbResp, err := c.loadbalancerService.CreateLoadBalancer(ctx, authToken, *createLBReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create load balancer")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	listLBResp, err := c.loadbalancerService.ListLoadBalancer(ctx, authToken, lbResp.LoadBalancer.ID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to list load balancer")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	loadbalancerIP := listLBResp.LoadBalancer.VIPAddress
	// Control plane access type
	if req.ClusterAPIAccess == "public" {
		createFloatingIPreq := &request.CreateFloatingIPRequest{
			FloatingIP: request.FloatingIP{
				FloatingNetworkID: config.GlobalConfig.GetPublicNetworkIDConfig().PublicNetworkID,
				PortID:            listLBResp.LoadBalancer.VipPortID,
			},
		}
		createFloatingIPResponse, err := c.networkService.CreateFloatingIP(ctx, authToken, *createFloatingIPreq)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create floating ip")
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
			}
			return
		}
		loadbalancerIP = createFloatingIPResponse.FloatingIP.FloatingIP
		floatingIPUUID = createFloatingIPResponse.FloatingIP.ID
	}
	// Create security group for master and worker
	createSecurityGroupReq := &request.CreateSecurityGroupRequest{
		SecurityGroup: request.SecurityGroup{
			Name:        fmt.Sprintf("%v-master-sg", req.ClusterName),
			Description: fmt.Sprintf("%v-master-sg", req.ClusterName),
		},
	}

	// create security group for master
	createMasterSecurityResp, err := c.networkService.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create security group")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	// create security group for worker
	createSecurityGroupReq.SecurityGroup.Name = fmt.Sprintf("%v-worker-sg", req.ClusterName)
	createSecurityGroupReq.SecurityGroup.Description = fmt.Sprintf("%v-worker-sg", req.ClusterName)

	createWorkerSecurityResp, err := c.networkService.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create security group")

		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	// create security group for shared
	createSecurityGroupReq.SecurityGroup.Name = fmt.Sprintf("%v-cluster-shared-sg", req.ClusterName)
	createSecurityGroupReq.SecurityGroup.Description = fmt.Sprintf("%v-cluster-shared-sg", req.ClusterName)

	createClusterSharedSecurityResp, err := c.networkService.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create security group")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	ClusterSharedSecurityGroupUUID := createClusterSharedSecurityResp.SecurityGroup.ID

	clusterSubdomainHash := uuid.New().String()
	rke2Token := uuid.New().String()
	rke2AgentToken := uuid.New().String()

	createServerGroupReq := &request.CreateServerGroupRequest{
		ServerGroup: request.ServerGroup{
			Name:   fmt.Sprintf("%v-master-server-group", req.ClusterName),
			Policy: "soft-anti-affinity",
		},
	}
	masterServerGroupResp, err := c.computeService.CreateServerGroup(ctx, authToken, *createServerGroupReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create server group")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	masterNodeGroupModel := &model.NodeGroups{
		ClusterUUID:            clusterUUID,
		NodeGroupUUID:          masterServerGroupResp.ServerGroup.ID,
		NodeGroupName:          fmt.Sprintf("%v-master", req.ClusterName),
		NodeGroupMinSize:       3,
		NodeGroupMaxSize:       3,
		NodeDiskSize:           80,
		NodeFlavorUUID:         req.MasterInstanceFlavorUUID,
		NodeGroupsStatus:       NodeGroupCreatingStatus,
		NodeGroupsType:         NodeGroupMasterType,
		NodeGroupSecurityGroup: createMasterSecurityResp.SecurityGroup.ID,
		IsHidden:               true,
		NodeGroupCreateDate:    time.Now(),
	}

	err = c.repository.NodeGroups().CreateNodeGroups(ctx, masterNodeGroupModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create node groups")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	createServerGroupReq.ServerGroup.Name = fmt.Sprintf("%v-default-worker-server-group", req.ClusterName)
	workerServerGroupResp, err := c.computeService.CreateServerGroup(ctx, authToken, *createServerGroupReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create server group")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	workerNodeGroupModel := &model.NodeGroups{
		ClusterUUID:            clusterUUID,
		NodeGroupUUID:          workerServerGroupResp.ServerGroup.ID,
		NodeGroupName:          clusterModel.ClusterName + "-default-wg",
		NodeGroupMinSize:       req.WorkerNodeGroupMinSize,
		NodeGroupMaxSize:       req.WorkerNodeGroupMaxSize,
		NodeDiskSize:           req.WorkerDiskSizeGB,
		NodeFlavorUUID:         req.WorkerInstanceFlavorUUID,
		NodeGroupsStatus:       NodeGroupCreatingStatus,
		NodeGroupsType:         NodeGroupWorkerType,
		NodeGroupSecurityGroup: createWorkerSecurityResp.SecurityGroup.ID,
		IsHidden:               false,
		NodeGroupCreateDate:    time.Now(),
	}

	err = c.repository.NodeGroups().CreateNodeGroups(ctx, workerNodeGroupModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create node groups")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	rke2InitScript, err := GenerateUserDataFromTemplate("true",
		MasterServerType,
		rke2Token,
		fmt.Sprintf("%s.%s", clusterSubdomainHash, config.GlobalConfig.GetCloudflareConfig().Domain),
		req.KubernetesVersion,
		req.ClusterName,
		clusterUUID,
		req.ProjectID,
		config.GlobalConfig.GetWebConfig().Endpoint,
		authToken,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		"",
		fmt.Sprintf("%s/v3/", config.GlobalConfig.GetEndpointsConfig().IdentityEndpoint),
		config.GlobalConfig.GetVkeAgentConfig().ClusterAutoscalerVersion,
		config.GlobalConfig.GetVkeAgentConfig().CloudProviderVkeVersion,
		createApplicationCredentialReq.Credential.ID,
		createApplicationCredentialReq.Credential.Secret,
	)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to generate user data from template")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	getNetworkIdResp, err := c.networkService.GetNetworkID(ctx, authToken, req.SubnetIDs[0])
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to get networkId")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	// access from ip
	createSecurityGroupRuleReq := &request.CreateSecurityGroupRuleForIpRequest{
		SecurityGroupRule: request.SecurityGroupRuleForIP{
			Direction:       "ingress",
			PortRangeMin:    "6443",
			Ethertype:       "IPv4",
			PortRangeMax:    "6443",
			Protocol:        "tcp",
			SecurityGroupID: createMasterSecurityResp.SecurityGroup.ID,
			RemoteIPPrefix:  "0.0.0.0/0",
		},
	}

	for _, allowedCIDR := range req.AllowedCIDRS {
		createSecurityGroupRuleReq.SecurityGroupRule.RemoteIPPrefix = allowedCIDR
		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create security group rule")
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
			}
			return
		}
	}

	//for any access between cluster nodes
	// shared to shared Security Group
	createSecurityGroupRuleReqSG := &request.CreateSecurityGroupRuleForSgRequest{
		SecurityGroupRule: request.SecurityGroupRuleForSG{
			Direction:       "ingress",
			Ethertype:       "IPv4",
			SecurityGroupID: createClusterSharedSecurityResp.SecurityGroup.ID,
			RemoteGroupID:   createClusterSharedSecurityResp.SecurityGroup.ID,
		},
	}
	err = c.networkService.CreateSecurityGroupRuleForSG(ctx, authToken, *createSecurityGroupRuleReqSG)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create security group rule")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	randSubnetId := GetRandomStringFromArray(req.SubnetIDs)
	portRequest := &request.CreateNetworkPortRequest{
		Port: request.Port{
			NetworkID:    getNetworkIdResp.Subnet.NetworkID,
			Name:         "PortName",
			AdminStateUp: true,
			FixedIps: []request.FixedIp{
				{
					SubnetID: randSubnetId,
				},
			},
			SecurityGroups: []string{createMasterSecurityResp.SecurityGroup.ID, createClusterSharedSecurityResp.SecurityGroup.ID},
		},
	}
	portRequest.Port.Name = fmt.Sprintf("%v-master-1-port", req.ClusterName)
	portRequest.Port.SecurityGroups = []string{createMasterSecurityResp.SecurityGroup.ID, createClusterSharedSecurityResp.SecurityGroup.ID}
	portResp, err := c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create network port")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	masterRequest := &request.CreateComputeRequest{
		Server: request.Server{
			Name:             "ServerName",
			ImageRef:         config.GlobalConfig.GetImageRefConfig().ImageRef,
			FlavorRef:        req.MasterInstanceFlavorUUID,
			KeyName:          req.NodeKeyPairName,
			AvailabilityZone: "nova",
			SecurityGroups: []request.SecurityGroups{
				{Name: createMasterSecurityResp.SecurityGroup.Name},
				{Name: createClusterSharedSecurityResp.SecurityGroup.Name},
			},
			BlockDeviceMappingV2: []request.BlockDeviceMappingV2{
				{
					BootIndex:           0,
					DestinationType:     "volume",
					DeleteOnTermination: true,
					SourceType:          "image",
					UUID:                config.GlobalConfig.GetImageRefConfig().ImageRef,
					VolumeSize:          50,
				},
			},
			Networks: []request.Networks{
				{Port: portResp.Port.ID},
			},
			UserData: Base64Encoder(rke2InitScript),
		},
		SchedulerHints: request.SchedulerHints{
			Group: masterServerGroupResp.ServerGroup.ID,
		},
	}

	masterRequest.Server.Name = fmt.Sprintf("%v-master-1", req.ClusterName)

	_, err = c.computeService.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create compute")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	for _, subnetID := range req.SubnetIDs {
		subnetDetails, err := c.networkService.GetSubnetByID(ctx, authToken, subnetID)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to get subnet details")
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
			}
			return
		}

		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "6443"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "6443"
		createSecurityGroupRuleReq.SecurityGroupRule.SecurityGroupID = createMasterSecurityResp.SecurityGroup.ID
		createSecurityGroupRuleReq.SecurityGroupRule.RemoteIPPrefix = subnetDetails.Subnet.CIDR

		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create security group rule")

			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
			}
			return
		}

		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "9345"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "9345"
		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create security group rule")
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
			}
			return
		}

		// Access NodePort from Subnets for LB

		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "30000"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "32767"
		createSecurityGroupRuleReq.SecurityGroupRule.SecurityGroupID = createClusterSharedSecurityResp.SecurityGroup.ID
		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, authToken, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create security group rule")
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
			}
			return
		}
	}

	// add DNS record to cloudflare

	addDNSResp, err := c.cloudflareService.AddDNSRecordToCloudflare(ctx, loadbalancerIP, clusterSubdomainHash, req.ClusterName)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to add dns record to cloudflare")

		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	createListenerReq := &request.CreateListenerRequest{
		Listener: request.Listener{
			Name:           fmt.Sprintf("%v-api-listener", req.ClusterName),
			AdminStateUp:   true,
			Protocol:       "TCP",
			ProtocolPort:   6443,
			LoadbalancerID: lbResp.LoadBalancer.ID,
		},
	}

	apiListenerResp, err := c.loadbalancerService.CreateListener(ctx, authToken, *createListenerReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create listener")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	createListenerReq.Listener.Name = fmt.Sprintf("%v-register-listener", req.ClusterName)
	createListenerReq.Listener.ProtocolPort = 9345

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	registerListenerResp, err := c.loadbalancerService.CreateListener(ctx, authToken, *createListenerReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create listener")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.Errorf("failed to check load balancer status, error: %v  clusterUUID:%s", err, clusterUUID)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	createPoolReq := &request.CreatePoolRequest{
		Pool: request.Pool{
			Protocol:     "TCP",
			AdminStateUp: true,
			ListenerID:   apiListenerResp.Listener.ID,
			Name:         fmt.Sprintf("%v-api-pool", req.ClusterName),
			LBAlgorithm:  "SOURCE_IP_PORT",
		},
	}
	apiPoolResp, err := c.loadbalancerService.CreatePool(ctx, authToken, *createPoolReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create pool")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	err = c.loadbalancerService.CreateHealthTCPMonitor(ctx, authToken, request.CreateHealthMonitorTCPRequest{
		HealthMonitor: request.HealthMonitorTCP{
			Name:           fmt.Sprintf("%v-api-healthmonitor", req.ClusterName),
			AdminStateUp:   true,
			PoolID:         apiPoolResp.Pool.ID,
			MaxRetries:     "10",
			Delay:          "10",
			TimeOut:        "10",
			Type:           "TCP",
			MaxRetriesDown: 3,
		},
	})
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create health monitor")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	createPoolReq.Pool.ListenerID = registerListenerResp.Listener.ID
	createPoolReq.Pool.Name = fmt.Sprintf("%v-register-pool", req.ClusterName)
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	registerPoolResp, err := c.loadbalancerService.CreatePool(ctx, authToken, *createPoolReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create pool")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	err = c.loadbalancerService.CreateHealthHTTPMonitor(ctx, authToken, request.CreateHealthMonitorHTTPRequest{
		HealthMonitor: request.HealthMonitorHTTP{
			Name:           fmt.Sprintf("%v-register-healthmonitor", req.ClusterName),
			AdminStateUp:   true,
			PoolID:         registerPoolResp.Pool.ID,
			MaxRetries:     "10",
			Delay:          "30",
			TimeOut:        "10",
			Type:           "TCP",
			MaxRetriesDown: 3,
		},
	})
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create health monitor")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	createMemberReq := &request.AddMemberRequest{
		Member: request.Member{
			Name:         fmt.Sprintf("%v-master-1", req.ClusterName),
			AdminStateUp: true,
			SubnetID:     randSubnetId,
			Address:      portResp.Port.FixedIps[0].IpAddress,
			ProtocolPort: 6443,
			Backup:       false,
		},
	}
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	err = c.loadbalancerService.CreateMember(ctx, authToken, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create member")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	createMemberReq.Member.ProtocolPort = 9345

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	err = c.loadbalancerService.CreateMember(ctx, authToken, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create member")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	portRequest.Port.Name = fmt.Sprintf("%v-master-2-port", req.ClusterName)
	portResp, err = c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create network port")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	masterRequest.Server.Networks[0].Port = portResp.Port.ID
	masterRequest.Server.Name = fmt.Sprintf("%s-master-2", req.ClusterName)
	rke2InitScript, err = GenerateUserDataFromTemplate("false",
		MasterServerType,
		rke2Token,
		fmt.Sprintf("%s.%s", clusterSubdomainHash, config.GlobalConfig.GetCloudflareConfig().Domain),
		req.KubernetesVersion,
		req.ClusterName,
		clusterUUID,
		"",
		config.GlobalConfig.GetWebConfig().Endpoint,
		authToken,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		"",
		"",
		"",
		"",
		"",
		"",
	)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to generate user data from template")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	masterRequest.Server.UserData = Base64Encoder(rke2InitScript)

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	_, err = c.computeService.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create compute")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	//create member for master 02 for api and register pool
	createMemberReq.Member.Name = fmt.Sprintf("%v-master-2", req.ClusterName)
	createMemberReq.Member.Address = portResp.Port.FixedIps[0].IpAddress
	createMemberReq.Member.ProtocolPort = 6443
	err = c.loadbalancerService.CreateMember(ctx, authToken, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create member")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	createMemberReq.Member.ProtocolPort = 9345
	err = c.loadbalancerService.CreateMember(ctx, authToken, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create member")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	portRequest.Port.Name = fmt.Sprintf("%v-master-3-port", req.ClusterName)
	portResp, err = c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create network port")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	masterRequest.Server.Name = fmt.Sprintf("%s-master-3", req.ClusterName)
	masterRequest.Server.Networks[0].Port = portResp.Port.ID

	_, err = c.computeService.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create compute")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	masterNodeGroupModel.NodeGroupSecurityGroup = createMasterSecurityResp.SecurityGroup.ID
	masterNodeGroupModel.NodeGroupsStatus = NodeGroupActiveStatus
	masterNodeGroupModel.NodeGroupUpdateDate = time.Now()

	err = c.repository.NodeGroups().UpdateNodeGroups(ctx, masterNodeGroupModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to update node groups")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	//create member for master 03 for api and register pool
	createMemberReq.Member.Name = fmt.Sprintf("%v-master-3", req.ClusterName)
	createMemberReq.Member.Address = portResp.Port.FixedIps[0].IpAddress
	createMemberReq.Member.ProtocolPort = 6443
	err = c.loadbalancerService.CreateMember(ctx, authToken, apiPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create member")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, authToken, lbResp.LoadBalancer.ID)

	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check load balancer status")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	createMemberReq.Member.ProtocolPort = 9345
	err = c.loadbalancerService.CreateMember(ctx, authToken, registerPoolResp.Pool.ID, *createMemberReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create member")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	// Worker Create
	defaultWorkerLabels := []string{"type=default-worker"}
	nodeGroupLabelsJSON, err := json.Marshal(defaultWorkerLabels)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to marshal default worker labels")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}
	rke2WorkerInitScript, err := GenerateUserDataFromTemplate("false",
		WorkerServerType,
		rke2Token,
		fmt.Sprintf("%s.%s", clusterSubdomainHash, config.GlobalConfig.GetCloudflareConfig().Domain),
		req.KubernetesVersion,
		req.ClusterName,
		clusterUUID,
		"",
		config.GlobalConfig.GetWebConfig().Endpoint,
		authToken,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		strings.Join(defaultWorkerLabels, ","),
		"",
		"",
		"",
		"",
		"",
	)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to generate user data from template")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	WorkerRequest := &request.CreateComputeRequest{
		Server: request.Server{
			Name:             "ServerName",
			ImageRef:         config.GlobalConfig.GetImageRefConfig().ImageRef,
			FlavorRef:        req.WorkerInstanceFlavorUUID,
			KeyName:          req.NodeKeyPairName,
			AvailabilityZone: "nova",
			SecurityGroups: []request.SecurityGroups{
				{Name: createWorkerSecurityResp.SecurityGroup.Name},
				{Name: createClusterSharedSecurityResp.SecurityGroup.Name},
			},
			BlockDeviceMappingV2: []request.BlockDeviceMappingV2{
				{
					BootIndex:           0,
					DestinationType:     "volume",
					DeleteOnTermination: true,
					SourceType:          "image",
					UUID:                config.GlobalConfig.GetImageRefConfig().ImageRef,
					VolumeSize:          req.WorkerDiskSizeGB,
				},
			},
			Networks: []request.Networks{
				{Port: portResp.Port.ID},
			},
			UserData: Base64Encoder(rke2WorkerInitScript),
		},
		SchedulerHints: request.SchedulerHints{
			Group: workerServerGroupResp.ServerGroup.ID,
		},
	}
	for i := 1; i <= req.WorkerNodeGroupMinSize; i++ {
		portRequest.Port.Name = fmt.Sprintf("%v-%s-port", req.ClusterName, workerNodeGroupModel.NodeGroupName)
		portRequest.Port.SecurityGroups = []string{createWorkerSecurityResp.SecurityGroup.ID, createClusterSharedSecurityResp.SecurityGroup.ID}
		portResp, err = c.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create network port")
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
			}
			return
		}
		WorkerRequest.Server.Networks[0].Port = portResp.Port.ID
		WorkerRequest.Server.Name = fmt.Sprintf("%s-%s", workerNodeGroupModel.NodeGroupName, uuid.New().String())

		_, err = c.computeService.CreateCompute(ctx, authToken, *WorkerRequest)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create compute")
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
			}
			return
		}
	}
	workerNodeGroupModel.NodeGroupLabels = nodeGroupLabelsJSON
	workerNodeGroupModel.NodeGroupsStatus = NodeGroupActiveStatus
	workerNodeGroupModel.NodeGroupSecurityGroup = createWorkerSecurityResp.SecurityGroup.ID
	workerNodeGroupModel.NodeGroupUpdateDate = time.Now()

	err = c.repository.NodeGroups().UpdateNodeGroups(ctx, workerNodeGroupModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to update node groups")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
		}
		return
	}

	clusterModel = &model.Cluster{
		ClusterUUID:                clusterUUID,
		ClusterName:                req.ClusterName,
		ClusterVersion:             req.KubernetesVersion,
		ClusterStatus:              ActiveClusterStatus,
		ClusterProjectUUID:         req.ProjectID,
		ClusterLoadbalancerUUID:    lbResp.LoadBalancer.ID,
		ClusterRegisterToken:       rke2Token,
		ClusterAgentToken:          rke2AgentToken,
		ClusterSubnets:             subnetIdsJSON,
		ClusterNodeKeypairName:     req.NodeKeyPairName,
		ClusterAPIAccess:           req.ClusterAPIAccess,
		FloatingIPUUID:             floatingIPUUID,
		ClusterSharedSecurityGroup: ClusterSharedSecurityGroupUUID,
		ClusterEndpoint:            addDNSResp.Result.Name,
		ClusterCloudflareRecordID:  addDNSResp.Result.ID,
	}
	err = c.CheckKubeConfig(ctx, clusterUUID)
	if err != nil {
		clusterModel.ClusterStatus = ErrorClusterStatus
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to check kube config")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}
	}
	err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to update cluster")
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
		}
		return
	}

	err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Created")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create audit log")
		return
	}
}

func (c *clusterService) GetCluster(ctx context.Context, authToken, clusterID string) (resource.GetClusterResponse, error) {
	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetClusterResponse{}, err
	}

	if cluster == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetClusterResponse{}, nil
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetClusterResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		return resource.GetClusterResponse{}, err
	}

	clusterResp := resource.GetClusterResponse{
		ClusterName:                cluster.ClusterName,
		ClusterID:                  cluster.ClusterUUID,
		ProjectID:                  cluster.ClusterProjectUUID,
		KubernetesVersion:          cluster.ClusterVersion,
		ClusterAPIAccess:           cluster.ClusterAPIAccess,
		ClusterStatus:              cluster.ClusterStatus,
		ClusterSharedSecurityGroup: cluster.ClusterSharedSecurityGroup,
	}

	return clusterResp, nil
}

func (c *clusterService) GetClusterDetails(ctx context.Context, authToken, clusterID string) (resource.GetClusterDetailsResponse, error) {
	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID}).Error("failed to get cluster")
		return resource.GetClusterDetailsResponse{}, err
	}

	if cluster == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetClusterDetailsResponse{}, nil
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetClusterDetailsResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		return resource.GetClusterDetailsResponse{}, err
	}

	clusterSubnetsArr := []string{}

	err = json.Unmarshal(cluster.ClusterSubnets, &clusterSubnetsArr)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to unmarshal cluster subnets")
		return resource.GetClusterDetailsResponse{}, err
	}

	getClusterDetailsResp := resource.GetClusterDetailsResponse{
		ClusterUUID:                  cluster.ClusterUUID,
		ClusterName:                  cluster.ClusterName,
		ClusterVersion:               cluster.ClusterVersion,
		ClusterStatus:                cluster.ClusterStatus,
		ClusterProjectUUID:           cluster.ClusterProjectUUID,
		ClusterLoadbalancerUUID:      cluster.ClusterLoadbalancerUUID,
		ClusterSubnets:               clusterSubnetsArr,
		ClusterEndpoint:              cluster.ClusterEndpoint,
		ClusterAPIAccess:             cluster.ClusterAPIAccess,
		ClusterCertificateExpireDate: cluster.ClusterCertificateExpireDate,
	}

	nodeGroups, err := c.nodeGroupsService.GetNodeGroupsByClusterUUID(ctx, cluster.ClusterUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get node groups")
		return resource.GetClusterDetailsResponse{}, err
	}

	if nodeGroups == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get node groups")

		getClusterDetailsResp.ClusterMasterServerGroup = resource.NodeGroup{}
		getClusterDetailsResp.ClusterWorkerServerGroups = []resource.NodeGroup{}

		return getClusterDetailsResp, nil
	}

	for _, nodeGroup := range nodeGroups {
		if nodeGroup.NodeGroupsType == NodeGroupMasterType {
			getClusterDetailsResp.ClusterMasterServerGroup = nodeGroup
			continue
		}

		getClusterDetailsResp.ClusterWorkerServerGroups = append(getClusterDetailsResp.ClusterWorkerServerGroups, nodeGroup)
	}

	return getClusterDetailsResp, nil
}

func (c *clusterService) GetClustersByProjectId(ctx context.Context, authToken, projectID string) ([]resource.GetClusterResponse, error) {
	clusters, err := c.repository.Cluster().GetClustersByProjectId(ctx, projectID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"projectID": projectID,
		}).Error("failed to get cluster")
		return []resource.GetClusterResponse{}, err
	}

	if clusters == nil {
		c.logger.WithFields(logrus.Fields{
			"projectID": projectID,
		}).Error("failed to get cluster")
		return []resource.GetClusterResponse{}, nil
	}

	err = c.identityService.CheckAuthToken(ctx, authToken, projectID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"projectID": projectID,
		}).Error("failed to check auth token")
		return []resource.GetClusterResponse{}, err
	}

	var clustersResp []resource.GetClusterResponse

	for _, cluster := range clusters {
		clustersResp = append(clustersResp, resource.GetClusterResponse{
			ClusterName:                cluster.ClusterName,
			ClusterID:                  cluster.ClusterUUID,
			ProjectID:                  cluster.ClusterProjectUUID,
			KubernetesVersion:          cluster.ClusterVersion,
			ClusterAPIAccess:           cluster.ClusterAPIAccess,
			ClusterStatus:              cluster.ClusterStatus,
			ClusterSharedSecurityGroup: cluster.ClusterSharedSecurityGroup,
		})
	}

	if clustersResp == nil {
		return nil, err
	}

	return clustersResp, nil
}

func (c *clusterService) DestroyCluster(ctx context.Context, authToken, clusterID string, clUUID chan string) {
	clUUID <- clusterID

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return
	}

	if cluster == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return
	}

	err = c.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		return
	}

	err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Cluster Destroying")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to create audit log")
	}

	clModel := &model.Cluster{
		ClusterStatus:     DeletingClusterStatus,
		ClusterDeleteDate: time.Now(),
	}

	err = c.repository.Cluster().DeleteUpdateCluster(ctx, clModel, cluster.ClusterUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to delete update cluster")
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to delete update cluster")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to create audit log")
		}
	}
	//Delete LoadBalancer Pool and Listener
	getLoadBalancerPoolsResponse, err := c.loadbalancerService.GetLoadBalancerPools(ctx, authToken, cluster.ClusterLoadbalancerUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get load balancer pools")
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to get load balancer pools")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to create audit log")
		}
	}

	for _, member := range getLoadBalancerPoolsResponse.Pools {
		err = c.loadbalancerService.DeleteLoadbalancerPools(ctx, authToken, member)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"pool":        member,
			}).Error("failed to delete load balancer pools")
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to delete load balancer pools")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
				}).Error("failed to create audit log")
			}
		}
		err = c.loadbalancerService.CheckLoadBalancerDeletingPools(ctx, authToken, member)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"pool":        member,
			}).Error("failed to check load balancer deleting pools")

			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to check load balancer deleting pools")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
				}).Error("failed to create audit log")
			}
		}
	}
	getLoadBalancerListenersResponse, err := c.loadbalancerService.GetLoadBalancerListeners(ctx, authToken, cluster.ClusterLoadbalancerUUID)
	if err != nil {
		c.logger.Errorf("failed to get loadbalancer listeners, error: %v clusterUUID: %s", err, cluster.ClusterUUID)
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to get loadbalancer listeners")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to create audit log")
		}
	}
	for _, member := range getLoadBalancerListenersResponse.Listeners {
		err = c.loadbalancerService.DeleteLoadbalancerListeners(ctx, authToken, member)
		if err != nil {
			c.logger.Errorf("failed to delete loadbalancer listeners, error: %v clusterUUID:%s Listener: %v", err, cluster.ClusterUUID, member)
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to delete loadbalancer listeners")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
				}).Error("failed to create audit log")
			}
		}
		err = c.loadbalancerService.CheckLoadBalancerDeletingListeners(ctx, authToken, member)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to check loadbalancer deleting listeners")
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to check loadbalancer deleting listeners")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
				}).Error("failed to create audit log")
			}
		}
	}

	//Delete LoadBalancer
	err = c.loadbalancerService.DeleteLoadbalancer(ctx, authToken, cluster.ClusterLoadbalancerUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to delete load balancer")
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to delete load balancer")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to create audit log")
		}
	}

	//Delete DNS Record
	err = c.cloudflareService.DeleteDNSRecordFromCloudflare(ctx, cluster.ClusterCloudflareRecordID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to delete dns record")
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to delete dns record")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to create audit log")
		}
	}

	//Delete FloatingIP
	if cluster.ClusterAPIAccess == "public" {
		deleteFloatingIPResp := c.networkService.DeleteFloatingIP(ctx, authToken, cluster.FloatingIPUUID)
		if deleteFloatingIPResp != nil {
			c.logger.WithError(deleteFloatingIPResp).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to delete floating ip")
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to delete floating ip")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
				}).Error("failed to create audit log")
			}
		}
	}
	nodeGroupsOfCluster, err := c.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, cluster.ClusterUUID, "")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get node groups")
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to get node groups")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to create audit log")
		}
	}
	// Delete Worker Server Group Members ports and compute and server groups
	var clusterWorkerServerGroupsUUIDString []string
	for _, nodeGroup := range nodeGroupsOfCluster {
		clusterWorkerServerGroupsUUIDString = append(clusterWorkerServerGroupsUUIDString, nodeGroup.NodeGroupUUID)
	}
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to unmarshal cluster worker server groups uuid")
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to unmarshal cluster worker server groups uuid")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to create audit log")
		}
	}
	for _, serverGroup := range nodeGroupsOfCluster {
		getServerGroupMembersListResp, err := c.computeService.GetServerGroupMemberList(ctx, authToken, serverGroup.NodeGroupUUID)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to get server group members list")
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to get server group members list")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
				}).Error("failed to create audit log")
			}
		}
		for _, member := range getServerGroupMembersListResp.Members {
			getWorkerComputePortIdResp, err := c.networkService.GetComputeNetworkPorts(ctx, authToken, member)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
				}).Error("failed to get compute network ports")
				err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to get compute network ports")
				if err != nil {
					c.logger.WithError(err).WithFields(logrus.Fields{
						"clusterUUID": cluster.ClusterUUID,
					}).Error("failed to create audit log")
				}
			}
			if len(getWorkerComputePortIdResp.Ports) > 0 {
				err = c.networkService.DeleteNetworkPort(ctx, authToken, getWorkerComputePortIdResp.Ports[0])
				if err != nil {
					c.logger.WithError(err).WithFields(logrus.Fields{
						"clusterUUID": cluster.ClusterUUID,
					}).Error("failed to delete network port")
					err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to delete network port")
					if err != nil {
						c.logger.WithError(err).WithFields(logrus.Fields{
							"clusterUUID": cluster.ClusterUUID,
						}).Error("failed to create audit log")
					}
				}
			} else {
				c.logger.WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
				}).Errorf("compute node port not found, error: %v clusterUUID:%s PortID: %v", err, cluster.ClusterUUID, member)
				err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to get compute network ports")
				if err != nil {
					c.logger.WithFields(logrus.Fields{
						"clusterUUID": cluster.ClusterUUID,
					}).Error("failed to create audit log")
				}
			}

			err = c.computeService.DeleteCompute(ctx, authToken, member)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
				}).Errorf("failed to delete compute, error: %v clusterUUID:%s ComputeID: %v", err, cluster.ClusterUUID, member)
				err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to delete compute")
				if err != nil {
					c.logger.WithError(err).WithFields(logrus.Fields{
						"clusterUUID": cluster.ClusterUUID,
					}).Error("failed to create audit log")
				}
			}
		}
		err = c.computeService.DeleteServerGroup(ctx, authToken, serverGroup.NodeGroupUUID)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Errorf("failed to delete server group, error: %v clusterUUID:%s ServerGroup: %s", err, cluster.ClusterUUID, serverGroup.NodeGroupUUID)
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to delete server group")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
				}).Error("failed to create audit log")
			}
		}
		// Delete Worker Security Group
		err = c.networkService.DeleteSecurityGroup(ctx, authToken, serverGroup.NodeGroupSecurityGroup)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Errorf("failed to delete security group, error: %v clusterUUID:%s SecurityGroup: %s", err, cluster.ClusterUUID, serverGroup.NodeGroupSecurityGroup)
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to delete security group")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
				}).Error("failed to create audit log")
			}
		}
		ngModel := &model.NodeGroups{
			NodeGroupUUID:       serverGroup.NodeGroupUUID,
			NodeGroupsStatus:    NodeGroupDeletedStatus,
			NodeGroupUpdateDate: time.Now(),
		}
		err = c.repository.NodeGroups().UpdateNodeGroups(ctx, ngModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Errorf("failed to update node groups, error: %v clusterUUID:%s NodeGroupUUID: %s", err, cluster.ClusterUUID, serverGroup.NodeGroupUUID)
			err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to update node groups")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
				}).Error("failed to create audit log")
			}
		}
	}
	// Delete Cluster Shared Security Group
	err = c.networkService.DeleteSecurityGroup(ctx, authToken, cluster.ClusterSharedSecurityGroup)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Errorf("failed to delete security group, error: %v clusterUUID:%s SecurityGroup: %s", err, cluster.ClusterUUID, cluster.ClusterSharedSecurityGroup)
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to delete security group")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to create audit log")
		}
	}

	// Delete Application Credentials
	err = c.identityService.DeleteApplicationCredential(ctx, authToken, cluster.ApplicationCredentialID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to delete application credentials")
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to delete application credentials")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to create audit log")
		}
	}

	clModel = &model.Cluster{
		ClusterStatus:     DeletedClusterStatus,
		ClusterDeleteDate: time.Now(),
	}

	err = c.repository.Cluster().DeleteUpdateCluster(ctx, clModel, cluster.ClusterUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to delete update cluster")
		err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "failed to delete update cluster")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to create audit log")
		}
	}

	err = c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Cluster Destroyed")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to create audit log")
	}
}

func (c *clusterService) GetKubeConfig(ctx context.Context, authToken, clusterID string) (resource.GetKubeConfigResponse, error) {
	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetKubeConfigResponse{}, err
	}

	if cluster == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetKubeConfigResponse{}, err
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.GetKubeConfigResponse{}, err
	}

	err = c.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		return resource.GetKubeConfigResponse{}, err
	}

	kubeConfig, err := c.repository.Kubeconfig().GetKubeconfigByUUID(ctx, cluster.ClusterUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get kube config")
		return resource.GetKubeConfigResponse{}, err
	}

	clusterResp := resource.GetKubeConfigResponse{
		ClusterUUID: kubeConfig.ClusterUUID,
		KubeConfig:  kubeConfig.KubeConfig,
	}

	return clusterResp, nil
}

func (c *clusterService) CreateKubeConfig(ctx context.Context, authToken string, req request.CreateKubeconfigRequest) (resource.CreateKubeconfigResponse, error) {
	if req.ClusterID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to get cluster")
		return resource.CreateKubeconfigResponse{}, fmt.Errorf("failed to get cluster")
	}

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, req.ClusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to get cluster")
		return resource.CreateKubeconfigResponse{}, err
	}

	if cluster == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to get cluster")
		return resource.CreateKubeconfigResponse{}, fmt.Errorf("failed to get cluster")
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to get cluster")
		return resource.CreateKubeconfigResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to check auth token")
		return resource.CreateKubeconfigResponse{}, err
	}

	kubeConfig := &model.Kubeconfigs{
		ClusterUUID: cluster.ClusterUUID,
		KubeConfig:  req.KubeConfig,
		CreateDate:  time.Now(),
	}

	if !IsValidBase64(req.KubeConfig) {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to create kube config, invalid kube config")
		return resource.CreateKubeconfigResponse{}, fmt.Errorf("failed to create kube config, invalid kube config")
	}

	err = c.repository.Kubeconfig().CreateKubeconfig(ctx, kubeConfig)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": req.ClusterID,
		}).Error("failed to create kube config")
		return resource.CreateKubeconfigResponse{}, err
	}

	return resource.CreateKubeconfigResponse{
		ClusterUUID: kubeConfig.ClusterUUID,
	}, nil
}
