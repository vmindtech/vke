package model

import "time"

type AuditLog struct {
	ID          int       `json:"id" gorm:"column:id;type:int(11);AUTO_INCREMENT;primary_key"`
	ProjectUUID string    `json:"project_uuid" gorm:"column:project_uuid;type:varchar(36)"`
	ClusterUUID string    `json:"cluster_uuid" gorm:"column:cluster_uuid;type:varchar(36)"`
	Event       string    `json:"event" gorm:"column:event;type:text"`
	CreateDate  time.Time `json:"create_date" gorm:"column:create_date;type:datetime"`
}

func (m *AuditLog) TableName() string {
	return "audit_log"
}
