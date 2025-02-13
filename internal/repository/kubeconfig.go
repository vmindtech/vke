package repository

import (
	"context"
	"time"

	"github.com/vmindtech/vke/internal/model"
	"github.com/vmindtech/vke/pkg/mysqldb"
)

type IKubeconfigRepository interface {
	GetKubeconfigByUUID(ctx context.Context, clusterUUID string) (*model.Kubeconfigs, error)
	CreateKubeconfig(ctx context.Context, kubeconfig *model.Kubeconfigs) error
	UpdateKubeconfig(ctx context.Context, clusterUUID string, kubeConfig string) error
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

func (k *KubeconfigRepository) CreateKubeconfig(ctx context.Context, kubeconfig *model.Kubeconfigs) error {
	return k.mysqlInstance.
		Database().
		WithContext(ctx).
		Create(kubeconfig).
		Error
}

func (k *KubeconfigRepository) UpdateKubeconfig(ctx context.Context, clusterUUID string, kubeConfig string) error {
	return k.mysqlInstance.
		Database().
		WithContext(ctx).
		Where(&model.Kubeconfigs{ClusterUUID: clusterUUID}).
		Updates(&model.Kubeconfigs{
			KubeConfig: kubeConfig,
			UpdateDate: time.Now(),
		}).
		Error
}
