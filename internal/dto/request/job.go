package request

type JobMessage struct {
	JobUUID string `json:"job_uuid"`
}

type CreateClusterJobPayload struct {
	Request   CreateClusterRequest `json:"request"`
	ClusterUUID string             `json:"cluster_uuid"`
}

type DeleteClusterJobPayload struct {
	AuthToken string `json:"auth_token"`
	ClusterID string `json:"cluster_id"`
}

