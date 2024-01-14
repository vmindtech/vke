package service

import (
	"context"

	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/repository"
)

type INodeGroupsService interface {
	GetNodeGroups(ctx context.Context, authToken, clusterID string) (resource.GetNodeGroupsResponse, error)
}

type nodeGroupsService struct {
	repository      repository.IRepository
	logger          *logrus.Logger
	identityService IIdentityService
}

func NewNodeGroupsService(logger *logrus.Logger, repository repository.IRepository, i IIdentityService) INodeGroupsService {
	return &nodeGroupsService{
		repository:      repository,
		logger:          logger,
		identityService: i,
	}
}

func (nodg *nodeGroupsService) GetNodeGroups(ctx context.Context, authToken, clusterID string) (resource.GetNodeGroupsResponse, error) {
	clusterProjectUUID, err := nodg.repository.Cluster().GetClusterByUUID(ctx, clusterID)
	if err != nil {
		nodg.logger.Errorf("failed to get cluster project uuid by cluster uuid %s, err: %v", clusterID, err)
		return resource.GetNodeGroupsResponse{}, err
	}
	err = nodg.identityService.CheckAuthToken(ctx, authToken, clusterProjectUUID.ClusterProjectUUID)
	if err != nil {
		nodg.logger.Errorf("failed to check auth token, err: %v", err)
		return resource.GetNodeGroupsResponse{}, err
	}
	nodeGroups, err := nodg.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, clusterID, "")
	if err != nil {
		nodg.logger.Errorf("failed to get node groups by cluster uuid %s, err: %v", clusterID, err)
		return resource.GetNodeGroupsResponse{}, err
	}
	var resp resource.GetNodeGroupsResponse
	for _, nodeGroup := range nodeGroups {
		resp.NodeGroups = append(resp.NodeGroups, resource.NodeGroup{
			ClusterUUID:      nodeGroup.ClusterUUID,
			NodeGroupUUID:    nodeGroup.NodeGroupUUID,
			NodeGroupName:    nodeGroup.NodeGroupName,
			NodeGroupMinSize: nodeGroup.NodeGroupMinSize,
			NodeGroupMaxSize: nodeGroup.NodeGroupMaxSize,
			NodeDiskSize:     nodeGroup.NodeDiskSize,
			NodeFlavorUUID:   nodeGroup.NodeFlavorUUID,
			NodeGroupsType:   nodeGroup.NodeGroupsType,
		})
	}
	return resp, nil

}
