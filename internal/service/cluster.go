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
	"github.com/vmindtech/vke/pkg/constants"
)

type IClusterService interface {
	CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest, clUUID chan string)
	GetCluster(ctx context.Context, authToken, clusterID string) (resource.GetClusterResponse, error)
	GetClusterDetails(ctx context.Context, authToken, clusterID string) (resource.GetClusterDetailsResponse, error)
	GetClustersByProjectId(ctx context.Context, authToken, projectID string) ([]resource.GetClusterResponse, error)
	DestroyCluster(ctx context.Context, authToken string, clusterID string)
	UpdateCluster(ctx context.Context, authToken, clusterID string, req request.UpdateClusterRequest) (resource.UpdateClusterResponse, error)
	GetClusterErrors(ctx context.Context, authToken, clusterID string) ([]resource.GetClusterErrorsResponse, error)
	GetKubeConfig(ctx context.Context, authToken, clusterID string) (resource.GetKubeConfigResponse, error)
	CreateKubeConfig(ctx context.Context, authToken string, req request.CreateKubeconfigRequest) (resource.CreateKubeconfigResponse, error)
	UpdateKubeConfig(ctx context.Context, authToken string, clusterID string, req request.UpdateKubeconfigRequest) (resource.UpdateKubeconfigResponse, error)
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

func (c *clusterService) logClusterError(ctx context.Context, clusterUUID, errorMessage string) {
	if clusterUUID == "" {
		clusterUUID = "unknown"
	}

	errorRecord := &model.Error{
		ClusterUUID:  clusterUUID,
		ErrorMessage: errorMessage,
		CreatedAt:    time.Now(),
	}

	go func() {
		if err := c.repository.Error().CreateError(ctx, errorRecord); err != nil {
			c.logger.WithError(err).Error("Failed to save cluster error to database")
		}
	}()
}

func (c *clusterService) logClusterErrorWithDetails(ctx context.Context, clusterUUID, baseMessage, operation, details string) {
	errorMessage := constants.GetDetailedErrorMessage(baseMessage, operation, clusterUUID, details)
	c.logClusterError(ctx, clusterUUID, errorMessage)
}

func (c *clusterService) logClusterErrorSimple(ctx context.Context, clusterUUID, baseMessage, operation string) {
	errorMessage := constants.GetErrorMessage(baseMessage, operation, clusterUUID)
	c.logClusterError(ctx, clusterUUID, errorMessage)
}

func (c *clusterService) logClusterErrorSafe(ctx context.Context, clusterUUID, baseMessage, operation string, err error) {
	errorMessage := constants.GetSafeErrorMessage(baseMessage, operation, clusterUUID, err)
	c.logClusterError(ctx, clusterUUID, errorMessage)
}

func (c *clusterService) logClusterErrorFiltered(ctx context.Context, clusterUUID, baseMessage, operation string, err error) {
	errorMessage := constants.GetFilteredErrorMessage(baseMessage, operation, clusterUUID, err)
	c.logClusterError(ctx, clusterUUID, errorMessage)
}

