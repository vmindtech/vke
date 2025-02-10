package model

import (
	"time"

	"gorm.io/datatypes"
)

type Cluster struct {
	ID                           int64          `json:"-" gorm:"primary_key;auto_increment"`
	ClusterUUID                  string         `json:"cluster_uuid" gorm:"type:varchar(36)"`
	ClusterName                  string         `json:"cluster_name" gorm:"type:varchar(50)"`
	ClusterCreateDate            time.Time      `json:"cluster_create_date" gorm:"type:datetime"`
	ClusterDeleteDate            time.Time      `json:"cluster_delete_date" gorm:"type:datetime;default:null"`
	ClusterUpdateDate            time.Time      `json:"cluster_update_date" gorm:"type:datetime;default:null"`
	ClusterCertificateExpireDate time.Time      `json:"cluster_certificate_expire_date" gorm:"type:datetime;default:null"`
	ClusterVersion               string         `json:"cluster_version" gorm:"type:varchar(30)"`
	ClusterStatus                string         `json:"cluster_status" gorm:"type:varchar(10)"`
	ClusterProjectUUID           string         `json:"cluster_project_uuid" gorm:"type:varchar(36)"`
	ClusterLoadbalancerUUID      string         `json:"cluster_loadbalancer_uuid" gorm:"type:varchar(36)"`
	ClusterRegisterToken         string         `json:"cluster_register_token" gorm:"type:varchar(255)"`
	ClusterAgentToken            string         `json:"cluster_agent_token" gorm:"type:varchar(255)"`
	ClusterSubnets               datatypes.JSON `json:"cluster_subnets" gorm:"type:json"`
	ClusterNodeKeypairName       string         `json:"cluster_node_keypair_name" gorm:"type:varchar(140)"`
	ClusterEndpoint              string         `json:"cluster_endpoint" gorm:"type:varchar(144)"`
	ClusterAPIAccess             string         `json:"cluster_api_access" gorm:"type:varchar(255)"`
	FloatingIPUUID               string         `json:"floating_ip_uuid" gorm:"type:varchar(36)"`
	ClusterCloudflareRecordID    string         `json:"cluster_cloudflare_record_id" gorm:"type:varchar(36)"`
	ClusterSharedSecurityGroup   string         `json:"cluster_shared_security_group" gorm:"type:varchar(50)"`
	ApplicationCredentialID      string         `json:"application_credential_id" gorm:"type:varchar(36)"`
}

func (Cluster) TableName() string {
	return "clusters"
}
