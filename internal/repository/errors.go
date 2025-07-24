package repository

import (
	"context"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type IErrorRepository interface {
	CreateError(ctx context.Context, err *model.Error) error
	GetErrorsByClusterUUID(ctx context.Context, clusterUUID string) ([]model.Error, error)
}

type ErrorRepository struct {
	mysqlInstance mysqldb.IMysqlInstance
}

func NewErrorRepository(mysqlInstance mysqldb.IMysqlInstance) *ErrorRepository {
	return &ErrorRepository{
		mysqlInstance: mysqlInstance,
	}
}

func (e *ErrorRepository) CreateError(ctx context.Context, err *model.Error) error {
	return e.mysqlInstance.
		Database().
		WithContext(ctx).
		Create(err).
		Error
}

func (e *ErrorRepository) GetErrorsByClusterUUID(ctx context.Context, clusterUUID string) ([]model.Error, error) {
	var errors []model.Error

	err := e.mysqlInstance.
		Database().
		Debug().
		WithContext(ctx).
		Where(&model.Error{ClusterUUID: clusterUUID}).
		Order("created_at DESC").
		Find(&errors).
		Error

	if err != nil {
		return nil, err
	}
	return errors, nil
}
