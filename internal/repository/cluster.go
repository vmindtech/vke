package repository

import (
	"context"
	"errors"
	"time"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

var ErrClusterNameAlreadyExists = errors.New("cluster name already exists in project")

type IClusterRepository interface {
	GetClusterByUUID(ctx context.Context, uuid string) (*model.Cluster, error)
	GetClustersByProjectId(ctx context.Context, projectId string) ([]model.Cluster, error)
	ClusterNameExists(ctx context.Context, projectID, clusterName string) (bool, error)
	CreateCluster(ctx context.Context, cluster *model.Cluster) error
	UpdateCluster(ctx context.Context, cluster *model.Cluster) error
	DeleteUpdateCluster(ctx context.Context, cluster *model.Cluster, clusterUUID string) error
	ClearLoadbalancerUUIDIfMatches(ctx context.Context, clusterUUID, loadBalancerUUID string) error
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

func (c *ClusterRepository) ClusterNameExists(ctx context.Context, projectID, clusterName string) (bool, error) {
	var count int64
	err := c.mysqlInstance.
		Database().
		WithContext(ctx).
		Model(&model.Cluster{}).
		Where("cluster_project_uuid = ? AND cluster_name = ? AND cluster_status <> ?", projectID, clusterName, "Deleted").
		Count(&count).
		Error
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (c *ClusterRepository) CreateCluster(ctx context.Context, cluster *model.Cluster) error {
	return c.mysqlInstance.
		Database().
		WithContext(ctx).
		Create(cluster).
		Error
}

func (c *ClusterRepository) UpdateCluster(ctx context.Context, cluster *model.Cluster) error {
	cluster.ClusterUpdateDate = time.Now()
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

func (c *ClusterRepository) ClearLoadbalancerUUIDIfMatches(ctx context.Context, clusterUUID, loadBalancerUUID string) error {
	return c.mysqlInstance.
		Database().
		WithContext(ctx).
		Model(&model.Cluster{}).
		Where("cluster_uuid = ? AND cluster_loadbalancer_uuid = ?", clusterUUID, loadBalancerUUID).
		Update("cluster_loadbalancer_uuid", "").
		Error
}
