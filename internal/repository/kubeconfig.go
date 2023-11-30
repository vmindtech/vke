package repository

import (
	"context"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type IKubeconfigRepository interface {
	GetKubeconfigByUUID(ctx context.Context, clusterUUID string) (*model.Kubeconfigs, error)
}

type KubeconfigRepository struct {
	mysqlInstance mysqldb.IMysqlInstance
}

func NewKubeconfigRepository(mysqlInstance mysqldb.IMysqlInstance) *KubeconfigRepository {
	return &KubeconfigRepository{
		mysqlInstance: mysqlInstance,
	}
}

func (k *KubeconfigRepository) GetKubeconfigByUUID(ctx context.Context, clusterUUID string) (*model.Kubeconfigs, error) {
	var kubeconfig model.Kubeconfigs

	err := k.mysqlInstance.
		Database().
		WithContext(ctx).
		Where(&model.Kubeconfigs{ClusterUUID: clusterUUID}).
		First(&kubeconfig).
		Error

	if err != nil {
		return nil, err
	}
	return &kubeconfig, nil
}
