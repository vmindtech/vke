package model

import (
	"time"

	"gorm.io/datatypes"
)

type Cluster struct {
	ID                      int64          `json:"-" gorm:"primary_key;auto_increment"`
	ClusterUUID             string         `json:"cluster_uuid" gorm:"type:varchar(36)"`
	ClusterName             string         `json:"cluster_name" gorm:"type:varchar(50)"`
	ClusterCreateDate       time.Time      `json:"cluster_create_date" gorm:"type:datetime"`
	ClusterDeleteDate       time.Time      `json:"cluster_delete_date" gorm:"type:datetime;default:null"`
	ClusterUpdateDate       time.Time      `json:"cluster_update_date" gorm:"type:datetime;default:null"`
	ClusterVersion          string         `json:"cluster_version" gorm:"type:varchar(16)"`
	ClusterStatus           string         `json:"cluster_status" gorm:"type:varchar(10)"`
	ClusterProjectUUID      string         `json:"cluster_project_uuid" gorm:"type:varchar(36)"`
	ClusterLoadbalancerUUID string         `json:"cluster_loadbalancer_uuid" gorm:"type:varchar(36)"`
	ClusterNodeToken        string         `json:"cluster_node_token_id" gorm:"type:varchar(255)"`
	ClusterSubnets          datatypes.JSON `json:"cluster_subnets" gorm:"type:json"`
	WorkerCount             int            `json:"worker_count" gorm:"type:int(11)"`
	WorkerType              string         `json:"worker_type" gorm:"type:varchar(50)"`
	WorkerDiskSize          int            `json:"worker_disk_size" gorm:"type:int(11)"`
	ClusterEndpoint         string         `json:"cluster_endpoint" gorm:"type:varchar(144)"`
}

func (Cluster) TableName() string {
	return "clusters"
}
