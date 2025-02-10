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

type INodeGroupsService interface {
	GetNodeGroups(ctx context.Context, authToken, clusterID, nodeGroupID string) ([]resource.NodeGroup, error)
	GetNodeGroupsByClusterUUID(ctx context.Context, clusterUUID string) ([]resource.NodeGroup, error)
	UpdateNodeGroups(ctx context.Context, authToken, clusterID, nodeGroupID string, req request.UpdateNodeGroupRequest) (resource.UpdateNodeGroupResponse, error)
	AddNode(ctx context.Context, authToken string, clusterUUID, nodeGroupUUID string) (resource.AddNodeResponse, error)
	DeleteNode(ctx context.Context, authToken, clusterID, nodeGroupID, instanceName string) (resource.DeleteNodeResponse, error)
	CreateNodeGroup(ctx context.Context, authToken, clusterID string, req request.CreateNodeGroupRequest) (resource.CreateNodeGroupResponse, error)
	DeleteNodeGroup(ctx context.Context, authToken, clusterID, nodeGroupID string) error
}

type nodeGroupsService struct {
	repository      repository.IRepository
	logger          *logrus.Logger
	identityService IIdentityService
	computeService  IComputeService
	networkService  INetworkService
}

func NewNodeGroupsService(logger *logrus.Logger, repository repository.IRepository, i IIdentityService, c IComputeService, n INetworkService) INodeGroupsService {
	return &nodeGroupsService{
		repository:      repository,
		logger:          logger,
		identityService: i,
		computeService:  c,
		networkService:  n,
	}
}

func (nodg *nodeGroupsService) GetNodeGroups(ctx context.Context, authToken, clusterID, nodeGroupID string) ([]resource.NodeGroup, error) {
	clusterProjectUUID, err := nodg.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterID": clusterID,
		}).WithError(err).Error("failed to get cluster by uuid")
		return nil, err
	}
	err = nodg.identityService.CheckAuthToken(ctx, authToken, clusterProjectUUID.ClusterProjectUUID)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to check auth token")
		return nil, err
	}

	if nodeGroupID != "" {
		nodeGroup, err := nodg.repository.NodeGroups().GetNodeGroupByUUID(ctx, nodeGroupID)
		if err != nil {
			nodg.logger.WithFields(logrus.Fields{
				"nodeGroupID": nodeGroupID,
			}).WithError(err).Error("failed to get node group by uuid")
			return nil, err
		}
		count, err := nodg.computeService.GetCountOfServerFromServerGroup(ctx, authToken, nodeGroup.NodeGroupUUID, clusterProjectUUID.ClusterProjectUUID)
		if err != nil {
			nodg.logger.WithError(err).Error("failed to check current node size")
			return nil, err
		}

		var resp []resource.NodeGroup
		resp = append(resp, resource.NodeGroup{
			ClusterUUID:      nodeGroup.ClusterUUID,
			NodeGroupUUID:    nodeGroup.NodeGroupUUID,
			NodeGroupName:    nodeGroup.NodeGroupName,
			NodeGroupMinSize: nodeGroup.NodeGroupMinSize,
			NodeGroupMaxSize: nodeGroup.NodeGroupMaxSize,
			NodeDiskSize:     nodeGroup.NodeDiskSize,
			NodeFlavorUUID:   nodeGroup.NodeFlavorUUID,
			NodeGroupsType:   nodeGroup.NodeGroupsType,
			CurrentNodes:     count,
			NodeGroupsStatus: nodeGroup.NodeGroupsStatus,
		})
		return resp, nil
	} else {
		nodeGroups, err := nodg.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, clusterID, "")
		if err != nil {
			nodg.logger.WithFields(logrus.Fields{
				"clusterID": clusterID,
			}).WithError(err).Error("failed to get node groups by cluster uuid")
			return nil, err
		}
		var resp []resource.NodeGroup
		for _, nodeGroup := range nodeGroups {
			count, err := nodg.computeService.GetCountOfServerFromServerGroup(ctx, authToken, nodeGroup.NodeGroupUUID, clusterProjectUUID.ClusterProjectUUID)
			if err != nil {
				nodg.logger.WithError(err).Error("failed to check current node size")
				return nil, err
			}

			resp = append(resp, resource.NodeGroup{
				ClusterUUID:      nodeGroup.ClusterUUID,
				NodeGroupUUID:    nodeGroup.NodeGroupUUID,
				NodeGroupName:    nodeGroup.NodeGroupName,
				NodeGroupMinSize: nodeGroup.NodeGroupMinSize,
				NodeGroupMaxSize: nodeGroup.NodeGroupMaxSize,
				NodeDiskSize:     nodeGroup.NodeDiskSize,
				NodeFlavorUUID:   nodeGroup.NodeFlavorUUID,
				NodeGroupsType:   nodeGroup.NodeGroupsType,
				CurrentNodes:     count,
				NodeGroupsStatus: nodeGroup.NodeGroupsStatus,
			})
		}
		return resp, nil
	}
}

