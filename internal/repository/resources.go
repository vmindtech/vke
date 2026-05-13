package repository

import (
	"context"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type IResourcesRepository interface {
	CreateResource(ctx context.Context, resource *model.Resource) error
	GetResourceByClusterUUID(ctx context.Context, clusterUUID string, resourceType string) ([]model.Resource, error)
	SetResourceUUIDByClusterAndType(ctx context.Context, clusterUUID, resourceType, resourceUUID string) error
	DeleteResourcesByClusterTypeAndUUID(ctx context.Context, clusterUUID, resourceType, resourceUUID string) error
}

type ResourcesRepository struct {
	mysqlInstance mysqldb.IMysqlInstance
}

func NewResourcesRepository(mysqlInstance mysqldb.IMysqlInstance) *ResourcesRepository {
	return &ResourcesRepository{
		mysqlInstance: mysqlInstance,
	}

}
func (c *ResourcesRepository) CreateResource(ctx context.Context, resource *model.Resource) error {
	return c.mysqlInstance.
		Database().
		WithContext(ctx).
		Create(resource).
		Error
}
func (c *ResourcesRepository) GetResourceByClusterUUID(ctx context.Context, clusterUUID string, resourceType string) ([]model.Resource, error) {
	var resources []model.Resource
	return resources, c.mysqlInstance.
		Database().
		WithContext(ctx).
		Where("cluster_uuid = ? AND resource_type = ?", clusterUUID, resourceType).
		Find(&resources).
		Error
}

func (c *ResourcesRepository) SetResourceUUIDByClusterAndType(ctx context.Context, clusterUUID, resourceType, resourceUUID string) error {
	return c.mysqlInstance.
		Database().
		WithContext(ctx).
		Model(&model.Resource{}).
		Where("cluster_uuid = ? AND resource_type = ?", clusterUUID, resourceType).
		Update("resource_uuid", resourceUUID).
		Error
}

func (c *ResourcesRepository) DeleteResourcesByClusterTypeAndUUID(ctx context.Context, clusterUUID, resourceType, resourceUUID string) error {
	return c.mysqlInstance.
		Database().
		WithContext(ctx).
		Where("cluster_uuid = ? AND resource_type = ? AND resource_uuid = ?", clusterUUID, resourceType, resourceUUID).
		Delete(&model.Resource{}).
		Error
}