func (c *clusterService) CheckKubeConfig(ctx context.Context, clusterUUID string) error {
	waitIterator := 0
	waitSeconds := 10
	for {
		if waitIterator < 30 {
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
	token := strings.Clone(authToken)

	err := c.identityService.CheckAuthToken(ctx, token, req.ProjectID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"projectID": req.ProjectID,
		}).Error("failed to check auth token")
		c.logClusterErrorSafe(ctx, "", constants.ErrAuthTokenCheckFailed, "cluster_creation", err)
		return
	}

	clusterUUID := uuid.New().String()
	clUUID <- clusterUUID

	subnetIdsJSON, err := json.Marshal(req.SubnetIDs)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to marshal subnet ids")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrClusterSubnetInvalid, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}
		return
	}

	createApplicationCredentialReq, err := c.identityService.CreateApplicationCredential(ctx, clusterUUID, token)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create application credential")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrApplicationCredentialCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}
		return
	}

	resourceModel := &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "application_credential",
		ResourceUUID: createApplicationCredentialReq.Credential.ID,
	}
	err = c.repository.Resources().CreateResource(ctx, resourceModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create resource")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrResourceCreateFailed, "cluster_creation", err)
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
		DeleteState:                  constants.DeleteStateInitial,
	}

	err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create audit log")
		c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err.Error())
		return
	}

	err = c.repository.Cluster().CreateCluster(ctx, clusterModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create cluster")
		c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrClusterCreateFailed, "cluster_creation", err.Error())

		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err.Error())
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
			Provider:     config.GlobalConfig.GetOpenStackApiConfig().LoadbalancerProvider,
		},
	}

	lbResp, err := c.loadbalancerService.CreateLoadBalancer(ctx, token, *createLBReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create load balancer")
		c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrLoadBalancerCreateFailed, "cluster_creation", err.Error())
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err.Error())
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorWithDetails(ctx, clusterUUID, constants.ErrClusterCreateFailed, "cluster_creation", err.Error())
		}
		return
	}
	loadBalancerResourceModel := &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "load_balancer",
		ResourceUUID: lbResp.LoadBalancer.ID,
	}
	err = c.repository.Resources().CreateResource(ctx, loadBalancerResourceModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create resource")
		return
	}

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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

	listLBResp, err := c.loadbalancerService.ListLoadBalancer(ctx, token, lbResp.LoadBalancer.ID)
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
		createFloatingIPResponse, err := c.networkService.CreateFloatingIP(ctx, token, *createFloatingIPreq)
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
		floatingIPResourceModel := &model.Resource{
			ClusterUUID:  clusterUUID,
			ResourceType: "floating_ip",
			ResourceUUID: createFloatingIPResponse.FloatingIP.ID,
		}
		err = c.repository.Resources().CreateResource(ctx, floatingIPResourceModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create resource")
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
	createMasterSecurityResp, err := c.networkService.CreateSecurityGroup(ctx, token, *createSecurityGroupReq)
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
	masterSecurityGroupResourceModel := &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "security_group",
		ResourceUUID: createMasterSecurityResp.SecurityGroup.ID,
	}
	err = c.repository.Resources().CreateResource(ctx, masterSecurityGroupResourceModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create resource")
		return
	}
	// create security group for worker
	createSecurityGroupReq.SecurityGroup.Name = fmt.Sprintf("%v-worker-sg", req.ClusterName)
	createSecurityGroupReq.SecurityGroup.Description = fmt.Sprintf("%v-worker-sg", req.ClusterName)

	createWorkerSecurityResp, err := c.networkService.CreateSecurityGroup(ctx, token, *createSecurityGroupReq)
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
	workerSecurityGroupResourceModel := &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "security_group",
		ResourceUUID: createWorkerSecurityResp.SecurityGroup.ID,
	}
	err = c.repository.Resources().CreateResource(ctx, workerSecurityGroupResourceModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create resource")
		return
	}

	// create security group for shared
	createSecurityGroupReq.SecurityGroup.Name = fmt.Sprintf("%v-cluster-shared-sg", req.ClusterName)
	createSecurityGroupReq.SecurityGroup.Description = fmt.Sprintf("%v-cluster-shared-sg", req.ClusterName)

	createClusterSharedSecurityResp, err := c.networkService.CreateSecurityGroup(ctx, token, *createSecurityGroupReq)
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
	clusterSharedSecurityGroupResourceModel := &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "security_group",
		ResourceUUID: createClusterSharedSecurityResp.SecurityGroup.ID,
	}
	err = c.repository.Resources().CreateResource(ctx, clusterSharedSecurityGroupResourceModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create resource")
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
	masterServerGroupResp, err := c.computeService.CreateServerGroup(ctx, token, *createServerGroupReq)
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

	masterServerGroupResourceModel := &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "server_group",
		ResourceUUID: masterServerGroupResp.ServerGroup.ID,
	}
	err = c.repository.Resources().CreateResource(ctx, masterServerGroupResourceModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create resource")
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
	workerServerGroupResp, err := c.computeService.CreateServerGroup(ctx, token, *createServerGroupReq)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create server group")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrComputeServerGroupCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
		}
		return
	}
	workerServerGroupResourceModel := &model.Resource{
		ClusterUUID:  clusterUUID,
		ResourceType: "server_group",
		ResourceUUID: workerServerGroupResp.ServerGroup.ID,
	}
	err = c.repository.Resources().CreateResource(ctx, workerServerGroupResourceModel)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create resource")
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
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrNodeGroupCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
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
		token,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		"",
		"",
		fmt.Sprintf("%s/v3/", config.GlobalConfig.GetEndpointsConfig().EnvoyEndpoint),
		config.GlobalConfig.GetVkeAgentConfig().ClusterAutoscalerVersion,
		config.GlobalConfig.GetVkeAgentConfig().CloudProviderVkeVersion,
		createApplicationCredentialReq.Credential.ID,
		createApplicationCredentialReq.Credential.Secret,
		config.GlobalConfig.GetVkeAgentConfig().ClusterAgentVersion,
		config.GlobalConfig.GetPublicNetworkIDConfig().PublicNetworkID,
	)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to generate user data from template")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrClusterCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
		}
		return
	}

	getNetworkIdResp, err := c.networkService.GetNetworkID(ctx, token, req.SubnetIDs[0])
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to get networkId")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrNetworkCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
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
		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, token, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create security group rule")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrSecurityGroupCreateFailed, "cluster_creation", err)
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
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
	err = c.networkService.CreateSecurityGroupRuleForSG(ctx, token, *createSecurityGroupRuleReqSG)
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
	portResp, err := c.networkService.CreateNetworkPort(ctx, token, *portRequest)
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

	_, err = c.computeService.CreateCompute(ctx, token, *masterRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create compute")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrComputeCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
		}
		return
	}

	for _, subnetID := range req.SubnetIDs {
		subnetDetails, err := c.networkService.GetSubnetByID(ctx, token, subnetID)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to get subnet details")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrSubnetCreateFailed, "cluster_creation", err)
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
			}
			return
		}

		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "6443"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "6443"
		createSecurityGroupRuleReq.SecurityGroupRule.SecurityGroupID = createMasterSecurityResp.SecurityGroup.ID
		createSecurityGroupRuleReq.SecurityGroupRule.RemoteIPPrefix = subnetDetails.Subnet.CIDR

		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, token, *createSecurityGroupRuleReq)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create security group rule")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrSecurityGroupCreateFailed, "cluster_creation", err)
			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
			}
			return
		}

		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMin = "9345"
		createSecurityGroupRuleReq.SecurityGroupRule.PortRangeMax = "9345"
		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, token, *createSecurityGroupRuleReq)
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
		err = c.networkService.CreateSecurityGroupRuleForIP(ctx, token, *createSecurityGroupRuleReq)
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

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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

	apiListenerResp, err := c.loadbalancerService.CreateListener(ctx, token, *createListenerReq)
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

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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

	registerListenerResp, err := c.loadbalancerService.CreateListener(ctx, token, *createListenerReq)
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

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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
	apiPoolResp, err := c.loadbalancerService.CreatePool(ctx, token, *createPoolReq)
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
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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
	err = c.loadbalancerService.CreateHealthTCPMonitor(ctx, token, request.CreateHealthMonitorTCPRequest{
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
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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
	registerPoolResp, err := c.loadbalancerService.CreatePool(ctx, token, *createPoolReq)
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
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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
	err = c.loadbalancerService.CreateHealthHTTPMonitor(ctx, token, request.CreateHealthMonitorHTTPRequest{
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
	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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

	err = c.loadbalancerService.CreateMember(ctx, token, apiPoolResp.Pool.ID, *createMemberReq)
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

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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

	err = c.loadbalancerService.CreateMember(ctx, token, registerPoolResp.Pool.ID, *createMemberReq)
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
	portResp, err = c.networkService.CreateNetworkPort(ctx, token, *portRequest)
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
		token,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		config.GlobalConfig.GetPublicNetworkIDConfig().PublicNetworkID,
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

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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

	_, err = c.computeService.CreateCompute(ctx, token, *masterRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create compute")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrComputeCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
		}
		return
	}

	//create member for master 02 for api and register pool
	createMemberReq.Member.Name = fmt.Sprintf("%v-master-2", req.ClusterName)
	createMemberReq.Member.Address = portResp.Port.FixedIps[0].IpAddress
	createMemberReq.Member.ProtocolPort = 6443
	err = c.loadbalancerService.CreateMember(ctx, token, apiPoolResp.Pool.ID, *createMemberReq)
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

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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
	err = c.loadbalancerService.CreateMember(ctx, token, registerPoolResp.Pool.ID, *createMemberReq)
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
	portResp, err = c.networkService.CreateNetworkPort(ctx, token, *portRequest)
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

	_, err = c.computeService.CreateCompute(ctx, token, *masterRequest)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).Error("failed to create compute")
		c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrComputeCreateFailed, "cluster_creation", err)
		err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
		}

		clusterModel.ClusterStatus = ErrorClusterStatus
		err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to update cluster")
			c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
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

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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
	err = c.loadbalancerService.CreateMember(ctx, token, apiPoolResp.Pool.ID, *createMemberReq)
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

	_, err = c.loadbalancerService.CheckLoadBalancerStatus(ctx, token, lbResp.LoadBalancer.ID)

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
	err = c.loadbalancerService.CreateMember(ctx, token, registerPoolResp.Pool.ID, *createMemberReq)
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
		token,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		strings.Join(defaultWorkerLabels, ","),
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		config.GlobalConfig.GetPublicNetworkIDConfig().PublicNetworkID,
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
		portResp, err = c.networkService.CreateNetworkPort(ctx, token, *portRequest)
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
		WorkerRequest.Server.Name = fmt.Sprintf("%s-%s", workerNodeGroupModel.NodeGroupName, uuid.New().String()[:8])

		_, err = c.computeService.CreateCompute(ctx, token, *WorkerRequest)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": clusterUUID,
			}).Error("failed to create compute")

			// Check if it's a quota exceeded error
			if strings.Contains(err.Error(), "Quota exceeded") {
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrComputeQuotaExceeded, "cluster_creation", err)
			} else {
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrComputeCreateFailed, "cluster_creation", err)
			}

			err = c.CreateAuditLog(ctx, clusterUUID, req.ProjectID, "Cluster Create Failed")
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to create audit log")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrAuditLogCreateFailed, "cluster_creation", err)
			}

			clusterModel.ClusterStatus = ErrorClusterStatus
			err = c.repository.Cluster().UpdateCluster(ctx, clusterModel)
			if err != nil {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": clusterUUID,
				}).Error("failed to update cluster")
				c.logClusterErrorFiltered(ctx, clusterUUID, constants.ErrDatabaseQueryFailed, "cluster_creation", err)
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
	token := strings.Clone(authToken)

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

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		return resource.GetClusterResponse{}, err
	}

	clusterResp := resource.GetClusterResponse{
		ClusterName:                  cluster.ClusterName,
		ClusterID:                    cluster.ClusterUUID,
		ProjectID:                    cluster.ClusterProjectUUID,
		KubernetesVersion:            cluster.ClusterVersion,
		ClusterAPIAccess:             cluster.ClusterAPIAccess,
		ClusterStatus:                cluster.ClusterStatus,
		ClusterSharedSecurityGroup:   cluster.ClusterSharedSecurityGroup,
		ClusterCertificateExpireDate: cluster.ClusterCertificateExpireDate,
	}

	return clusterResp, nil
}

