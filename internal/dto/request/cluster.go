package request

type CreateClusterRequest struct {
	ClusterName            string   `json:"clusterName"`
	ProjectID              string   `json:"projectId"`
	KubernetesVersion      string   `json:"kubernetesVersion"`
	NodeKeyPairName        string   `json:"nodeKeyPairName"`
	ClusterAPIAccess       string   `json:"clusterApiAccess"`
	SubnetIDs              []string `json:"subnetIds"`
	WorkerCount            int      `json:"workerCount"`
	WorkerInstanceFlavorID string   `json:"workerInstanceFlavorID"`
	MasterInstanceFlavorID string   `json:"masterInstanceFlavorID"`
}
