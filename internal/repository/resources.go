package repository

import (
	"context"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type IResourcesRepository interface {
	CreateResource(ctx context.Context, resource *model.Resource) error
	GetResourceByClusterUUID(ctx context.Context, clusterUUID string, resourceType string) ([]model.Resource, error)
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