func (c *clusterService) GetClusterDetails(ctx context.Context, authToken, clusterID string) (resource.GetClusterDetailsResponse, error) {
	token := strings.Clone(authToken)

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

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
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
	token := strings.Clone(authToken)

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

	err = c.identityService.CheckAuthToken(ctx, token, projectID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"projectID": projectID,
		}).Error("failed to check auth token")
		return []resource.GetClusterResponse{}, err
	}

	var clustersResp []resource.GetClusterResponse

	for _, cluster := range clusters {
		clustersResp = append(clustersResp, resource.GetClusterResponse{
			ClusterName:                  cluster.ClusterName,
			ClusterID:                    cluster.ClusterUUID,
			ProjectID:                    cluster.ClusterProjectUUID,
			KubernetesVersion:            cluster.ClusterVersion,
			ClusterAPIAccess:             cluster.ClusterAPIAccess,
			ClusterStatus:                cluster.ClusterStatus,
			ClusterSharedSecurityGroup:   cluster.ClusterSharedSecurityGroup,
			ClusterCertificateExpireDate: cluster.ClusterCertificateExpireDate,
		})
	}

	if clustersResp == nil {
		return nil, err
	}

	return clustersResp, nil
}

func (c *clusterService) DestroyCluster(ctx context.Context, authToken string, clusterID string) {
	token := strings.Clone(authToken)

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithField("clusterUUID", clusterID).Error("failed to get cluster")
		c.logClusterErrorWithDetails(ctx, clusterID, constants.ErrDatabaseQueryFailed, "cluster_deletion", err.Error())
		return
	}

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		c.logClusterErrorSimple(ctx, clusterID, constants.ErrAuthTokenCheckFailed, "cluster_deletion")
		return
	}

	if cluster.ClusterStatus == CreatingClusterStatus {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Info("cluster is being created, cannot delete")
		return
	}

	err = c.repository.Cluster().DeleteUpdateCluster(ctx, &model.Cluster{
		ClusterStatus:     DeletingClusterStatus,
		ClusterDeleteDate: time.Now(),
		DeleteState:       constants.DeleteStateInitial,
	}, clusterID)
	if err != nil {
		c.logger.WithError(err).WithField("clusterUUID", clusterID).Error("failed to update cluster status")
		c.logClusterErrorWithDetails(ctx, clusterID, constants.ErrDatabaseQueryFailed, "cluster_deletion", err.Error())
		return
	}

	cluster.DeleteState = constants.DeleteStateInitial

	c.logger.WithFields(logrus.Fields{
		"clusterUUID": cluster.ClusterUUID,
		"clusterName": cluster.ClusterName,
		"deleteState": cluster.DeleteState,
	}).Info("starting cluster deletion")

	switch cluster.DeleteState {
	case constants.DeleteStateInitial:
		maxRetries := 10
		waitSeconds := 3
		var lastError error
		for attempt := 1; attempt <= maxRetries; attempt++ {
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			if err := c.deleteLoadBalancerComponents(ctx, token, cluster); err != nil {
				lastError = err
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
					"attempt":     attempt,
				}).Error("failed to delete load balancer components")
			} else {
				break
			}
		}

		if lastError != nil {
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrLoadBalancerDeleteFailed, "cluster_deletion", lastError)
		}
		cluster.DeleteState = constants.DeleteStateLoadBalancer
		c.updateClusterDeleteState(ctx, cluster)
		fallthrough

	case constants.DeleteStateLoadBalancer:
		if err := c.deleteDNSRecord(ctx, cluster); err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to delete DNS record")
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrDNSRecordDeleteFailed, "cluster_deletion", err)
		}
		cluster.DeleteState = constants.DeleteStateDNS
		c.updateClusterDeleteState(ctx, cluster)
		fallthrough

	case constants.DeleteStateDNS:
		if err := c.deleteFloatingIP(ctx, token, cluster); err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to delete floating IP")
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrFloatingIPDeleteFailed, "cluster_deletion", err)
		}
		cluster.DeleteState = constants.DeleteStateFloatingIP
		c.updateClusterDeleteState(ctx, cluster)
		fallthrough

	case constants.DeleteStateFloatingIP:
		if err := c.deleteNodeGroups(ctx, token, cluster); err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to delete node groups")
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrNodeGroupDeleteFailed, "cluster_deletion", err)
		}
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
			"deleteState": constants.DeleteStateNodes,
		}).Info("completed node groups deletion")
		cluster.DeleteState = constants.DeleteStateNodes
		c.updateClusterDeleteState(ctx, cluster)
		fallthrough

	case constants.DeleteStateNodes:
		if err := c.deleteSecurityGroups(ctx, token, cluster); err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to delete security groups")
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrSecurityGroupDeleteFailed, "cluster_deletion", err)
		}
		cluster.DeleteState = constants.DeleteStateSecurityGroups
		c.updateClusterDeleteState(ctx, cluster)
		fallthrough

	case constants.DeleteStateSecurityGroups:
		if err := c.deleteApplicationCredentials(ctx, token, cluster); err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to delete application credentials")
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrApplicationCredentialDeleteFailed, "cluster_deletion", err)
		}
		cluster.DeleteState = constants.DeleteStateCredentials
		c.updateClusterDeleteState(ctx, cluster)
		fallthrough

	case constants.DeleteStateCredentials:
		cluster.DeleteState = constants.DeleteStateCompleted
		cluster.ClusterStatus = DeletedClusterStatus
		c.updateClusterDeleteState(ctx, cluster)
		if err := c.CreateAuditLog(ctx, cluster.ClusterUUID, cluster.ClusterProjectUUID, "Cluster Destroyed"); err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to create audit log")
			c.logClusterErrorFiltered(ctx, cluster.ClusterUUID, constants.ErrAuditLogCreateFailed, "cluster_deletion", err)
		}
	}
}

