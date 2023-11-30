package model

import "time"

type Kubeconfigs struct {
	ID          int       `json:"id" gorm:"column:id;type:int(11);AUTO_INCREMENT;primary_key"`
	ClusterUUID string    `json:"cluster_uuid" gorm:"column:cluster_uuid;type:varchar(36)"`
	KubeConfig  string    `json:"kubeconfig" gorm:"column:kubeconfig;type:text"`
	CreateDate  time.Time `json:"create_date" gorm:"column:create_date;type:datetime"`
	UpdateDate  time.Time `json:"update_date" gorm:"column:update_date;type:datetime"`
}

func (m *Kubeconfigs) TableName() string {
	return "kubeconfigs"
}
