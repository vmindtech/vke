package model

import (
	"time"

	"gorm.io/datatypes"
)

type Cluster struct {
	ID                      int64          `json:"-" gorm:"primary_key;auto_increment"`
	UUID                    string         `json:"cluster_uuid" gorm:"type:varchar(36);unique_index"`
	CreateDate              time.Time      `json:"cluster_create_date" gorm:"type:datetime"`
	DeleteDate              time.Time      `json:"cluster_delete_date" gorm:"type:datetime"`
	UpdateDate              time.Time      `json:"cluster_update_date" gorm:"type:datetime"`
	ClusterVersion          string         `json:"cluster_version" gorm:"type:varchar(16)"`
	ClusterStatus           string         `json:"cluster_status" gorm:"type:varchar(10)"`
	ClusterProjectUUID      string         `json:"cluster_project_uuid" gorm:"type:varchar(36)"`
	ClusterLoadbalancerUUID string         `json:"cluster_loadbalancer_uuid" gorm:"type:varchar(36)"`
	ClusterNodeToken        string         `json:"cluster_node_token" gorm:"type:varchar(36)"`
	ClusterSubnets          datatypes.JSON `json:"cluster_subnets" gorm:"type:json"`
}