func (c *clusterService) updateClusterDeleteState(ctx context.Context, cluster *model.Cluster) {
	clModel := &model.Cluster{
		DeleteState: cluster.DeleteState,
	}
	if cluster.DeleteState == constants.DeleteStateCompleted {
		clModel.ClusterStatus = DeletedClusterStatus
		clModel.ClusterDeleteDate = time.Now()
	}

	err := c.repository.Cluster().DeleteUpdateCluster(ctx, clModel, cluster.ClusterUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to update cluster delete state")
		c.logClusterError(ctx, cluster.ClusterUUID, fmt.Sprintf("Failed to update cluster delete state: %v", err))
	}
}

func (c *clusterService) deleteLoadBalancerComponents(ctx context.Context, authToken string, cluster *model.Cluster) error {
	token := strings.Clone(authToken)
	getLoadBalancer, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "load_balancer")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get load balancer")
		return err
	}

	if len(getLoadBalancer) == 0 {
		return nil
	}

	pools, err := c.loadbalancerService.GetLoadBalancerPools(ctx, token, getLoadBalancer[0].ResourceUUID)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			c.logger.WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Info("loadbalancer not found, skipping deletion")
			return nil
		}
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get load balancer pools")
		return err
	}

	for _, pool := range pools.Pools {
		err = c.loadbalancerService.DeleteLoadbalancerPools(ctx, token, pool)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"poolID":      pool,
			}).Error("failed to delete pool")
			return err
		}
	}

	listeners, err := c.loadbalancerService.GetLoadBalancerListeners(ctx, token, getLoadBalancer[0].ResourceUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get load balancer listeners")
		return err
	}

	for _, listener := range listeners.Listeners {
		err = c.loadbalancerService.DeleteLoadbalancerListeners(ctx, token, listener)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"listenerID":  listener,
			}).Error("failed to delete listener")
			return err
		}
		// Wait for listener deletion
		err = c.loadbalancerService.CheckLoadBalancerDeletingListeners(ctx, token, listener)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"listenerID":  listener,
			}).Error("failed to check listener deletion status")
			return err
		}
	}

	// Finally delete the loadbalancer
	maxRetries := 10
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := c.loadbalancerService.DeleteLoadbalancer(ctx, token, getLoadBalancer[0].ResourceUUID)
		if err == nil {
			return nil
		}

		if attempt == maxRetries {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID":      cluster.ClusterUUID,
				"loadbalancerUUID": getLoadBalancer[0].ResourceUUID,
				"attempt":          attempt,
			}).Error("failed to delete load balancer after all retries")
			return err
		}

		c.logger.WithFields(logrus.Fields{
			"clusterUUID":      cluster.ClusterUUID,
			"loadbalancerUUID": getLoadBalancer[0].ResourceUUID,
			"attempt":          attempt,
		}).Warn("retrying load balancer deletion")

		time.Sleep(time.Duration(attempt) * 5 * time.Second)
	}

	return nil
}

