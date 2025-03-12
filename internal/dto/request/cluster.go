package request

import "time"

type CreateClusterRequest struct {
	ClusterName              string   `json:"clusterName"`
	ProjectID                string   `json:"projectId"`
	KubernetesVersion        string   `json:"kubernetesVersion"`
	NodeKeyPairName          string   `json:"nodeKeyPairName"`
	ClusterAPIAccess         string   `json:"clusterApiAccess"`
	SubnetIDs                []string `json:"subnetIds"`
	WorkerNodeGroupMinSize   int      `json:"workerNodeGroupMinSize"`
	WorkerNodeGroupMaxSize   int      `json:"workerNodeGroupMaxSize"`
	WorkerInstanceFlavorUUID string   `json:"workerInstanceFlavorUUID"`
	MasterInstanceFlavorUUID string   `json:"masterInstanceFlavorUUID"`
	WorkerDiskSizeGB         int      `json:"workerDiskSizeGB"`
	AllowedCIDRS             []string `json:"allowedCIDRs"`
}

type CreateKubeconfigRequest struct {
	ClusterID  string `json:"clusterId"`
	KubeConfig string `json:"kubeconfig"`
}

type UpdateKubeconfigRequest struct {
	KubeConfig string `json:"kubeconfig"`
}

type UpdateClusterRequest struct {
	ClusterName                  string    `json:"cluster_name"`
	ClusterVersion               string    `json:"cluster_version"`
	ClusterStatus                string    `json:"cluster_status"`
	ClusterAPIAccess             string    `json:"cluster_api_access"`
	ClusterCertificateExpireDate time.Time `json:"cluster_certificate_expire_date"`
}
