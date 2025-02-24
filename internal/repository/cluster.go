package repository

import (
	"context"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type IClusterRepository interface {
	GetClusterByUUID(ctx context.Context, uuid string) (*model.Cluster, error)
	GetClustersByProjectId(ctx context.Context, projectId string) ([]model.Cluster, error)
	CreateCluster(ctx context.Context, cluster *model.Cluster) error
	UpdateCluster(ctx context.Context, cluster *model.Cluster) error
	DeleteUpdateCluster(ctx context.Context, cluster *model.Cluster, clusterUUID string) error
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

func (c *ClusterRepository) GetClustersByProjectId(ctx context.Context, projectId string) ([]model.Cluster, error) {
	var clusters []model.Cluster

	err := c.mysqlInstance.
		Database().
		Debug().
		WithContext(ctx).
		Where(&model.Cluster{ClusterProjectUUID: projectId}).
		Not(&model.Cluster{ClusterStatus: "Deleted"}).
		Find(&clusters).
		Error

	if err != nil {
		return nil, err
	}
	return clusters, nil
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
		Model(&model.Cluster{}).
		Where(&model.Cluster{ClusterUUID: clusterUUID}).
		Updates(&model.Cluster{
			ClusterStatus:     cluster.ClusterStatus,
			ClusterDeleteDate: cluster.ClusterDeleteDate,
			DeleteState:       cluster.DeleteState,
		}).
		Error
}
