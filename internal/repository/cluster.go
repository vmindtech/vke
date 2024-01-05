package repository

import (
	"context"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type IClusterRepository interface {
	GetClusterByUUID(ctx context.Context, uuid string) (*model.Cluster, error)
	CreateCluster(ctx context.Context, cluster *model.Cluster) error
	UpdateCluster(ctx context.Context, cluster *model.Cluster) error
	DeleteUpdateCluster(ctx context.Context, cluster *model.Cluster, clusterUUID string) error
	GetClusterProjectUUIDByClusterUUID(ctx context.Context, clusterUUID string) (string, error)
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
		Debug().
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

func (c *ClusterRepository) UpdateCluster(ctx context.Context, cluster *model.Cluster) error {
	return c.mysqlInstance.
		Database().
		WithContext(ctx).
		Where(&model.Cluster{ClusterUUID: cluster.ClusterUUID}).
		Updates(cluster).
		Error
}

func (c *ClusterRepository) DeleteUpdateCluster(ctx context.Context, cluster *model.Cluster, clusterUUID string) error {
	return c.mysqlInstance.
		Database().
		WithContext(ctx).
		Where(&model.Cluster{ClusterUUID: clusterUUID}).
		Updates(&model.Cluster{
			ClusterDeleteDate: cluster.ClusterDeleteDate,
			ClusterStatus:     cluster.ClusterStatus,
		}).
		Error
}

func (c *ClusterRepository) GetClusterProjectUUIDByClusterUUID(ctx context.Context, clusterUUID string) (string, error) {
	var cluster model.Cluster

	err := c.mysqlInstance.
		Database().
		WithContext(ctx).
		Where(&model.Cluster{ClusterUUID: clusterUUID}).
		First(&cluster).
		Error

	if err != nil {
		return "", err
	}
	return cluster.ClusterProjectUUID, nil
}
