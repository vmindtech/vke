package repository

import (
	"context"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type IClusterRepository interface {
	GetClusterByUUID(ctx context.Context, uuid string) (*model.Cluster, error)
	ListClustersByProjectUUID(ctx context.Context, projectUUID string) ([]*model.Cluster, error)
}

type ClusterRepository struct {
	mysqlInstance mysqldb.IMysqlInstance
}

func NewClusterRepository(mysqlInstance mysqldb.IMysqlInstance) *ClusterRepository {
	return &ClusterRepository{
		mysqlInstance: mysqlInstance,
	}
}

func (c *ClusterRepository) GetClusterByUUID(ctx context.Context, uuid string) (*model.Cluster, error) {
	var cluster model.Cluster

	err := c.mysqlInstance.
		Database().
		WithContext(ctx).
		Where(&model.Cluster{UUID: uuid}).
		First(&cluster).
		Error

	if err != nil {
		return nil, err
	}
	return &cluster, nil
}

func (c *ClusterRepository) ListClustersByProjectUUID(ctx context.Context, projectUUID string) ([]*model.Cluster, error) {
	var clusters []*model.Cluster

	err := c.mysqlInstance.
		Database().
		WithContext(ctx).
		Where(&model.Cluster{ClusterProjectUUID: projectUUID}).
		Find(&clusters).
		Error

	if err != nil {
		return nil, err
	}
	return clusters, nil
}
