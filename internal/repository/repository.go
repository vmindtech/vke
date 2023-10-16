package repository

import (
	"context"

	"github.com/vmindtech/vke/pkg/mysqldb"
	"gorm.io/gorm"
)

type IRepository interface {
	Cluster() IClusterRepository
	StartDBTransaction(ctx context.Context) (*gorm.DB, error)
	CommitDBTransaction(tx *gorm.DB) error
}

type repository struct {
	mysqlInstance mysqldb.IMysqlInstance
	cluster       IClusterRepository
}

func NewRepository(mi mysqldb.IMysqlInstance, cr IClusterRepository) IRepository {
	return &repository{
		mysqlInstance: mi,
		cluster:       cr,
	}
}

func (r *repository) StartDBTransaction(ctx context.Context) (*gorm.DB, error) {
	tx := r.mysqlInstance.
		Database().
		WithContext(ctx).Begin()

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err := tx.Error; err != nil {
		return nil, err
	}

	return tx, nil
}

func (r *repository) CommitDBTransaction(tx *gorm.DB) error {
	return tx.Commit().Error
}

func (r *repository) Cluster() IClusterRepository {
	return r.cluster
}
