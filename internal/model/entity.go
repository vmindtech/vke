package model

import (
	"time"

	"gorm.io/datatypes"
)

type Cluster struct {
	ID                      int64          `json:"-" gorm:"primary_key;auto_increment"`
	UUID                    string         `json:"cluster_uuid" gorm:"type:varchar(36);unique_index"`
	CreateDate              time.Time      `json:"create_date" gorm:"type:datetime"`
	DeleteDate              time.Time      `json:"delete_date" gorm:"type:datetime"`
	UpdateDate              time.Time      `json:"update_date" gorm:"type:datetime"`
	ClusterVersion          string         `json:"cluster_version" gorm:"type:varchar(16)"`
	ClusterStatus           string         `json:"cluster_status" gorm:"type:varchar(10)"`
	ClusterProjectUUID      string         `json:"cluster_project_uuid" gorm:"type:varchar(36)"`
	ClusterLoadbalancerUUID string         `json:"cluster_loadbalancer_uuid" gorm:"type:varchar(36)"`
	ClusterNodeTokenID      uint64         `json:"cluster_node_token_id" gorm:"type:bigint(20)"`
	ClusterSubnets          datatypes.JSON `json:"cluster_subnets" gorm:"type:json"`
}
