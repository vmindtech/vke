package service

import (
	"context"
	"encoding/json"
	"fmt"
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
	UpdateNodeGroups(ctx context.Context, authToken, clusterID, nodeGroupID string, req resource.UpdateNodeGroupRequest) (resource.UpdateNodeGroupResponse, error)
	AddNode(ctx context.Context, authToken string, req request.AddNodeRequest) (resource.AddNodeResponse, error)
	DeleteNode(ctx context.Context, authToken, clusterID, nodeID string) (resource.DeleteNodeResponse, error)
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
		nodg.logger.Errorf("failed to get cluster project uuid by cluster uuid %s, err: %v", clusterID, err)
		return nil, err
	}
	err = nodg.identityService.CheckAuthToken(ctx, authToken, clusterProjectUUID.ClusterProjectUUID)
	if err != nil {
		nodg.logger.Errorf("failed to check auth token, err: %v", err)
		return nil, err
	}

	if nodeGroupID != "" {
		nodeGroup, err := nodg.repository.NodeGroups().GetNodeGroupByUUID(ctx, nodeGroupID)
		if err != nil {
			nodg.logger.Errorf("failed to get node group by uuid %s, err: %v", nodeGroupID, err)
			return nil, err
		}
		count, err := nodg.computeService.GetCountOfServerFromServerGroup(ctx, authToken, nodeGroup.NodeGroupUUID, clusterProjectUUID.ClusterProjectUUID)
		if err != nil {
			nodg.logger.Errorf("failed to check current node size, err: %v", err)
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
			DesiredNodes:     nodeGroup.DesiredNodes,
			CurrentNodes:     count,
			NodeGroupsStatus: nodeGroup.NodeGroupsStatus,
			NodesToRemove:    ConvertDataJSONtoStringArray(nodeGroup.NodesToRemove),
		})
		return resp, nil
	} else {
		nodeGroups, err := nodg.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, clusterID, "")
		if err != nil {
			nodg.logger.Errorf("failed to get node groups by cluster uuid %s, err: %v", clusterID, err)
			return nil, err
		}
		var resp []resource.NodeGroup
		for _, nodeGroup := range nodeGroups {
			count, err := nodg.computeService.GetCountOfServerFromServerGroup(ctx, authToken, nodeGroup.NodeGroupUUID, clusterProjectUUID.ClusterProjectUUID)
			if err != nil {
				nodg.logger.Errorf("failed to check current node size, err: %v", err)
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
				DesiredNodes:     nodeGroup.DesiredNodes,
				CurrentNodes:     count,
				NodeGroupsStatus: nodeGroup.NodeGroupsStatus,
				NodesToRemove:    ConvertDataJSONtoStringArray(nodeGroup.NodesToRemove),
			})
		}
		return resp, nil
	}
}
func (nodg *nodeGroupsService) AddNode(ctx context.Context, authToken string, req request.AddNodeRequest) (resource.AddNodeResponse, error) {
	if authToken == "" {
		nodg.logger.Errorf("failed to get cluster")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to get cluster")
	}

	cluster, err := nodg.repository.Cluster().GetClusterByUUID(ctx, req.ClusterID)
	if err != nil {
		nodg.logger.Errorf("failed to get cluster, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	if cluster == nil {
		nodg.logger.Errorf("failed to get cluster")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to get cluster")
	}

	if cluster.ClusterProjectUUID == "" {
		nodg.logger.Errorf("failed to get cluster")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = nodg.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		nodg.logger.Errorf("failed to check auth token, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	nodeGroup, err := nodg.repository.NodeGroups().GetNodeGroupByUUID(ctx, req.NodeGroupID)
	if err != nil {
		nodg.logger.Errorf("failed to get node group, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	if nodeGroup.NodeGroupsStatus != NodeGroupActiveStatus {
		nodg.logger.Errorf("failed to get node groups")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to get node groups")
	}

	desiredCount, err := nodg.computeService.GetCountOfServerFromServerGroup(ctx, authToken, nodeGroup.NodeGroupUUID, cluster.ClusterProjectUUID)
	if err != nil {
		nodg.logger.Errorf("failed to get count of server from server group, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	if desiredCount >= nodeGroup.NodeGroupMaxSize {
		nodg.logger.Errorf("failed to add node, node group max size reached")
		return resource.AddNodeResponse{}, fmt.Errorf("failed to add node, node group max size reached")
	}

	subnetIDs := []string{}
	err = json.Unmarshal(cluster.ClusterSubnets, &subnetIDs)
	if err != nil {
		nodg.logger.Errorf("failed to unmarshal cluster subnets, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	networkIDResp, err := nodg.networkService.GetNetworkID(ctx, authToken, subnetIDs[0])
	if err != nil {
		nodg.logger.Errorf("failed to get network id, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	randSubnetId := GetRandomStringFromArray(subnetIDs)

	createPortRequest := request.CreateNetworkPortRequest{
		Port: request.Port{
			Name:         nodeGroup.NodeGroupName,
			NetworkID:    networkIDResp.Subnet.NetworkID,
			AdminStateUp: true,
			FixedIps: []request.FixedIp{
				{
					SubnetID: randSubnetId,
				},
			},
			SecurityGroups: []string{cluster.WorkerSecurityGroup},
		},
	}

	portResp, err := nodg.networkService.CreateNetworkPort(ctx, authToken, createPortRequest)
	if err != nil {
		nodg.logger.Errorf("failed to create port, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	rke2InitScript, err := GenerateUserDataFromTemplate("false",
		WorkerServerType,
		cluster.ClusterRegisterToken,
		cluster.ClusterEndpoint,
		cluster.ClusterVersion,
		cluster.ClusterName,
		cluster.ClusterUUID,
		config.GlobalConfig.GetWebConfig().Endpoint,
		authToken,
	)
	if err != nil {
		nodg.logger.Errorf("failed to generate user data from template, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	securityGroup, err := nodg.networkService.GetSecurityGroupByID(ctx, authToken, cluster.WorkerSecurityGroup)
	if err != nil {
		nodg.logger.Errorf("failed to get security group, error: %v", err)
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
				{Name: securityGroup.SecurityGroup.Name},
			},
			UserData: Base64Encoder(rke2InitScript),
		},
		SchedulerHints: request.SchedulerHints{
			Group: nodeGroup.NodeGroupUUID,
		},
	}

	serverResp, err := nodg.computeService.CreateCompute(ctx, authToken, createServerRequest)
	if err != nil {
		nodg.logger.Errorf("failed to create compute, error: %v", err)
		return resource.AddNodeResponse{}, err
	}

	err = nodg.repository.AuditLog().CreateAuditLog(ctx, &model.AuditLog{
		ClusterUUID: cluster.ClusterUUID,
		ProjectUUID: cluster.ClusterProjectUUID,
		Event:       fmt.Sprintf("Node %s added to cluster", nodeGroup.NodeGroupName),
		CreateDate:  time.Now(),
	})
	if err != nil {
		nodg.logger.Errorf("failed to create audit log, error: %v", err)
		return resource.AddNodeResponse{}, err
	}
	err = nodg.repository.NodeGroups().UpdateNodeGroups(ctx, &model.NodeGroups{
		DesiredNodes:        (desiredCount) + 1,
		NodeGroupUpdateDate: time.Now(),
		NodeGroupUUID:       nodeGroup.NodeGroupUUID,
	})
	if err != nil {
		nodg.logger.Errorf("failed to update node groups, error: %v", err)
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
func (nodg *nodeGroupsService) DeleteNode(ctx context.Context, authToken string, nodeGroupID string, clusterUUID string) (resource.DeleteNodeResponse, error) {
	if authToken == "" {
		nodg.logger.Errorf("failed to get cluster")
		return resource.DeleteNodeResponse{}, fmt.Errorf("failed to get cluster")
	}

	cluster, err := nodg.repository.Cluster().GetClusterByUUID(ctx, clusterUUID)
	if err != nil {
		nodg.logger.Errorf("failed to get cluster, error: %v", err)
		return resource.DeleteNodeResponse{}, err
	}
	if cluster == nil {
		nodg.logger.Errorf("failed to get cluster")
		return resource.DeleteNodeResponse{}, fmt.Errorf("failed to get cluster")
	}
	if cluster.ClusterProjectUUID == "" {
		nodg.logger.Errorf("failed to get cluster")
		return resource.DeleteNodeResponse{}, fmt.Errorf("failed to get cluster")
	}

	err = nodg.identityService.CheckAuthToken(ctx, authToken, cluster.ClusterProjectUUID)
	if err != nil {
		nodg.logger.Errorf("failed to check auth token, error: %v", err)
		return resource.DeleteNodeResponse{}, err
	}

	ng, err := nodg.repository.NodeGroups().GetNodeGroupByUUID(ctx, nodeGroupID)
	if err != nil {
		nodg.logger.Errorf("failed to get count of server from server group, error: %v", err)
		return resource.DeleteNodeResponse{}, err
	}
	if ng == nil {
		nodg.logger.Errorf("failed to get node group. Because node group is nil.")
		return resource.DeleteNodeResponse{}, fmt.Errorf("failed to get node group")
	}

	if ng.DesiredNodes <= ng.NodeGroupMinSize {
		nodg.logger.Errorf("failed to delete node, node group min size reached")
		return resource.DeleteNodeResponse{}, fmt.Errorf("failed to delete node, node group min size reached")
	}

	compute, err := nodg.computeService.GetInstances(ctx, authToken, nodeGroupID)
	if err != nil {
		nodg.logger.Errorf("failed to get instances, error: %v", err)
		return resource.DeleteNodeResponse{}, err
	}
	for _, server := range compute {
		for _, nodes := range ConvertDataJSONtoStringArray(ng.NodesToRemove) {

			if server.InstanceName == nodes {
				err = nodg.computeService.DeleteCompute(ctx, authToken, server.InstanceUUID)
				if err != nil {
					nodg.logger.Errorf("failed to delete compute, error: %v", err)
					return resource.DeleteNodeResponse{}, err
				}
				newNodesToRemove := DeleteItemFromArray(ConvertDataJSONtoStringArray(ng.NodesToRemove), nodes)
				nodesToRemoveJSON, err := json.Marshal(newNodesToRemove)
				if err != nil {
					nodg.logger.Errorf("failed to marshal nodes to remove, error: %v", err)
					return resource.DeleteNodeResponse{}, err
				}
				err = nodg.repository.NodeGroups().UpdateNodeGroups(ctx, &model.NodeGroups{
					DesiredNodes:        (ng.DesiredNodes) - 1,
					NodeGroupUpdateDate: time.Now(),
					NodeGroupUUID:       nodeGroupID,
					NodesToRemove:       nodesToRemoveJSON,
				})
				if err != nil {
					nodg.logger.Errorf("failed to update node groups, error: %v", err)
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
		nodg.logger.Errorf("failed to create audit log, error: %v", err)
		return resource.DeleteNodeResponse{}, err
	}
	return resource.DeleteNodeResponse{
		NodeGroupID: ng.NodeGroupUUID,
		ClusterID:   cluster.ClusterUUID,
	}, nil

}

func (nodg *nodeGroupsService) UpdateNodeGroups(ctx context.Context, authToken, clusterID, nodeGroupID string, req resource.UpdateNodeGroupRequest) (resource.UpdateNodeGroupResponse, error) {
	clusterProjectUUID, err := nodg.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		nodg.logger.Errorf("failed to get cluster project uuid by cluster uuid %s, err: %v", clusterID, err)
		return resource.UpdateNodeGroupResponse{}, err
	}
	getCurrentStateOfNodeGroup, err := nodg.repository.NodeGroups().GetNodeGroupByUUID(ctx, nodeGroupID)
	if err != nil {
		nodg.logger.Errorf("failed to get node group by uuid %s, err: %v", nodeGroupID, err)
		return resource.UpdateNodeGroupResponse{}, err
	}
	err = nodg.identityService.CheckAuthToken(ctx, authToken, clusterProjectUUID.ClusterProjectUUID)
	if err != nil {
		nodg.logger.Errorf("failed to check auth token, err: %v", err)
		return resource.UpdateNodeGroupResponse{}, err
	}
	fmt.Println("req", req)

	if err != nil {
		return resource.UpdateNodeGroupResponse{}, err
	}
	var latestStateOfNode []string
	var checkCurrentNode bool
	for _, node := range ConvertDataJSONtoStringArray(getCurrentStateOfNodeGroup.NodesToRemove) {
		latestStateOfNode = append(latestStateOfNode, node)
	}
	for _, node := range req.NodesToRemove {
		for _, currentNode := range latestStateOfNode {
			if currentNode == node {
				checkCurrentNode = true
				break
			}
		}
		if checkCurrentNode == false {
			latestStateOfNode = append(latestStateOfNode, node)
		}
	}
	nodesToRemove, err := json.Marshal(latestStateOfNode)

	err = nodg.repository.NodeGroups().UpdateNodeGroups(ctx, &model.NodeGroups{
		NodeGroupUUID:    nodeGroupID,
		DesiredNodes:     int(*req.DesiredNodes),
		NodeGroupMinSize: getCurrentStateOfNodeGroup.NodeGroupMinSize,
		NodeGroupMaxSize: getCurrentStateOfNodeGroup.NodeGroupMaxSize,
		NodesToRemove:    nodesToRemove,
	})
	if err != nil {
		nodg.logger.Errorf("failed to update node group by uuid %s, err: %v", nodeGroupID, err)
		return resource.UpdateNodeGroupResponse{}, err
	}
	_, err = nodg.DeleteNode(ctx, authToken, nodeGroupID, clusterID)
	if err != nil {
		nodg.logger.Errorf("failed to delete node by uuid %s, err: %v", nodeGroupID, err)
		return resource.UpdateNodeGroupResponse{}, err
	}

	response := resource.UpdateNodeGroupResponse{
		ClusterID:    clusterID,
		NodeGroupID:  nodeGroupID,
		MinSize:      getCurrentStateOfNodeGroup.NodeGroupMinSize,
		MaxSize:      getCurrentStateOfNodeGroup.NodeGroupMaxSize,
		Status:       getCurrentStateOfNodeGroup.NodeGroupsStatus,
		DesiredNodes: getCurrentStateOfNodeGroup.DesiredNodes,
	}
	return response, nil
}
