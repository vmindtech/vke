package request

type CreateClusterRequest struct {
	ClusterName            string   `json:"clusterName"`
	ProjectID              string   `json:"projectId"`
	KubernetesVersion      string   `json:"kubernetesVersion"`
	NodeKeyPairName        string   `json:"nodeKeyPairName"`
	ClusterAPIAccess       string   `json:"clusterApiAccess"`
	SubnetIDs              []string `json:"subnetIds"`
	WorkerNodeGroupMinSize int      `json:"workerNodeGroupMinSize"`
	WorkerNodeGroupMaxSize int      `json:"workerNodeGroupMaxSize"`
	WorkerInstanceFlavorID string   `json:"workerInstanceFlavorID"`
	MasterInstanceFlavorID string   `json:"masterInstanceFlavorID"`
	WorkerDiskSizeGB       int      `json:"workerDiskSizeGB"`
	AllowedCIDRS           []string `json:"allowedCIDRs"`
}

type CreateKubeconfigRequest struct {
	ClusterID  string `json:"clusterId"`
	KubeConfig string `json:"kubeconfig"`
}
