package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/internal/repository"
)

type INodeGroupsService interface {
	GetNodeGroups(ctx context.Context, authToken, clusterID, nodeGroupID string) ([]resource.NodeGroup, error)
	UpdateNodeGroups(ctx context.Context, authToken, clusterID, nodeGroupID string, req resource.UpdateNodeGroupRequest) (resource.UpdateNodeGroupResponse, error)
}

type nodeGroupsService struct {
	repository      repository.IRepository
	logger          *logrus.Logger
	identityService IIdentityService
	computeService  IComputeService
}

func NewNodeGroupsService(logger *logrus.Logger, repository repository.IRepository, i IIdentityService, c IComputeService) INodeGroupsService {
	return &nodeGroupsService{
		repository:      repository,
		logger:          logger,
		identityService: i,
		computeService:  c,
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

	nodesToRemove, err := json.Marshal(req.NodesToRemove)
	if err != nil {
		return resource.UpdateNodeGroupResponse{}, err
	}

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
