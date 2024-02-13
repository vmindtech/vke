package repository

import (
	"context"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type INodeGroupsRepository interface {
	GetNodeGroupsByClusterUUID(ctx context.Context, uuid, nodeType string) ([]model.NodeGroups, error)
	CreateNodeGroups(ctx context.Context, nodeGroups *model.NodeGroups) error
	UpdateNodeGroups(ctx context.Context, nodeGroups *model.NodeGroups) error
	GetNodeGroupByUUID(ctx context.Context, uuid string) (*model.NodeGroups, error)
	GetClusterProjectUUIDByNodeGroupUUID(ctx context.Context, nodeGroupUUID string) (string, error)
}

type NodeGroupsRepository struct {
	mysqlInstance mysqldb.IMysqlInstance
}

func NewNodeGroupsRepository(mysqlInstance mysqldb.IMysqlInstance) *NodeGroupsRepository {
	return &NodeGroupsRepository{
		mysqlInstance: mysqlInstance,
	}
}

func (n *NodeGroupsRepository) GetNodeGroupsByClusterUUID(ctx context.Context, uuid, nodeType string) ([]model.NodeGroups, error) {
	var nodeGroups []model.NodeGroups
	queryModel := &model.NodeGroups{ClusterUUID: uuid}

	if nodeType != "" {
		queryModel.NodeGroupsType = nodeType
	}

	err := n.mysqlInstance.
		Database().
		WithContext(ctx).
		Where(queryModel).
		Where(model.NodeGroups{NodeGroupsType: "worker"}).
		Find(&nodeGroups).
		Error

	if err != nil {
		return nil, err
	}

	return nodeGroups, nil
}

func (n *NodeGroupsRepository) CreateNodeGroups(ctx context.Context, nodeGroups *model.NodeGroups) error {
	return n.mysqlInstance.
		Database().
		WithContext(ctx).
		Create(nodeGroups).
		Error
}

func (n *NodeGroupsRepository) UpdateNodeGroups(ctx context.Context, nodeGroups *model.NodeGroups) error {
	return n.mysqlInstance.
		Database().
		WithContext(ctx).
		Where(&model.NodeGroups{NodeGroupUUID: nodeGroups.NodeGroupUUID}).
		Updates(nodeGroups).
		Error
}

func (n *NodeGroupsRepository) GetNodeGroupByUUID(ctx context.Context, uuid string) (*model.NodeGroups, error) {
	var nodeGroup model.NodeGroups

	err := n.mysqlInstance.
		Database().
		WithContext(ctx).
		Where(&model.NodeGroups{NodeGroupUUID: uuid}).
		First(&nodeGroup).
		Error

	if err != nil {
		return nil, err
	}
	return &nodeGroup, nil
}

func (c *NodeGroupsRepository) GetClusterProjectUUIDByNodeGroupUUID(ctx context.Context, nodeGroupUUID string) (string, error) {
	var nodeGroup model.NodeGroups

	err := c.mysqlInstance.
		Database().
		WithContext(ctx).
		Where(&model.NodeGroups{NodeGroupUUID: nodeGroupUUID}).
		First(&nodeGroup).
		Error

	if err != nil {
		return "", err
	}
	return nodeGroup.ClusterUUID, nil
}