func (nodg *nodeGroupsService) GetNodeGroupsByClusterUUID(ctx context.Context, clusterUUID string) ([]resource.NodeGroup, error) {
	nodeGroups, err := nodg.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, clusterUUID, "")
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterUUID": clusterUUID,
		}).WithError(err).Error("failed to get node groups by cluster uuid")
		return nil, err
	}

	var resp []resource.NodeGroup

	for _, nodeGroup := range nodeGroups {
		resp = append(resp, resource.NodeGroup{
			ClusterUUID:      nodeGroup.ClusterUUID,
			NodeGroupUUID:    nodeGroup.NodeGroupUUID,
			NodeGroupName:    nodeGroup.NodeGroupName,
			NodeGroupMinSize: nodeGroup.NodeGroupMinSize,
			NodeGroupMaxSize: nodeGroup.NodeGroupMaxSize,
			NodeDiskSize:     nodeGroup.NodeDiskSize,
			NodeFlavorUUID:   nodeGroup.NodeFlavorUUID,
			NodeGroupsType:   nodeGroup.NodeGroupsType,
			CurrentNodes:     0, //ToDo: Keep current node count in db
			NodeGroupsStatus: nodeGroup.NodeGroupsStatus,
		})

	}

	return resp, nil
}

func (nodg *nodeGroupsService) AddNode(ctx context.Context, authToken string, clusterUUID, nodeGroupUUD string) (resource.AddNodeResponse, error) {
	if authToken == "" {
		nodg.logger.WithFields(logrus.Fields{
			"clusterID": clusterUUID,
		}).Error("failed to get cluster")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to get cluster")
	}

	cluster, err := nodg.repository.Cluster().GetClusterByUUID(ctx, clusterUUID)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to get cluster")
		return resource.AddNodeResponse{}, err
	}

	if cluster == nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterID": clusterUUID,
		}).Error("failed to get cluster")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to get cluster")
	}

	if cluster.ClusterProjectUUID == "" {
		nodg.logger.WithFields(logrus.Fields{
			"clusterID": clusterUUID,
		}).Error("failed to get cluster")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = nodg.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to check auth token")
		return resource.AddNodeResponse{}, err
	}

	nodeGroup, err := nodg.repository.NodeGroups().GetNodeGroupByUUID(ctx, nodeGroupUUD)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to get node group by uuid")
		return resource.AddNodeResponse{}, err
	}

	if nodeGroup.NodeGroupsStatus != NodeGroupActiveStatus {
		nodg.logger.Error("failed to get node groups")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to get node groups")
	}

	currentCount, err := nodg.computeService.GetCountOfServerFromServerGroup(ctx, authToken, nodeGroup.NodeGroupUUID, cluster.ClusterProjectUUID)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to get count of server from server group")
		return resource.AddNodeResponse{}, err
	}

	if currentCount >= nodeGroup.NodeGroupMaxSize {
		nodg.logger.Error("failed to add node, node group max size reached")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to add node, node group max size reached")
	}

	subnetIDs := []string{}
	err = json.Unmarshal(cluster.ClusterSubnets, &subnetIDs)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to unmarshal cluster subnets")
		return resource.AddNodeResponse{}, err
	}

	networkIDResp, err := nodg.networkService.GetNetworkID(ctx, authToken, subnetIDs[0])
	if err != nil {
		nodg.logger.WithError(err).Error("failed to get networkId")
		return resource.AddNodeResponse{}, err
	}

	randSubnetId := GetRandomStringFromArray(subnetIDs)

	createPortRequest := request.CreateNetworkPortRequest{
		Port: request.Port{
			Name:         fmt.Sprintf("%s-%s", cluster.ClusterName, nodeGroup.NodeGroupName),
			NetworkID:    networkIDResp.Subnet.NetworkID,
			AdminStateUp: true,
			FixedIps: []request.FixedIp{
				{
					SubnetID: randSubnetId,
				},
			},
			SecurityGroups: []string{cluster.ClusterSharedSecurityGroup, nodeGroup.NodeGroupSecurityGroup},
		},
	}

	portResp, err := nodg.networkService.CreateNetworkPort(ctx, authToken, createPortRequest)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to create network port")
		return resource.AddNodeResponse{}, err
	}

	nodeGroupLabelsArr := []string{}
	if nodeGroup.NodeGroupLabels != nil {
		err = json.Unmarshal(nodeGroup.NodeGroupLabels, &nodeGroupLabelsArr)
		if err != nil {
			nodg.logger.WithError(err).Error("failed to unmarshal node group labels")
			return resource.AddNodeResponse{}, err
		}
	}
	rke2InitScript, err := GenerateUserDataFromTemplate("false",
		WorkerServerType,
		cluster.ClusterRegisterToken,
		cluster.ClusterEndpoint,
		cluster.ClusterVersion,
		cluster.ClusterName,
		cluster.ClusterUUID,
		"",
		config.GlobalConfig.GetWebConfig().Endpoint,
		authToken,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		strings.Join(nodeGroupLabelsArr, ","),
		"",
		"",
		"",
		"",
		"",
		"",
	)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to generate user data from template")
		return resource.AddNodeResponse{}, err
	}

	sharedSecurityGroup, err := nodg.networkService.GetSecurityGroupByID(ctx, authToken, cluster.ClusterSharedSecurityGroup)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to get sharedSecurityGroup")
		return resource.AddNodeResponse{}, err
	}
	nodeSecurityGroup, err := nodg.networkService.GetSecurityGroupByID(ctx, authToken, nodeGroup.NodeGroupSecurityGroup)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to get nodeSecurityGroup")
		return resource.AddNodeResponse{}, err
	}

	createServerRequest := request.CreateComputeRequest{
		Server: request.Server{
			Name:             nodeGroup.NodeGroupName + "-" + uuid.New().String(),
			ImageRef:         config.GlobalConfig.GetImageRefConfig().ImageRef,
			FlavorRef:        nodeGroup.NodeFlavorUUID,
			KeyName:          cluster.ClusterNodeKeypairName,
			AvailabilityZone: "nova",
			BlockDeviceMappingV2: []request.BlockDeviceMappingV2{
				{
					BootIndex:           0,
					DestinationType:     "volume",
					DeleteOnTermination: true,
					SourceType:          "image",
					UUID:                config.GlobalConfig.GetImageRefConfig().ImageRef,
					VolumeSize:          nodeGroup.NodeDiskSize,
				},
			},
			Networks: []request.Networks{
				{Port: portResp.Port.ID},
			},
			SecurityGroups: []request.SecurityGroups{
				{Name: sharedSecurityGroup.SecurityGroup.ID},
				{Name: nodeSecurityGroup.SecurityGroup.ID},
			},
			UserData: Base64Encoder(rke2InitScript),
		},
		SchedulerHints: request.SchedulerHints{
			Group: nodeGroup.NodeGroupUUID,
		},
	}

	serverResp, err := nodg.computeService.CreateCompute(ctx, authToken, createServerRequest)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to create compute")
		return resource.AddNodeResponse{}, err
	}

	err = nodg.repository.AuditLog().CreateAuditLog(ctx, &model.AuditLog{
		ClusterUUID: cluster.ClusterUUID,
		ProjectUUID: cluster.ClusterProjectUUID,
		Event:       fmt.Sprintf("Node %s added to cluster", nodeGroup.NodeGroupName),
		CreateDate:  time.Now(),
	})
	if err != nil {
		nodg.logger.WithError(err).Error("failed to create audit log")
		return resource.AddNodeResponse{}, err
	}
	err = nodg.repository.NodeGroups().UpdateNodeGroups(ctx, &model.NodeGroups{
		NodeGroupUpdateDate: time.Now(),
		NodeGroupUUID:       nodeGroup.NodeGroupUUID,
	})
	if err != nil {
		nodg.logger.WithError(err).Error("failed to update node group")
		return resource.AddNodeResponse{}, err
	}

	return resource.AddNodeResponse{
		NodeGroupID: nodeGroup.NodeGroupUUID,
		ComputeID:   serverResp.Server.ID,
		ClusterID:   cluster.ClusterUUID,
		MinSize:     nodeGroup.NodeGroupMinSize,
		MaxSize:     nodeGroup.NodeGroupMaxSize,
	}, nil
}
func (nodg *nodeGroupsService) DeleteNode(ctx context.Context, authToken string, clusterUUID string, nodeGroupID string, instanceName string) (resource.DeleteNodeResponse, error) {
	if authToken == "" {
		nodg.logger.WithFields(logrus.Fields{
			"clusterID": clusterUUID,
		}).Error("failed to get cluster")
		return resource.DeleteNodeResponse{}, fmt.Errorf("failed to get cluster")
	}

	cluster, err := nodg.repository.Cluster().GetClusterByUUID(ctx, clusterUUID)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to get cluster")
		return resource.DeleteNodeResponse{}, err
	}
	if cluster == nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterID": clusterUUID,
		}).Error("failed to get cluster")
		return resource.DeleteNodeResponse{}, fmt.Errorf("failed to get cluster")
	}
	if cluster.ClusterProjectUUID == "" {
		nodg.logger.WithFields(logrus.Fields{
			"clusterID": clusterUUID,
		}).Error("failed to get cluster")
		return resource.DeleteNodeResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = nodg.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to check auth token")
		return resource.DeleteNodeResponse{}, err
	}

	ng, err := nodg.repository.NodeGroups().GetNodeGroupByUUID(ctx, nodeGroupID)
	if err != nil {
		nodg.logger.WithError(err).Error("failed to get node group")
		return resource.DeleteNodeResponse{}, err
	}
	if ng == nil {
		nodg.logger.Error("failed to get node group")
		return resource.DeleteNodeResponse{}, fmt.Errorf("failed to get node group")
	}

	compute, err := nodg.computeService.GetInstances(ctx, authToken, ng.NodeGroupUUID)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"nodeGroupUUID": ng.NodeGroupUUID,
		}).WithError(err).Error("failed to get instances")
		return resource.DeleteNodeResponse{}, err

	}
	computeCount, err := nodg.computeService.GetCountOfServerFromServerGroup(ctx, authToken, ng.NodeGroupUUID, cluster.ClusterProjectUUID)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"nodeGroupUUID": ng.NodeGroupUUID,
		}).WithError(err).Error("failed to get count of server from server group")
		return resource.DeleteNodeResponse{}, err
	}
	if computeCount <= ng.NodeGroupMinSize {
		nodg.logger.WithFields(logrus.Fields{
			"nodeGroupUUID": ng.NodeGroupUUID,
		}).WithError(err).Error("failed to delete node, node group min size reached")
		return resource.DeleteNodeResponse{}, fmt.Errorf("failed to delete node, node group min size reached")
	}
	for _, server := range compute {
		if server.InstanceName == instanceName {
			getPortIDs, err := nodg.networkService.GetComputeNetworkPorts(ctx, authToken, server.InstanceUUID)
			if err != nil {
				nodg.logger.WithFields(logrus.Fields{
					"instanceUUID": server.InstanceUUID,
				}).WithError(err).Error("failed to get compute network ports")
				return resource.DeleteNodeResponse{}, err
			}
			err = nodg.computeService.DeleteCompute(ctx, authToken, server.InstanceUUID)
			if err != nil {
				nodg.logger.WithFields(logrus.Fields{
					"instanceUUID": server.InstanceUUID,
				}).WithError(err).Error("failed to delete compute")
				return resource.DeleteNodeResponse{}, err
			}
			for _, portID := range getPortIDs.Ports {
				err = nodg.networkService.DeleteNetworkPort(ctx, authToken, portID)
				if err != nil {
					nodg.logger.WithFields(logrus.Fields{
						"portID": portID,
					}).WithError(err).Error("failed to delete network port")
					return resource.DeleteNodeResponse{}, err
				}
			}
		}
	}

	err = nodg.repository.AuditLog().CreateAuditLog(ctx, &model.AuditLog{
		ClusterUUID: cluster.ClusterUUID,
		ProjectUUID: cluster.ClusterProjectUUID,
		Event:       fmt.Sprintf("Node %s deleted from cluster", ng.NodeGroupName),
		CreateDate:  time.Now(),
	})
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).WithError(err).Error("failed to create audit log")
		return resource.DeleteNodeResponse{}, err
	}
	return resource.DeleteNodeResponse{
		NodeGroupID: ng.NodeGroupUUID,
		ClusterID:   cluster.ClusterUUID,
	}, nil

}

