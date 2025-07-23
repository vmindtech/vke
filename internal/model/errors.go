package model

import "time"

type Error struct {
	ID           uint64    `json:"id" gorm:"primary_key;auto_increment"`
	ClusterUUID  string    `json:"cluster_uuid" gorm:"not null"`
	ErrorMessage string    `json:"error_message" gorm:"not null"`
	CreatedAt    time.Time `json:"created_at" gorm:"not null"`
}

func (Error) TableName() string {
	return "errors"
}