func (c *clusterService) deleteDNSRecord(ctx context.Context, cluster *model.Cluster) error {
	if cluster.ClusterCloudflareRecordID == "" {
		return nil
	}

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := c.cloudflareService.DeleteDNSRecord(ctx, cluster.ClusterCloudflareRecordID)
		if err == nil {
			return nil
		}

		if attempt == maxRetries {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"recordID":    cluster.ClusterCloudflareRecordID,
				"attempt":     attempt,
			}).Error("failed to delete DNS record after all retries")
			return err
		}

		c.logger.WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
			"recordID":    cluster.ClusterCloudflareRecordID,
			"attempt":     attempt,
		}).Warn("retrying DNS record deletion")

		time.Sleep(time.Duration(attempt) * 5 * time.Second)
	}

	return nil
}

func (c *clusterService) deleteFloatingIP(ctx context.Context, authToken string, cluster *model.Cluster) error {
	token := strings.Clone(authToken)

	getFloatingIP, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "floating_ip")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get floating IP")
		return err
	}

	if len(getFloatingIP) == 0 {
		return nil
	}

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := c.networkService.DeleteFloatingIP(ctx, token, getFloatingIP[0].ResourceUUID)
		if err == nil {
			return nil
		}

		if attempt == maxRetries {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID":    cluster.ClusterUUID,
				"floatingIPUUID": getFloatingIP[0].ResourceUUID,
				"attempt":        attempt,
			}).Error("failed to delete floating IP after all retries")
			return err
		}

		c.logger.WithFields(logrus.Fields{
			"clusterUUID":    cluster.ClusterUUID,
			"floatingIPUUID": getFloatingIP[0].ResourceUUID,
			"attempt":        attempt,
		}).Warn("retrying floating IP deletion")

		time.Sleep(time.Duration(attempt) * 5 * time.Second)
	}

	return nil
}