func (nodg *nodeGroupsService) UpdateNodeGroups(ctx context.Context, authToken, clusterID, nodeGroupID string, req request.UpdateNodeGroupRequest) (resource.UpdateNodeGroupResponse, error) {
	clusterProjectUUID, err := nodg.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterID": clusterID,
		}).WithError(err).Error("failed to get cluster by uuid")
		return resource.UpdateNodeGroupResponse{}, err
	}
	getCurrentStateOfNodeGroup, err := nodg.repository.NodeGroups().GetNodeGroupByUUID(ctx, nodeGroupID)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"nodeGroupID": nodeGroupID,
		}).WithError(err).Error("failed to get node group by uuid")
		return resource.UpdateNodeGroupResponse{}, err
	}
	err = nodg.identityService.CheckAuthToken(ctx, authToken, clusterProjectUUID.ClusterProjectUUID)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterProjectUUID": clusterProjectUUID.ClusterProjectUUID,
		}).WithError(err).Error("failed to check auth token")
		return resource.UpdateNodeGroupResponse{}, err
	}

	err = nodg.repository.NodeGroups().UpdateNodeGroups(ctx, &model.NodeGroups{
		NodeGroupUUID:    nodeGroupID,
		NodeGroupMinSize: int(*req.MinNodes),
		NodeGroupMaxSize: int(*req.MaxNodes),
	})
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"nodeGroupID": nodeGroupID,
		}).WithError(err).Error("failed to update node group")
		return resource.UpdateNodeGroupResponse{}, err
	}
	response := resource.UpdateNodeGroupResponse{
		ClusterID:   clusterID,
		NodeGroupID: nodeGroupID,
		MinSize:     int(*req.MinNodes),
		MaxSize:     int(*req.MaxNodes),
		Status:      getCurrentStateOfNodeGroup.NodeGroupsStatus,
	}
	return response, nil
}
func (nodg *nodeGroupsService) CreateNodeGroup(ctx context.Context, authToken, clusterID string, req request.CreateNodeGroupRequest) (resource.CreateNodeGroupResponse, error) {
	if len(req.NodeGroupName) > 26 {
		return resource.CreateNodeGroupResponse{}, fmt.Errorf("node group name is too long")
	}
	cluster, err := nodg.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterID": clusterID,
		}).WithError(err).Error("failed to get cluster by uuid")
		return resource.CreateNodeGroupResponse{}, err
	}

	if cluster == nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterID": clusterID,
		}).Error("failed to get cluster")
		return resource.CreateNodeGroupResponse{}, fmt.Errorf("failed to get cluster")
	}

	if cluster.ClusterProjectUUID == "" {
		nodg.logger.WithFields(logrus.Fields{
			"clusterID": clusterID,
		}).Error("failed to get cluster")
		return resource.CreateNodeGroupResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = nodg.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterProjectUUID": cluster.ClusterProjectUUID,
		}).WithError(err).Error("failed to check auth token")
		return resource.CreateNodeGroupResponse{}, err
	}

	nodeGroupLabelsJSON, err := json.Marshal(req.NodeGroupLabels)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"nodeGroupLabels": req.NodeGroupLabels,
		}).WithError(err).Error("failed to marshal node group labels")
		return resource.CreateNodeGroupResponse{}, err
	}

	createServerGroupReq := request.CreateServerGroupRequest{
		ServerGroup: request.ServerGroup{
			Name:   fmt.Sprintf("%v-%v-worker-server-group", cluster.ClusterName, req.NodeGroupName),
			Policy: "soft-anti-affinity",
		},
	}

	serverGroupResp, err := nodg.computeService.CreateServerGroup(ctx, authToken, createServerGroupReq)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterName":   cluster.ClusterName,
			"nodeGroupName": req.NodeGroupName,
		}).WithError(err).Error("failed to create server group")
		return resource.CreateNodeGroupResponse{}, err
	}

	createSecurityGroupReq := &request.CreateSecurityGroupRequest{
		SecurityGroup: request.SecurityGroup{
			Name:        fmt.Sprintf("%v-%v-worker-sg", cluster.ClusterName, req.NodeGroupName),
			Description: fmt.Sprintf("%v-%v-worker-sg", cluster.ClusterName, req.NodeGroupName),
		},
	}

	securityGroupResp, err := nodg.networkService.CreateSecurityGroup(ctx, authToken, *createSecurityGroupReq)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterName":   cluster.ClusterName,
			"nodeGroupName": req.NodeGroupName,
		}).WithError(err).Error("failed to create security group")
		return resource.CreateNodeGroupResponse{}, err
	}

	rke2WorkerInitScript, err := GenerateUserDataFromTemplate("false",
		WorkerServerType,
		cluster.ClusterRegisterToken,
		cluster.ClusterEndpoint,
		cluster.ClusterVersion,
		cluster.ClusterName,
		clusterID,
		"",
		config.GlobalConfig.GetWebConfig().Endpoint,
		authToken,
		config.GlobalConfig.GetVkeAgentConfig().VkeAgentVersion,
		strings.Join(req.NodeGroupLabels, ","),
		"",
		"",
		"",
		"",
		"",
		"",
	)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterName":   cluster.ClusterName,
			"nodeGroupName": req.NodeGroupName,
		}).WithError(err).Error("failed to generate user data from template")
		return resource.CreateNodeGroupResponse{}, err
	}

	//Get Cluster Shared Security Group
	getClusterSharedSecurityGroup, err := nodg.networkService.GetSecurityGroupByID(ctx, authToken, cluster.ClusterSharedSecurityGroup)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterName":   cluster.ClusterName,
			"nodeGroupName": req.NodeGroupName,
		}).WithError(err).Error("failed to get cluster shared security group")
		return resource.CreateNodeGroupResponse{}, err
	}

	WorkerRequest := &request.CreateComputeRequest{
		Server: request.Server{
			Name:             "ServerName",
			ImageRef:         config.GlobalConfig.GetImageRefConfig().ImageRef,
			FlavorRef:        req.NodeFlavorUUID,
			KeyName:          cluster.ClusterNodeKeypairName,
			AvailabilityZone: "nova",
			SecurityGroups: []request.SecurityGroups{
				{Name: securityGroupResp.SecurityGroup.Name},
				{Name: getClusterSharedSecurityGroup.SecurityGroup.Name},
			},
			BlockDeviceMappingV2: []request.BlockDeviceMappingV2{
				{
					BootIndex:           0,
					DestinationType:     "volume",
					DeleteOnTermination: true,
					SourceType:          "image",
					UUID:                config.GlobalConfig.GetImageRefConfig().ImageRef,
					VolumeSize:          req.NodeDiskSize,
				},
			},
			UserData: Base64Encoder(rke2WorkerInitScript),
		},
		SchedulerHints: request.SchedulerHints{
			Group: serverGroupResp.ServerGroup.ID,
		},
	}

	subnetIDSArr := []string{}
	err = json.Unmarshal(cluster.ClusterSubnets, &subnetIDSArr)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterName": cluster.ClusterName,
		}).WithError(err).Error("failed to unmarshal cluster subnets")
		return resource.CreateNodeGroupResponse{}, err
	}

	getNetworkIdResp, err := nodg.networkService.GetNetworkID(ctx, authToken, subnetIDSArr[0])
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterName": cluster.ClusterName,
		}).WithError(err).Error("failed to get network id")
		return resource.CreateNodeGroupResponse{}, err
	}

	for i := 1; i <= req.NodeGroupMinSize; i++ {
		randSubnetId := GetRandomStringFromArray(subnetIDSArr)
		portRequest := &request.CreateNetworkPortRequest{
			Port: request.Port{
				NetworkID:    getNetworkIdResp.Subnet.NetworkID,
				Name:         fmt.Sprintf("%v-%s-port", cluster.ClusterName, req.NodeGroupName),
				AdminStateUp: true,
				FixedIps: []request.FixedIp{
					{
						SubnetID: randSubnetId,
					},
				},
				SecurityGroups: []string{securityGroupResp.SecurityGroup.ID, getClusterSharedSecurityGroup.SecurityGroup.ID},
			},
		}
		portResp, err := nodg.networkService.CreateNetworkPort(ctx, authToken, *portRequest)
		if err != nil {
			nodg.logger.WithFields(logrus.Fields{
				"clusterName":   cluster.ClusterName,
				"nodeGroupName": req.NodeGroupName,
			}).WithError(err).Error("failed to create network port")
			return resource.CreateNodeGroupResponse{}, err
		}
		WorkerRequest.Server.Networks = []request.Networks{
			{Port: portResp.Port.ID},
		}
		WorkerRequest.Server.Name = fmt.Sprintf("%s-%s", req.NodeGroupName, uuid.New().String())

		_, err = nodg.computeService.CreateCompute(ctx, authToken, *WorkerRequest)
		if err != nil {
			return resource.CreateNodeGroupResponse{}, err
		}
	}

	err = nodg.repository.NodeGroups().CreateNodeGroups(ctx, &model.NodeGroups{
		NodeGroupUUID:          serverGroupResp.ServerGroup.ID,
		ClusterUUID:            cluster.ClusterUUID,
		NodeGroupName:          req.NodeGroupName,
		NodeFlavorUUID:         req.NodeFlavorUUID,
		NodeDiskSize:           req.NodeDiskSize,
		NodeGroupLabels:        nodeGroupLabelsJSON,
		NodeGroupMinSize:       req.NodeGroupMinSize,
		NodeGroupMaxSize:       req.NodeGroupMaxSize,
		NodeGroupsType:         NodeGroupWorkerType,
		NodeGroupSecurityGroup: securityGroupResp.SecurityGroup.ID,
		NodeGroupsStatus:       NodeGroupActiveStatus,
		IsHidden:               false,
		NodeGroupCreateDate:    time.Now(),
	})

	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterName":   cluster.ClusterName,
			"nodeGroupName": req.NodeGroupName,
		}).WithError(err).Error("failed to create node group")
		return resource.CreateNodeGroupResponse{}, err
	}

	err = nodg.repository.AuditLog().CreateAuditLog(ctx, &model.AuditLog{
		ClusterUUID: cluster.ClusterUUID,
		ProjectUUID: cluster.ClusterProjectUUID,
		Event:       fmt.Sprintf("Node group %s created", req.NodeGroupName),
		CreateDate:  time.Now(),
	})

	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterName":   cluster.ClusterName,
			"nodeGroupName": req.NodeGroupName,
		}).WithError(err).Error("failed to create audit log")
		return resource.CreateNodeGroupResponse{}, err
	}

	return resource.CreateNodeGroupResponse{
		ClusterID:   cluster.ClusterUUID,
		NodeGroupID: serverGroupResp.ServerGroup.ID,
	}, nil
}

