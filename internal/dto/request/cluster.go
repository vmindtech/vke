package request

import "time"

type CreateClusterRequest struct {
	ClusterName              string   `json:"clusterName" validate:"required,max=50"`
	ProjectID                string   `json:"projectId" validate:"required"`
	KubernetesVersion        string   `json:"kubernetesVersion" validate:"required,max=30"`
	NodeKeyPairName          string   `json:"nodeKeyPairName" validate:"required,max=140"`
	ClusterAPIAccess         string   `json:"clusterApiAccess" validate:"required,max=255"`
	SubnetIDs                []string `json:"subnetIds" validate:"required"`
	WorkerNodeGroupMinSize   int      `json:"workerNodeGroupMinSize" validate:"required,min=1"`
	WorkerNodeGroupMaxSize   int      `json:"workerNodeGroupMaxSize" validate:"required,min=1"`
	WorkerInstanceFlavorUUID string   `json:"workerInstanceFlavorUUID" validate:"required"`
	MasterInstanceFlavorUUID string   `json:"masterInstanceFlavorUUID" validate:"required"`
	WorkerDiskSizeGB         int      `json:"workerDiskSizeGB" validate:"required,min=20"`
	AllowedCIDRS             []string `json:"allowedCIDRs" validate:"required"`
}

type CreateKubeconfigRequest struct {
	ClusterID  string `json:"clusterId" validate:"required"`
	KubeConfig string `json:"kubeconfig"`
}

type UpdateKubeconfigRequest struct {
	KubeConfig string `json:"kubeconfig" validate:"required"`
}

type UpdateClusterRequest struct {
	ClusterName                  string    `json:"cluster_name" validate:"required,max=50"`
	ClusterVersion               string    `json:"cluster_version" validate:"omitempty,max=30"`
	ClusterStatus                string    `json:"cluster_status" validate:"required,max=10"`
	ClusterAPIAccess             string    `json:"cluster_api_access" validate:"required,max=255"`
	ClusterCertificateExpireDate time.Time `json:"cluster_certificate_expire_date" validate:"required"`
}
