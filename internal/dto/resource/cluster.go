package resource

type ClusterInfoResource struct {
	ClusterName string `json:"cluster_name"`
	ClusterID   string `json:"cluster_id"`
}

type CreateClusterResponse struct {
	ClusterID string `json:"clusterId"`
	ProjectID string `json:"projectId"`
}

type GetClusterResponse struct {
	ClusterID         string `json:"clusterId"`
	ProjectID         string `json:"projectId"`
	KubernetesVersion string `json:"kubernetesVersion"`
	ClusterAPIAccess  string `json:"clusterApiAccess"`
	ClusterStatus     string `json:"clusterStatus"`
}
