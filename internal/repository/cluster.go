package repository

import (
	"context"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type IClusterRepository interface {
	GetClusterByUUID(ctx context.Context, uuid string) (*model.Cluster, error)
	CreateCluster(ctx context.Context, cluster *model.Cluster) error
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
		Where(&model.Cluster{ClusterUUID: uuid}).
		First(&cluster).
		Error

	if err != nil {
		return nil, err
	}
	return &cluster, nil
}

func (c *ClusterRepository) CreateCluster(ctx context.Context, cluster *model.Cluster) error {
	return c.mysqlInstance.
		Database().
		WithContext(ctx).
		Create(cluster).
		Error
}