func (c *clusterService) getServerGroupMembers(ctx context.Context, authToken string, serverGroupID string) ([]string, error) {
	token := strings.Clone(authToken)

	serverGroup, err := c.computeService.GetServerGroup(ctx, token, serverGroupID)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}
	return serverGroup.ServerGroup.Members, nil
}

func (c *clusterService) deleteNodeGroups(ctx context.Context, authToken string, cluster *model.Cluster) error {
	token := strings.Clone(authToken)
	nodeGroup, err := c.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, cluster.ClusterUUID, "", constants.ActiveNodeGroupStatus)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get node groups")
		return err
	}
	getNodeGroups, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "server_group")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get node groups")
		return err
	}

	for _, nodeGroup := range getNodeGroups {
		maxRetries := 10
		for attempt := 1; attempt <= maxRetries; attempt++ {
			members, err := c.getServerGroupMembers(ctx, token, nodeGroup.ResourceUUID)
			if err != nil {
				if strings.Contains(err.Error(), "404") {
					c.logger.WithFields(logrus.Fields{
						"clusterUUID":   cluster.ClusterUUID,
						"nodeGroupUUID": nodeGroup.ResourceUUID,
					}).Info("server group not found, skipping member deletion")
					break
				}
				if attempt == maxRetries {
					return err
				}
				time.Sleep(time.Duration(attempt) * 5 * time.Second)
				continue
			}

			for _, serverID := range members {
				err = c.computeService.DeleteServer(ctx, token, serverID)
				if err != nil {
					if strings.Contains(err.Error(), "404") {
						c.logger.WithFields(logrus.Fields{
							"clusterUUID": cluster.ClusterUUID,
							"serverID":    serverID,
						}).Info("server not found, skipping deletion")
						continue
					}
					c.logger.WithError(err).WithFields(logrus.Fields{
						"clusterUUID": cluster.ClusterUUID,
						"serverID":    serverID,
						"attempt":     attempt,
					}).Error("failed to delete server")
					if attempt == maxRetries {
						return err
					}
					time.Sleep(time.Duration(attempt) * 5 * time.Second)
					continue
				}
			}

			time.Sleep(10 * time.Second)

			err = c.computeService.DeleteServerGroup(ctx, token, nodeGroup.ResourceUUID)
			if err != nil {
				if strings.Contains(err.Error(), "404") {
					c.logger.WithFields(logrus.Fields{
						"clusterUUID":   cluster.ClusterUUID,
						"nodeGroupUUID": nodeGroup.ResourceUUID,
					}).Info("server group not found, skipping deletion")
					break
				}
				if attempt == maxRetries {
					return err
				}
				time.Sleep(time.Duration(attempt) * 5 * time.Second)
				continue
			}

			break
		}
	}

	for _, nodeGroup := range nodeGroup {
		nodeGroup.NodeGroupsStatus = constants.DeletedNodeGroupStatus
		nodeGroup.NodeGroupDeleteDate = time.Now()
		err = c.repository.NodeGroups().UpdateNodeGroups(ctx, &nodeGroup)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to delete node group")
		}
	}
	return nil
}

