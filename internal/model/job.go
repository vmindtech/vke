package model

import (
	"time"

	"gorm.io/datatypes"
)

type Job struct {
	ID             int64          `json:"id" gorm:"primary_key;auto_increment"`
	JobUUID        string         `json:"job_uuid" gorm:"type:varchar(36);uniqueIndex"`
	JobType        string         `json:"job_type" gorm:"type:varchar(64);index"`
	Status         string         `json:"status" gorm:"type:varchar(24);index"`
	IdempotencyKey string         `json:"idempotency_key" gorm:"type:varchar(128);uniqueIndex"`
	ClusterUUID    string         `json:"cluster_uuid" gorm:"type:varchar(36);index"`
	ProjectUUID    string         `json:"project_uuid" gorm:"type:varchar(36);index"`
	Payload        datatypes.JSON `json:"payload" gorm:"type:json"`

	Attempts   int       `json:"attempts" gorm:"type:int;default:0"`
	MaxAttempts int      `json:"max_attempts" gorm:"type:int;default:10"`
	LastError  string    `json:"last_error" gorm:"type:text"`
	NextRunAt  *time.Time `json:"next_run_at" gorm:"type:datetime;index"`

	LockedBy string    `json:"locked_by" gorm:"type:varchar(64);index"`
	LockedAt *time.Time `json:"locked_at" gorm:"type:datetime;index"`

	CreatedAt time.Time `json:"created_at" gorm:"type:datetime;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"type:datetime;autoUpdateTime"`
}

func (Job) TableName() string {
	return "jobs"
}

