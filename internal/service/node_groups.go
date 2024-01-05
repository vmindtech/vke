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
	clusterProjectUUID, err := nodg.repository.Cluster().GetClusterProjectUUIDByClusterUUID(ctx, clusterID)
	if err != nil {
		return resource.GetNodeGroupsResponse{}, err
	}
	err = nodg.identityService.CheckAuthToken(ctx, authToken, clusterProjectUUID)
	if err != nil {
		return resource.GetNodeGroupsResponse{}, err
	}
	nodeGroups, err := nodg.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, clusterID, "")
	if err != nil {
		return resource.GetNodeGroupsResponse{}, err
	}
	var resp resource.GetNodeGroupsResponse
	for _, nodeGroup := range nodeGroups {
		resp.NodeGroups = append(resp.NodeGroups, resource.NodeGroups{
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