func (c *clusterService) deleteSecurityGroups(ctx context.Context, authToken string, cluster *model.Cluster) error {
	token := strings.Clone(authToken)

	sgUUIDs := []string{
		cluster.ClusterSharedSecurityGroup,
	}

	getSecurityGroups, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "security_group")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get security groups")
		return err
	}
	for _, nodeGroup := range getSecurityGroups {
		sgUUIDs = append(sgUUIDs, nodeGroup.ResourceUUID)
	}

	ports := []resource.NetworkPortsResponse{}

	for _, sgUUID := range sgUUIDs {
		tempPorts, err := c.networkService.GetSecurityGroupPorts(ctx, token, sgUUID)
		if err != nil {
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
			}).Error("failed to get security group ports")
			return err
		}

		ports = append(ports, tempPorts)
	}

	for _, port := range ports {
		for _, portID := range port.Ports {
			err = c.networkService.DeleteNetworkPort(ctx, token, portID)
			if err != nil && !strings.Contains(err.Error(), "404") {
				c.logger.WithError(err).WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
					"portID":      portID,
				}).Error("failed to delete port")
				return err
			}
		}
	}

	time.Sleep(30 * time.Second)

	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		var lastErr error
		successCount := 0

		for _, sgUUID := range sgUUIDs {
			err := c.networkService.DeleteSecurityGroup(ctx, token, sgUUID)
			if err == nil {
				successCount++
				continue
			}

			if strings.Contains(err.Error(), "404") {
				c.logger.WithFields(logrus.Fields{
					"clusterUUID": cluster.ClusterUUID,
					"sgUUID":      sgUUID,
				}).Info("security group not found, skipping deletion")
				continue
			}

			lastErr = err
			c.logger.WithError(err).WithFields(logrus.Fields{
				"clusterUUID": cluster.ClusterUUID,
				"sgUUID":      sgUUID,
				"attempt":     attempt,
			}).Error("failed to delete security group")
		}

		if successCount == len(sgUUIDs) {
			return nil
		}

		if attempt == maxRetries {
			return fmt.Errorf("failed to delete security groups after %d attempts, last error: %v", maxRetries, lastErr)
		}

		c.logger.WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
			"attempt":     attempt,
		}).Warn("retrying security group deletion")

		time.Sleep(time.Duration(attempt) * 5 * time.Second)
	}

	return nil
}

