package repository

import (
	"context"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type INodeGroupsRepository interface {
	GetNodeGroupsByClusterUUID(ctx context.Context, uuid, nodeType string) (*model.NodeGroups, error)
	CreateNodeGroups(ctx context.Context, nodeGroups *model.NodeGroups) error
	UpdateNodeGroups(ctx context.Context, nodeGroups *model.NodeGroups) error
}

type NodeGroupsRepository struct {
	mysqlInstance mysqldb.IMysqlInstance
}

func NewNodeGroupsRepository(mysqlInstance mysqldb.IMysqlInstance) *NodeGroupsRepository {
	return &NodeGroupsRepository{
		mysqlInstance: mysqlInstance,
	}
}

func (n *NodeGroupsRepository) GetNodeGroupsByClusterUUID(ctx context.Context, uuid, nodeType string) (*model.NodeGroups, error) {
	var nodeGroups model.NodeGroups

	err := n.mysqlInstance.
		Database().
		WithContext(ctx).
		Where(&model.NodeGroups{ClusterUUID: uuid}).
		Where(&model.NodeGroups{NodeGroupsType: nodeType}).
		First(&nodeGroups).
		Error

	if err != nil {
		return nil, err
	}
	return &nodeGroups, nil
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
