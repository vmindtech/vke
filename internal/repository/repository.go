package repository

import (
	"context"

	"github.com/vmindtech/vke/pkg/mysqldb"
	"gorm.io/gorm"
)

type IRepository interface {
	Cluster() IClusterRepository
	AuditLog() IAuditLogRepository
	Kubeconfig() IKubeconfigRepository
	NodeGroups() INodeGroupsRepository
	StartDBTransaction(ctx context.Context) (*gorm.DB, error)
	CommitDBTransaction(tx *gorm.DB) error
}

type repository struct {
	mysqlInstance mysqldb.IMysqlInstance
	cluster       IClusterRepository
	audit         IAuditLogRepository
	kubeconfig    IKubeconfigRepository
	nodegroups    INodeGroupsRepository
}

func NewRepository(mi mysqldb.IMysqlInstance, cr IClusterRepository, ar IAuditLogRepository, kr IKubeconfigRepository, ng INodeGroupsRepository) IRepository {
	return &repository{
		mysqlInstance: mi,
		cluster:       cr,
		audit:         ar,
		kubeconfig:    kr,
		nodegroups:    ng,
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

func (r *repository) AuditLog() IAuditLogRepository {
	return r.audit
}

func (r *repository) Kubeconfig() IKubeconfigRepository {
	return r.kubeconfig
}

func (r *repository) NodeGroups() INodeGroupsRepository {
	return r.nodegroups
}