func (nodg *nodeGroupsService) DeleteNodeGroup(ctx context.Context, authToken, clusterID, nodeGroupID string) error {
	cluster, err := nodg.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterID": clusterID,
		}).Error("failed to get cluster")
		return err
	}
	if cluster.ClusterProjectUUID == "" {
		nodg.logger.WithFields(logrus.Fields{
			"clusterID": clusterID,
		}).Error("failed to get cluster")
		return fmt.Errorf("failed to get cluster")
	}
	err = nodg.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		nodg.logger.Error("failed to check auth token")
		return err
	}
	nodeGroup, err := nodg.repository.NodeGroups().GetNodeGroupByUUID(ctx, nodeGroupID)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"nodeGroupID": nodeGroupID,
		}).WithError(err).Error("failed to get node group")
		return err
	}
	computes, err := nodg.computeService.GetInstances(ctx, authToken, nodeGroup.NodeGroupUUID)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"nodeGroupUUID": nodeGroup.NodeGroupUUID,
		}).WithError(err).Error("failed to get instances")
		return err
	}
	for _, server := range computes {
		getNetworkPortID, err := nodg.networkService.GetComputeNetworkPorts(ctx, authToken, server.InstanceUUID)
		if err != nil {
			nodg.logger.WithFields(logrus.Fields{
				"instanceUUID": server.InstanceUUID,
			}).WithError(err).Error("failed to get compute network ports")
			return err
		}
		for _, portID := range getNetworkPortID.Ports {
			err := nodg.networkService.DeleteNetworkPort(ctx, authToken, portID)
			if err != nil {
				nodg.logger.WithFields(logrus.Fields{
					"portID": portID,
				}).WithError(err).Error("failed to delete network port")
				return err
			}
		}
		err = nodg.computeService.DeleteCompute(ctx, authToken, server.InstanceUUID)
		if err != nil {
			nodg.logger.WithFields(logrus.Fields{
				"instanceUUID": server.InstanceUUID,
			}).WithError(err).Error("failed to delete compute")
			return err
		}
	}
	err = nodg.computeService.DeleteServerGroup(ctx, authToken, nodeGroup.NodeGroupUUID)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"nodeGroupUUID": nodeGroup.NodeGroupUUID,
		}).WithError(err).Error("failed to delete server group")
		return err
	}
	err = nodg.networkService.DeleteSecurityGroup(ctx, authToken, nodeGroup.NodeGroupSecurityGroup)
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"nodeGroupSecurityGroup": nodeGroup.NodeGroupSecurityGroup,
		}).WithError(err).Error("failed to delete security group")
		return err
	}

	err = nodg.repository.NodeGroups().UpdateNodeGroups(ctx, &model.NodeGroups{
		NodeGroupUUID:       nodeGroupID,
		NodeGroupsStatus:    NodeGroupDeletedStatus,
		IsHidden:            true,
		NodeGroupUpdateDate: time.Now(),
	})
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"nodeGroupID": nodeGroupID,
		}).WithError(err).Error("failed to update node group")
		return err
	}

	err = nodg.repository.AuditLog().CreateAuditLog(ctx, &model.AuditLog{
		ClusterUUID: cluster.ClusterUUID,
		ProjectUUID: cluster.ClusterProjectUUID,
		Event:       fmt.Sprintf("Node group %s deleted", nodeGroup.NodeGroupName),
		CreateDate:  time.Now(),
	})
	if err != nil {
		nodg.logger.WithFields(logrus.Fields{
			"clusterUUID": cluster.ClusterUUID,
		}).WithError(err).Error("failed to create audit log")
		return err
	}

	return nil

}
