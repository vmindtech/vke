package model

type Resource struct {
	ID           int64  `json:"-" gorm:"primary_key;auto_increment"`
	ClusterUUID  string `json:"cluster_uuid" gorm:"type:varchar(36)"`
	ResourceType string `json:"resource_type" gorm:"type:varchar(30)"`
	ResourceUUID string `json:"resource_uuid" gorm:"type:varchar(36)"`
}

func (Resource) TableName() string {
	return "resources"
}
