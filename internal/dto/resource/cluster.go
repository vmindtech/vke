package resource

type ClusterInfoResource struct {
	ClusterName string `json:"cluster_name"`
	ClusterID   string `json:"cluster_id"`
}

type CreateClusterResponse struct {
	ClusterID string `json:"clusterId"`
	ProjectID string `json:"projectId"`
}
