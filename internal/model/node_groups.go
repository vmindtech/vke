package model

import (
	"time"

	"gorm.io/datatypes"
)

type NodeGroups struct {
	ID                     int64          `json:"-" gorm:"primary_key;auto_increment"`
	ClusterUUID            string         `json:"cluster_uuid" gorm:"type:varchar(36)"`
	NodeGroupUUID          string         `json:"node_group_uuid" gorm:"type:varchar(36)"`
	NodeGroupName          string         `json:"node_group_name" gorm:"type:varchar(255)"`
	NodeGroupLabels        datatypes.JSON `json:"node_group_labels" gorm:"type:json"`
	NodeGroupTaints        datatypes.JSON `json:"node_group_taints" gorm:"type:json"`
	NodeGroupMinSize       int            `json:"node_group_min_size" gorm:"type:int(11)"`
	NodeGroupMaxSize       int            `json:"node_group_max_size" gorm:"type:int(11)"`
	NodeDiskSize           int            `json:"node_disk_size" gorm:"type:int(11)"`
	NodeFlavorUUID         string         `json:"node_flavor_uuid" gorm:"type:varchar(36)"`
	NodeGroupsStatus       string         `json:"node_groups_status" gorm:"type:varchar(10)"` // Active, Updating, Deleted
	NodeGroupsType         string         `json:"node_groups_type" gorm:"type:varchar(10)"`   // master, worker
	IsHidden               bool           `json:"is_hidden" gorm:"type:tinyint(1)"`
	NodeGroupCreateDate    time.Time      `json:"node_group_create_date" gorm:"type:datetime"`
	NodeGroupUpdateDate    time.Time      `json:"node_group_update_date" gorm:"type:datetime;default:null"`
	NodeGroupDeleteDate    time.Time      `json:"node_group_delete_date" gorm:"type:datetime;default:null"`
	NodeGroupSecurityGroup string         `json:"node_group_security_group" gorm:"type:varchar(50)"`
}

func (NodeGroups) TableName() string {
	return "node_groups"
}