func (c *clusterService) deleteApplicationCredentials(ctx context.Context, authToken string, cluster *model.Cluster) error {
	token := strings.Clone(authToken)

	err := c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to check auth token")
		return err
	}

	getApplicationCredential, err := c.repository.Resources().GetResourceByClusterUUID(ctx, cluster.ClusterUUID, "application_credential")
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to get application credential")
		return err
	}
	err = c.identityService.DeleteApplicationCredential(ctx, token, getApplicationCredential[0].ResourceUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).Error("failed to delete application credential")
		return err
	}
	return nil
}

func (c *clusterService) GetKubeConfig(ctx context.Context, authToken, clusterID string) (resource.GetKubeConfigResponse, error) {
	token := strings.Clone(authToken)

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

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
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
	token := strings.Clone(authToken)

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

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
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

func (c *clusterService) UpdateKubeConfig(ctx context.Context, authToken string, clusterID string, req request.UpdateKubeconfigRequest) (resource.UpdateKubeconfigResponse, error) {
	token := strings.Clone(authToken)

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.UpdateKubeconfigResponse{}, err
	}

	if cluster == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.UpdateKubeconfigResponse{}, fmt.Errorf("failed to get cluster")
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.UpdateKubeconfigResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		return resource.UpdateKubeconfigResponse{}, err
	}

	if !IsValidBase64(req.KubeConfig) {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to update kube config, invalid kube config")
		return resource.UpdateKubeconfigResponse{}, fmt.Errorf("failed to update kube config, invalid kube config")
	}

	err = c.repository.Kubeconfig().UpdateKubeconfig(ctx, cluster.ClusterUUID, req.KubeConfig)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to update kube config")
		return resource.UpdateKubeconfigResponse{}, err
	}

	return resource.UpdateKubeconfigResponse{
		ClusterUUID: cluster.ClusterUUID,
	}, nil
}

func (c *clusterService) UpdateCluster(ctx context.Context, authToken, clusterID string, req request.UpdateClusterRequest) (resource.UpdateClusterResponse, error) {
	token := strings.Clone(authToken)

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.UpdateClusterResponse{}, err
	}

	if cluster == nil {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.UpdateClusterResponse{}, fmt.Errorf("failed to get cluster")
	}

	if cluster.ClusterProjectUUID == "" {
		c.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return resource.UpdateClusterResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = c.identityService.CheckAuthToken(ctx, token, cluster.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		return resource.UpdateClusterResponse{}, err
	}

	cluster.ClusterName = req.ClusterName
	cluster.ClusterVersion = req.ClusterVersion
	cluster.ClusterStatus = req.ClusterStatus
	cluster.ClusterAPIAccess = req.ClusterAPIAccess
	cluster.ClusterCertificateExpireDate = req.ClusterCertificateExpireDate

	err = c.repository.Cluster().UpdateCluster(ctx, cluster)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to update cluster")
		return resource.UpdateClusterResponse{}, err
	}

	return resource.UpdateClusterResponse{
		ClusterUUID: cluster.ClusterUUID,
	}, nil
}

func (c *clusterService) GetClusterErrors(ctx context.Context, authToken, clusterID string) ([]resource.GetClusterErrorsResponse, error) {
	token := strings.Clone(authToken)

	cl, clErr := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if clErr != nil {
		c.logger.WithError(clErr).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return nil, clErr
	}

	err := c.identityService.CheckAuthToken(ctx, token, cl.ClusterProjectUUID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to check auth token")
		return nil, err
	}

	cluster, err := c.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster")
		return nil, err
	}

	if cluster == nil {
		return nil, fmt.Errorf("cluster not found")
	}

	errors, err := c.repository.Error().GetErrorsByClusterUUID(ctx, clusterID)
	if err != nil {
		c.logger.WithError(err).WithFields(logrus.Fields{
			"clusterUUID": clusterID,
		}).Error("failed to get cluster errors")
		return nil, err
	}

	response := []resource.GetClusterErrorsResponse{}
	for _, error := range errors {
		response = append(response, resource.GetClusterErrorsResponse{
			ClusterUUID:  error.ClusterUUID,
			ErrorMessage: error.ErrorMessage,
			CreatedAt:    error.CreatedAt,
		})
	}

	return response, nil
}
